package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSmartRecruitersFetchReadsPagesAndDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/companies/Endava/postings":
			if r.URL.Query().Get("offset") == "0" {
				_, _ = w.Write([]byte(`{"offset":0,"limit":1,"totalFound":2,"content":[{"id":"1","name":"Backend Intern","releasedDate":"2026-11-18T08:00:00.000Z","company":{"name":"Endava"},"location":{"city":"Belgrade","country":"rs","fullLocation":"Belgrade, Serbia","remote":false}}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"offset":1,"limit":1,"totalFound":2,"content":[{"id":"2","name":"Broken"}]}`))
		case "/v1/companies/Endava/postings/1":
			_, _ = w.Write([]byte(`{"id":"1","name":"Backend Intern","releasedDate":"2026-11-18T08:00:00.000Z","company":{"name":"Endava"},"location":{"city":"Belgrade","country":"rs","fullLocation":"Belgrade, Serbia"},"typeOfEmployment":{"label":"Intern"},"experienceLevel":{"label":"Entry level"},"postingUrl":"https://jobs.smartrecruiters.com/Endava/1","applyUrl":"https://jobs.smartrecruiters.com/Endava/1?oga=true","jobAd":{"sections":{"jobDescription":{"title":"Role","text":"<p>Build Go services.</p>"}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	snapshot, err := (SmartRecruiters{BaseURL: server.URL, Client: server.Client(), PageSize: 1}).Fetch(context.Background(), "Endava")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snapshot.Complete || len(snapshot.Postings) != 1 || len(snapshot.ItemErrors) != 1 {
		t.Fatalf("snapshot postings=%d errors=%d complete=%v", len(snapshot.Postings), len(snapshot.ItemErrors), snapshot.Complete)
	}
	job := snapshot.Postings[0]
	if job.Title != "Backend Intern" || job.Experience != "Entry level" || job.EmploymentType != "Intern" {
		t.Fatalf("job = %+v", job)
	}
	if job.Company == nil || *job.Company != "Endava" || job.Description != "Role: Build Go services." {
		t.Fatalf("company = %v description = %q", job.Company, job.Description)
	}
	if job.CanonicalURL != "https://jobs.smartrecruiters.com/Endava/1" || job.PostedAt == nil || !job.PostedAt.Equal(time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("url = %s posted = %v", job.CanonicalURL, job.PostedAt)
	}
	if len(job.Locations) != 1 || job.Locations[0].Raw != "Belgrade, Serbia" {
		t.Fatalf("locations = %+v", job.Locations)
	}
}
