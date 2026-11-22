package extract

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Lazzar19/RoleRadar/internal/domain"
)

type memLedger struct {
	tokens int
}

func (m *memLedger) ExtractorTokens(context.Context, time.Time) (int, error) {
	return m.tokens, nil
}

func (m *memLedger) AddExtractorTokens(_ context.Context, _ time.Time, n int) error {
	m.tokens += n
	return nil
}

func TestClientSendsOnlyThePostingAndRecordsTokens(t *testing.T) {
	const key = "super-secret-key"
	posting := domain.Job{
		ID:          "job-1",
		Title:       "Backend intern",
		Description: "Students are welcome. Strong Go skills are required. Linux is preferred. The team is remote. posting-marker-zz",
	}
	content := `{
		"role_category": "backend",
		"internship_shaped": true,
		"seniority": "intern",
		"required_skills": ["Go"],
		"preferred_skills": ["Linux"],
		"locations": ["remote"],
		"language": "en",
		"concerns": [],
		"evidence": [
			{"claim": "role_category", "quote": "Backend intern"},
			{"claim": "internship_shaped", "quote": "Students are welcome"},
			{"claim": "seniority", "quote": "intern"},
			{"claim": "required_skills:Go", "quote": "Strong Go skills"},
			{"claim": "preferred_skills:Linux", "quote": "Linux is preferred"},
			{"claim": "locations:remote", "quote": "remote"}
		]
	}`
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+key {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		if strings.Contains(r.URL.String(), key) {
			t.Error("API key is in the URL")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if strings.Contains(string(body), "threshold") || strings.Contains(string(body), key) {
			t.Errorf("request included a threshold or the key: %s", body)
		}
		var req chatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("request json: %v", err)
		}
		if req.Temperature != 0 || req.ResponseFormat["type"] != "json_object" || len(req.Messages) != 2 {
			t.Errorf("request = %+v", req)
		}
		if !strings.Contains(req.Messages[1].Content, "posting-marker-zz") {
			t.Errorf("user message = %q", req.Messages[1].Content)
		}
		encoded, err := json.Marshal(content)
		if err != nil {
			t.Errorf("marshal content: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + string(encoded) + `}}],"usage":{"prompt_tokens":20,"completion_tokens":11}}`))
	}))
	defer server.Close()

	ledger := &memLedger{}
	client := Client{
		BaseURL:     server.URL + "/v1",
		APIKey:      key,
		Model:       "fixture-model",
		DailyTokens: 100,
		Budget:      ledger,
		HTTP:        server.Client(),
	}
	facts, err := client.Extract(context.Background(), posting)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if calls != 1 || facts.RoleCategory != "backend" || ledger.tokens != 31 {
		t.Fatalf("calls = %d facts = %+v tokens = %d", calls, facts, ledger.tokens)
	}
}

func TestClientRejectsAScoreWithoutCallingAgain(t *testing.T) {
	const key = "super-secret-key"
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"score\":90,\"role_category\":\"backend\",\"internship_shaped\":false,\"seniority\":\"\",\"required_skills\":[],\"preferred_skills\":[],\"locations\":[],\"language\":\"\",\"concerns\":[],\"evidence\":[]}"}}],"usage":{"prompt_tokens":4,"completion_tokens":4}}`)
	}))
	defer server.Close()

	ledger := &memLedger{}
	client := Client{BaseURL: server.URL, APIKey: key, Budget: ledger, HTTP: server.Client()}
	_, err := client.Extract(context.Background(), domain.Job{Title: "Backend intern", Description: "posting-marker-zz"})
	if err == nil {
		t.Fatal("Extract() error = nil")
	}
	if strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "posting-marker-zz") {
		t.Fatalf("error includes a secret or the posting: %v", err)
	}
	if calls != 1 || ledger.tokens != 8 {
		t.Fatalf("calls = %d tokens = %d", calls, ledger.tokens)
	}
}

func TestClientStopsAtTheDailyBudget(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls++
	}))
	defer server.Close()

	client := Client{
		BaseURL:     server.URL,
		APIKey:      "super-secret-key",
		DailyTokens: 10,
		Budget:      &memLedger{tokens: 10},
		HTTP:        server.Client(),
	}
	_, err := client.Extract(context.Background(), domain.Job{Description: "Backend intern"})
	if !errors.Is(err, ErrBudget) {
		t.Fatalf("Extract() error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestClientFailureDoesNotEchoTheKeyOrThePosting(t *testing.T) {
	const key = "super-secret-key"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"super-secret-key posting-marker-zz"}}`)
	}))
	defer server.Close()

	ledger := &memLedger{}
	client := Client{BaseURL: server.URL, APIKey: key, Budget: ledger, HTTP: server.Client()}
	_, err := client.Extract(context.Background(), domain.Job{Description: "posting-marker-zz"})
	if err == nil {
		t.Fatal("Extract() error = nil")
	}
	if strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "posting-marker-zz") {
		t.Fatalf("error = %v", err)
	}
	if ledger.tokens != 0 {
		t.Fatalf("tokens = %d", ledger.tokens)
	}
}

func TestClientCapsTheDescription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req chatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("request json: %v", err)
		}
		if !strings.Contains(req.Messages[1].Content, "abcdefghij") || strings.Contains(req.Messages[1].Content, "abcdefghijk") {
			t.Errorf("description was not capped: %q", req.Messages[1].Content)
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"role_category\":\"abcdefghij\",\"internship_shaped\":false,\"seniority\":\"\",\"required_skills\":[],\"preferred_skills\":[],\"locations\":[],\"language\":\"\",\"concerns\":[],\"evidence\":[{\"claim\":\"role_category\",\"quote\":\"abcdefghij\"}]}"}}]}`)
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL, APIKey: "k", MaxChars: 10, HTTP: server.Client()}
	if _, err := client.Extract(context.Background(), domain.Job{Description: "abcdefghijkLMNOP"}); err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
}
