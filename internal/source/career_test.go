package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCareerFetchReadsAJSONFeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("accept = %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"jobs": [
				{
					"id": "1",
					"title": "Backend Intern",
					"url": "https://careers.example/jobs/1?utm_source=site",
					"description": "<p>Build services in Go.</p>",
					"company": "Example",
					"location": "Belgrade",
					"employment_type": "Intern",
					"posted_at": "2026-11-01T00:00:00Z"
				},
				{"title": "Missing text"}
			]
		}`))
	}))
	defer server.Close()

	feed := "http://" + server.Listener.Addr().String() + "/jobs.json"
	snapshot, err := (Career{Client: server.Client()}).Fetch(context.Background(), feed)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if !snapshot.Complete || len(snapshot.Postings) != 1 || len(snapshot.ItemErrors) != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	job := snapshot.Postings[0]
	if job.ExternalID == nil || *job.ExternalID != "1" || job.Completeness != "full" {
		t.Fatalf("posting = %+v", job)
	}
	if !strings.Contains(job.Description, "Build services in Go.") || strings.Contains(job.Description, "<p>") {
		t.Fatalf("description = %q", job.Description)
	}
	wantPosted := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if job.PostedAt == nil || !job.PostedAt.Equal(wantPosted) || job.Company == nil || *job.Company != "Example" {
		t.Fatalf("posting = %+v", job)
	}
}

func TestCareerFetchRejectsHTMLAndHelloWorld(t *testing.T) {
	htmlServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>jobs</html>"))
	}))
	defer htmlServer.Close()

	feed := "http://" + htmlServer.Listener.Addr().String() + "/careers"
	if _, err := (Career{Client: htmlServer.Client()}).Fetch(context.Background(), feed); err == nil || !strings.Contains(err.Error(), "not json") {
		t.Fatalf("html error = %v", err)
	}
	if _, err := (Career{}).Fetch(context.Background(), "https://www.helloworld.rs/poslovi"); err == nil {
		t.Fatal("helloworld feed was accepted")
	}
	if _, err := (Career{}).Fetch(context.Background(), "http://careers.example/jobs.json"); err == nil {
		t.Fatal("http feed was accepted")
	}
}

func TestCareerEmptyFeedIsComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jobs":[]}`))
	}))
	defer server.Close()

	feed := "http://" + server.Listener.Addr().String() + "/jobs.json"
	snapshot, err := (Career{Client: server.Client()}).Fetch(context.Background(), feed)
	if err != nil || !snapshot.Complete || len(snapshot.Postings) != 0 {
		t.Fatalf("snapshot = %+v error = %v", snapshot, err)
	}
}
