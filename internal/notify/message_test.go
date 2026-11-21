package notify

import (
	"strings"
	"testing"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

func TestPreviewTextHasNoScore(t *testing.T) {
	company := "Example Technologies"
	text := PreviewText(domain.Job{
		Title:     "Go_Intern *_*[v1]*",
		Company:   &company,
		Locations: []domain.Location{{Raw: "Europe", Remote: true}},
	}, "LinkedIn alerts", "https://www.linkedin.com/jobs/view/1?utm_source=alert")

	if strings.Contains(text, "/100") {
		t.Fatalf("preview showed a score:\n%s", text)
	}
	for _, want := range []string{
		"PREVIEW — description not in the source",
		"Go_Intern *_*[v1]*",
		"Company: Example Technologies",
		"Location: Remote / Europe",
		"Languages: Go",
		"Technologies: not stated",
		"Posted: not stated",
		"Reply to this message with the text to score it.",
		"Source: LinkedIn alerts",
		`<a href="https://www.linkedin.com/jobs/view/1?utm_source=alert">Go_Intern *_*[v1]*</a>`,
		`<a href="https://www.linkedin.com/jobs/view/1?utm_source=alert">Open posting</a>`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("preview missing %q\n%s", want, text)
		}
	}
	if strings.Contains(text, "URL:") {
		t.Fatalf("preview showed the raw URL:\n%s", text)
	}
}

func TestPostingLinkEscapesTitleAndQuery(t *testing.T) {
	text := EvaluatedText(domain.Job{Title: "Go <intern>"}, 70, "A & B", "https://boards.example/jobs/1?a=1&b=2")
	for _, want := range []string{
		`<a href="https://boards.example/jobs/1?a=1&amp;b=2">Go &lt;intern&gt;</a>`,
		"Source: A &amp; B",
		`<a href="https://boards.example/jobs/1?a=1&amp;b=2">Open posting</a>`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("evaluated text missing %q\n%s", want, text)
		}
	}
}

func TestEvaluatedTextShowsTheRuleScore(t *testing.T) {
	posted := time.Date(2026, 11, 18, 23, 30, 0, 0, time.FixedZone("CET", 3600))
	text := EvaluatedText(domain.Job{
		Title:          "Backend Intern",
		Description:    "The team uses Go, Kubernetes, and Linux.",
		EmploymentType: "Intern",
		PostedAt:       &posted,
	}, 84, "Greenhouse", "https://boards.example/jobs/1")
	for _, want := range []string{
		"MATCH 84/100",
		`<a href="https://boards.example/jobs/1">Backend Intern</a>`,
		"Languages: Go",
		"Technologies: Kubernetes, Linux",
		"Employment: Intern",
		"Posted: 2026-11-18",
		`<a href="https://boards.example/jobs/1">Open posting</a>`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("evaluated text missing %q\n%s", want, text)
		}
	}
}

func TestFeedbackButtonsUseTheNotification(t *testing.T) {
	buttons := FeedbackButtons("job-note")
	if len(buttons) != 2 || len(buttons[0]) != 2 || len(buttons[1]) != 2 {
		t.Fatalf("buttons = %+v", buttons)
	}
	if buttons[0][0].Data != "fb:interesting:job-note" || buttons[1][1].Data != "fb:already_applied:job-note" {
		t.Fatalf("buttons = %+v", buttons)
	}
}
