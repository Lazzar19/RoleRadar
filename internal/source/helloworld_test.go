package source

import (
	"strings"
	"testing"
)

func TestParseHelloWorldAlertReadsTheCardTitle(t *testing.T) {
	raw := []byte(strings.Join([]string{
		"From: HelloWorld.rs <noreply@helloworld.rs>",
		"Subject: Novi oglasi",
		"Date: Sun, 22 Nov 2026 09:00:00 +0000",
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
		"",
		"<html><body>",
		`<a class="card-title" href="https://www.helloworld.rs/posao/backend-intern/acme/768362?utm=mail">Backend Intern</a>`,
		"<div>Acme</div>",
		"<div>Beograd (Hybrid)</div>",
		`<a href="https://www.helloworld.rs/sacuvana-pretraga-odjava/24389">Odjava</a>`,
		"</body></html>",
	}, "\n"))
	postings, err := ParseJobAlert(raw)
	if err != nil {
		t.Fatalf("ParseJobAlert() error = %v", err)
	}
	if len(postings) != 1 {
		t.Fatalf("listings = %d, want 1", len(postings))
	}
	got := postings[0]
	if got.Title != "Backend Intern" || got.Company == nil || *got.Company != "Acme" {
		t.Fatalf("posting = %+v", got)
	}
	if got.ExternalID == nil || *got.ExternalID != "768362" {
		t.Fatalf("id = %v", got.ExternalID)
	}
	if got.CanonicalURL != "https://www.helloworld.rs/posao/backend-intern/acme/768362" {
		t.Fatalf("canonical = %s", got.CanonicalURL)
	}
	if len(got.Locations) != 1 || got.Locations[0].Raw != "Beograd (Hybrid)" {
		t.Fatalf("location = %+v", got.Locations)
	}
	if got.Description != "" || got.Completeness != "preview" {
		t.Fatalf("completeness = %s description = %q", got.Completeness, got.Description)
	}
}

func TestParseJobAlertRejectsAnUnrelatedSender(t *testing.T) {
	raw := []byte("From: News <news@example.com>\r\nSubject: hi\r\n\r\nhello\r\n")
	if _, err := ParseJobAlert(raw); err == nil {
		t.Fatal("ParseJobAlert() error = nil, want an unrelated sender error")
	}
}
