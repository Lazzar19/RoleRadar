package match

import (
	"strings"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

// DecideFacts scores a full posting from extracted facts.
// Company still comes from the job. A preview ignores the facts and uses the preview rule.
func DecideFacts(job domain.Job, profile domain.CandidateProfile, rules Rules, facts domain.Extraction) Outcome {
	if job.Completeness != domain.CompletenessFull {
		return preview(job, profile)
	}
	role := scoreRoleFacts(facts, profile, rules.Weights.Role)
	skills := scoreSkillFacts(facts, profile, rules.Weights.Skills)
	experience := scoreExperienceFacts(facts, rules.Weights.Experience)
	located := job
	located.Locations = locationsFromFacts(facts.Locations)
	location := scoreLocation(located, profile, rules.Weights.Location)
	company := scoreCompany(job, profile, rules.Weights.Company)
	overall := overallScore(rules.Weights, role, skills, experience, location, company)
	notify := overall != nil && *overall >= rules.Threshold
	return Outcome{
		Kind:       domain.DecisionEvaluated,
		Notify:     notify,
		Overall:    overall,
		Role:       role,
		Skills:     skills,
		Experience: experience,
		Location:   location,
		Company:    company,
	}
}

func scoreRoleFacts(facts domain.Extraction, profile domain.CandidateProfile, weight int) *int {
	text := facts.RoleCategory + "\n" + facts.Seniority
	if facts.InternshipShaped {
		text += "\nintern"
	}
	if anyPhrase(text, profile.ExcludedRoles) {
		return scoreValue(0)
	}
	if anyPhrase(text, profile.RoleSynonyms) || anyPhrase(text, profile.PreferredRoles) {
		return scoreValue(weight)
	}
	return nil
}

func scoreSkillFacts(facts domain.Extraction, profile domain.CandidateProfile, weight int) *int {
	preferred := cleanList(profile.PreferredSkills)
	additional := cleanList(profile.AdditionalSkills)
	if weight <= 0 || (len(preferred) == 0 && len(additional) == 0) {
		return nil
	}
	requiredText := strings.Join(cleanList(facts.RequiredSkills), "\n")
	preferredText := strings.Join(cleanList(facts.PreferredSkills), "\n")
	if hits := countPhrases(requiredText, preferred); hits > 0 {
		return scoreValue(points(weight, hits, len(preferred)))
	}
	if hits := countPhrases(preferredText, preferred); hits > 0 {
		value := points(weight, hits, len(preferred)*2)
		if value == 0 {
			return nil
		}
		return scoreValue(value)
	}
	if hits := countPhrases(requiredText+"\n"+preferredText, additional); hits > 0 {
		value := points(weight, hits, len(additional)*2)
		if value == 0 {
			return nil
		}
		return scoreValue(value)
	}
	return nil
}

func scoreExperienceFacts(facts domain.Extraction, weight int) *int {
	parts := []string{facts.Seniority}
	parts = append(parts, facts.Concerns...)
	if facts.InternshipShaped {
		parts = append(parts, "intern")
	}
	text := strings.Join(parts, "\n")
	if anyPhrase(text, studentExclusions) {
		return scoreValue(0)
	}
	if facts.InternshipShaped || anyPhrase(text, internshipSignals) {
		return scoreValue(weight)
	}
	return nil
}

func locationsFromFacts(raws []string) []domain.Location {
	var locations []domain.Location
	for _, raw := range raws {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		location := domain.Location{Raw: raw}
		folded := strings.ToLower(raw)
		if strings.Contains(folded, "remote") {
			location.Remote = true
		}
		if strings.Contains(folded, "hybrid") {
			location.Hybrid = true
		}
		locations = append(locations, location)
	}
	return locations
}
