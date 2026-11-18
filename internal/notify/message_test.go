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
		"URL: https://www.linkedin.com/jobs/view/1?utm_source=alert",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("preview missing %q\n%s", want, text)
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
	for _, want := range []string{"MATCH 84/100", "Backend Intern", "Languages: Go", "Technologies: Kubernetes, Linux", "Employment: Intern", "Posted: 2026-11-18"} {
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
