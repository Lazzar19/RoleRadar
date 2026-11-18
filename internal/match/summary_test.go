package match

import (
	"strings"
	"testing"
)

func TestPostingSummaryListsLanguagesAndTechnologies(t *testing.T) {
	text := PostingSummary(
		"Backend Intern",
		"Build services in Go and Python. Kubernetes and Docker run on Linux. A bachelor's degree is preferred.",
		"Intern",
		"",
	)
	for _, want := range []string{
		"Languages: Go, Python",
		"Technologies: Kubernetes, Docker, Linux",
		"Employment: Intern",
		"Study: bachelor's",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary missing %q\n%s", want, text)
		}
	}
	if strings.Contains(text, "Java,") || strings.Contains(text, "Java\n") {
		t.Fatalf("javascript-style false language in\n%s", text)
	}
}

func TestPostingSummarySaysWhenThePostingStatesNothing(t *testing.T) {
	text := PostingSummary("Account Executive", "", "", "")
	if !strings.Contains(text, "Languages: not stated") || !strings.Contains(text, "Technologies: not stated") {
		t.Fatalf("summary =\n%s", text)
	}
}
