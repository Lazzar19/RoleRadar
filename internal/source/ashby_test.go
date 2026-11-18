package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAshbyFetchReadsAListedJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/posting-api/job-board/perplexity" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jobs":[
			{"id":"hidden","title":"Secret","isListed":false,"jobUrl":"https://jobs.ashbyhq.com/perplexity/hidden","descriptionPlain":"no"},
			{"id":"intern-1","title":"Backend Intern","location":"Belgrade","workplaceType":"Hybrid","employmentType":"Intern","publishedAt":"2026-11-18T08:00:00Z","descriptionPlain":"Go services.","jobUrl":"https://jobs.ashbyhq.com/perplexity/intern-1","applyUrl":"https://jobs.ashbyhq.com/perplexity/intern-1/apply","address":{"postalAddress":{"addressLocality":"Belgrade","addressCountry":"Serbia"}}}
		]}`))
	}))
	t.Cleanup(server.Close)

	snapshot, err := (Ashby{BaseURL: server.URL, Client: server.Client()}).Fetch(context.Background(), "perplexity")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if !snapshot.Complete || len(snapshot.Postings) != 1 || len(snapshot.ItemErrors) != 0 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	job := snapshot.Postings[0]
	if job.ExternalID == nil || *job.ExternalID != "intern-1" || job.Title != "Backend Intern" {
		t.Fatalf("job = %+v", job)
	}
	if job.CanonicalURL != "https://jobs.ashbyhq.com/perplexity/intern-1" || job.OriginalURL != "https://jobs.ashbyhq.com/perplexity/intern-1/apply" {
		t.Fatalf("urls = %s %s", job.CanonicalURL, job.OriginalURL)
	}
	if job.Company == nil || *job.Company != "perplexity" || job.EmploymentType != "Intern" {
		t.Fatalf("company = %v employment = %s", job.Company, job.EmploymentType)
	}
	if job.PostedAt == nil || !job.PostedAt.Equal(time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("posted = %v", job.PostedAt)
	}
	if len(job.Locations) == 0 || !job.Locations[0].Hybrid || !strings.Contains(job.Locations[0].Raw, "Belgrade") {
		t.Fatalf("locations = %+v", job.Locations)
	}
}

func TestAshbyFetchRejectsAMissingBoard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	if _, err := (Ashby{BaseURL: server.URL, Client: server.Client()}).Fetch(context.Background(), "missing"); err == nil {
		t.Fatal("Fetch() error = nil, want the HTTP failure")
	}
}
