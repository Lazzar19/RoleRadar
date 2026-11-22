package match

import (
	"testing"

	"github.com/Lazzar19/RoleRadar/internal/domain"
)

func TestVersionKeepsTheDefaultKey(t *testing.T) {
	if DefaultRules().Version() != RulesPrompt {
		t.Fatalf("version = %s", DefaultRules().Version())
	}
	changed := DefaultRules()
	changed.Threshold = 80
	if changed.Version() == RulesPrompt || changed.Version() == DefaultRules().Version() {
		t.Fatalf("changed version = %s", changed.Version())
	}
}

func TestScoreComparesALabeledSetWithoutWriting(t *testing.T) {
	company := "Example"
	profile := domain.CandidateProfile{
		RoleSynonyms:       []string{"intern"},
		PreferredSkills:    []string{"Go"},
		PreferredLocations: []domain.Location{{Raw: "Remote", Remote: true}},
		PreferredCompanies: []string{"Example"},
	}
	strong := Example{
		Label:   LabelRelevant,
		Profile: profile,
		Job: domain.Job{
			Completeness: domain.CompletenessFull,
			Title:        "Backend Intern",
			Company:      &company,
			Locations:    []domain.Location{{Raw: "Remote", Remote: true}},
			Description:  "Students are welcome. Build services in Go.",
			Status:       domain.JobOpen,
		},
	}
	weak := Example{
		Label:   LabelNotRelevant,
		Profile: profile,
		Job: domain.Job{
			Completeness: domain.CompletenessFull,
			Title:        "Senior Frontend Engineer",
			Description:  "Students are not eligible. React only.",
			Status:       domain.JobOpen,
		},
	}
	current := Score([]Example{strong, weak}, DefaultRules())
	if current.RelevantNotified != 1 || current.NotRelevantNotified != 0 {
		t.Fatalf("current = %+v", current)
	}
	proposed := DefaultRules()
	proposed.Threshold = 101
	next := Score([]Example{strong, weak}, proposed)
	if next.RelevantNotified != 0 || next.NotRelevantNotified != 0 {
		t.Fatalf("proposed = %+v", next)
	}
}
