package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLeverFetchFollowsPagesAndKeepsABadPosting(t *testing.T) {
	var skips []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("mode") != "json" {
			t.Errorf("mode = %q", r.URL.Query().Get("mode"))
		}
		skips = append(skips, r.URL.Query().Get("skip"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("skip") == "0" {
			_, _ = w.Write([]byte(`[
				{
					"id": "abc",
					"text": "Backend Intern",
					"hostedUrl": "https://jobs.lever.co/acme/abc?utm_source=alert",
					"createdAt": 1407774751234,
					"workplaceType": "hybrid",
					"categories": {"commitment": "Intern", "location": "Belgrade", "allLocations": ["Belgrade", "Remote"]},
					"descriptionPlain": "Build services.",
					"lists": [{"text": "Requirements", "content": "<li>Go</li>"}],
					"additionalPlain": "Students are welcome."
				},
				"not-a-posting"
			]`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	snapshot, err := (Lever{BaseURL: server.URL, Client: server.Client(), PageSize: 2}).Fetch(context.Background(), "acme")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if !snapshot.Complete || len(snapshot.Postings) != 1 || len(snapshot.ItemErrors) != 1 {
		t.Fatalf("complete = %v postings = %d errors = %d", snapshot.Complete, len(snapshot.Postings), len(snapshot.ItemErrors))
	}
	if len(skips) != 2 || skips[0] != "0" || skips[1] != "2" {
		t.Fatalf("skips = %v", skips)
	}
	job := snapshot.Postings[0]
	if job.ExternalID == nil || *job.ExternalID != "abc" || job.Completeness != "full" {
		t.Fatalf("posting = %+v", job)
	}
	if !strings.Contains(job.Description, "Build services.") || !strings.Contains(job.Description, "Go") || !strings.Contains(job.Description, "Students are welcome.") || strings.Contains(job.Description, "<li>") {
		t.Fatalf("description = %q", job.Description)
	}
	if job.EmploymentType != "Intern" || len(job.Locations) != 2 || !job.Locations[0].Hybrid {
		t.Fatalf("employment = %q locations = %+v", job.EmploymentType, job.Locations)
	}
	wantPosted := time.UnixMilli(1407774751234).UTC()
	if job.PostedAt == nil || !job.PostedAt.Equal(wantPosted) {
		t.Fatalf("posted_at = %v", job.PostedAt)
	}
	if job.Company != nil {
		t.Fatalf("company = %q", *job.Company)
	}
}

func TestLeverFetchFailureIsNotASnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "slow down secret-body", http.StatusTooManyRequests)
	}))
	defer server.Close()

	snapshot, err := (Lever{BaseURL: server.URL, Client: server.Client()}).Fetch(context.Background(), "acme")
	if err == nil || snapshot.Complete || len(snapshot.Postings) != 0 {
		t.Fatalf("snapshot = %+v error = %v", snapshot, err)
	}
	if strings.Contains(err.Error(), "secret-body") {
		t.Fatalf("error includes the response body: %v", err)
	}
}

func TestLeverMissingDescriptionIsAnItemError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"abc","text":"Backend Intern","hostedUrl":"https://jobs.lever.co/acme/abc"}]`))
	}))
	defer server.Close()

	snapshot, err := (Lever{BaseURL: server.URL, Client: server.Client()}).Fetch(context.Background(), "acme")
	if err != nil || !snapshot.Complete || len(snapshot.Postings) != 0 || len(snapshot.ItemErrors) != 1 {
		t.Fatalf("snapshot = %+v error = %v", snapshot, err)
	}
}
