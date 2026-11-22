package match

import (
	"fmt"
	"testing"

	"github.com/Lazzar19/RoleRadar/internal/domain"
)

func TestDecideFactsScoresQuotedFacts(t *testing.T) {
	rules := DefaultRules()
	company := "Example"
	job := domain.Job{
		Completeness: domain.CompletenessFull,
		Title:        "Ignored title",
		Company:      &company,
		Description:  "This description mentions Excel and is not used.",
		Status:       domain.JobOpen,
	}
	profile := domain.CandidateProfile{
		RoleSynonyms:       []string{"intern"},
		PreferredRoles:     []string{"backend engineer"},
		PreferredSkills:    []string{"Go", "Linux"},
		PreferredLocations: []domain.Location{{Raw: "Remote", Remote: true}, {Raw: "Serbia"}},
		PreferredCompanies: []string{"Example"},
		ExcludedCompanies:  []string{"Excluded Co"},
	}
	facts := domain.Extraction{
		RoleCategory:     "backend",
		InternshipShaped: true,
		Seniority:        "intern",
		RequiredSkills:   []string{"Go", "Linux"},
		Locations:        []string{"Remote"},
	}

	outcome := DecideFacts(job, profile, rules, facts)
	assertScore(t, "role", outcome.Role, 30)
	assertScore(t, "skills", outcome.Skills, 30)
	assertScore(t, "experience", outcome.Experience, 20)
	assertScore(t, "location", outcome.Location, 5)
	assertScore(t, "company", outcome.Company, 10)
	if outcome.Overall == nil || *outcome.Overall != 95 || !outcome.Notify {
		t.Fatalf("overall = %v notify = %v", outcome.Overall, outcome.Notify)
	}
}

func TestDecideFactsKeepsUnknownSkillsNilAndHalvesPreferredHits(t *testing.T) {
	rules := DefaultRules()
	profile := domain.CandidateProfile{PreferredSkills: []string{"Go", "Linux"}}
	job := domain.Job{Completeness: domain.CompletenessFull}

	missing := DecideFacts(job, profile, rules, domain.Extraction{RequiredSkills: []string{"Excel"}})
	if missing.Skills != nil {
		t.Fatalf("unmatched skills = %v", *missing.Skills)
	}

	half := DecideFacts(job, profile, rules, domain.Extraction{PreferredSkills: []string{"Go"}})
	assertScore(t, "preferred skill", half.Skills, 8)
}

func TestDecideFactsExperienceUsesConcerns(t *testing.T) {
	rules := DefaultRules()
	job := domain.Job{Completeness: domain.CompletenessFull}

	excluded := DecideFacts(job, domain.CandidateProfile{}, rules, domain.Extraction{
		InternshipShaped: true,
		Concerns:         []string{"students are not eligible"},
	})
	assertScore(t, "excluded", excluded.Experience, 0)

	some := DecideFacts(job, domain.CandidateProfile{}, rules, domain.Extraction{
		InternshipShaped: true,
		Concerns:         []string{"some experience is helpful"},
	})
	assertScore(t, "some experience", some.Experience, 20)

	onlySome := DecideFacts(job, domain.CandidateProfile{}, rules, domain.Extraction{
		Concerns: []string{"some experience is helpful"},
	})
	if onlySome.Experience != nil {
		t.Fatalf("some experience alone scored %d", *onlySome.Experience)
	}
}

func TestDecideFactsLocationAndExcludedRole(t *testing.T) {
	rules := DefaultRules()
	profile := domain.CandidateProfile{
		RoleSynonyms:       []string{"intern"},
		ExcludedRoles:      []string{"intern"},
		PreferredLocations: []domain.Location{{Raw: "Serbia"}, {Raw: "Remote", Remote: true}},
	}
	job := domain.Job{Completeness: domain.CompletenessFull}

	role := DecideFacts(job, profile, rules, domain.Extraction{InternshipShaped: true, Seniority: "intern"})
	assertScore(t, "excluded role", role.Role, 0)

	mismatch := DecideFacts(job, profile, rules, domain.Extraction{Locations: []string{"New York"}})
	assertScore(t, "location", mismatch.Location, 0)

	unknown := DecideFacts(job, profile, rules, domain.Extraction{})
	if unknown.Location != nil {
		t.Fatalf("unknown location = %d", *unknown.Location)
	}
}

func assertScore(t *testing.T, name string, got *int, want int) {
	t.Helper()
	if got == nil || *got != want {
		gotText := "nil"
		if got != nil {
			gotText = fmt.Sprintf("%d", *got)
		}
		t.Fatalf("%s = %s, want %d", name, gotText, want)
	}
}
