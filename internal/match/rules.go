package match

import (
	"fmt"
	"math"
	"strings"

	"github.com/Lazzar19/RoleRadar/internal/domain"
)

const (
	// RulesModel is the cache-key model for a decision that did not call an extractor.
	RulesModel = "rules"
	// RulesPrompt is the cache-key prompt version for the default rule scorer.
	// A different weight or threshold uses Rules.Version so the previous row stays.
	RulesPrompt = "rules-v1"
)

// Weights are the points each criterion can add. They are configuration.
type Weights struct {
	Role       int
	Skills     int
	Experience int
	Location   int
	Company    int
}

// Rules is the scoring configuration. Threshold is the minimum overall score that notifies.
type Rules struct {
	Weights   Weights
	Threshold int
}

// DefaultRules returns the weights and threshold from the plan.
func DefaultRules() Rules {
	return Rules{
		Weights:   Weights{Role: 30, Skills: 30, Experience: 20, Location: 10, Company: 10},
		Threshold: 70,
	}
}

// Version is the evaluation cache key for these weights.
// The default weights keep rules-v1. Any other weights use a new key, and the old row stays.
func (r Rules) Version() string {
	if r == DefaultRules() {
		return RulesPrompt
	}
	return fmt.Sprintf("%s+%d.%d.%d.%d.%d.t%d", RulesPrompt, r.Weights.Role, r.Weights.Skills, r.Weights.Experience, r.Weights.Location, r.Weights.Company, r.Threshold)
}

// Outcome is one decision. Preview scores stay nil. A nil component was unknown, not a mismatch.
type Outcome struct {
	Kind       domain.DecisionKind
	Notify     bool
	Overall    *int
	Role       *int
	Skills     *int
	Experience *int
	Location   *int
	Company    *int
}

// Decide applies the preview rule or the weighted score.
// Unknown job fields are left nil. An explicit mismatch scores zero for that criterion.
func Decide(job domain.Job, profile domain.CandidateProfile, rules Rules) Outcome {
	if job.Completeness != domain.CompletenessFull {
		return preview(job, profile)
	}
	role := scoreRole(job, profile, rules.Weights.Role)
	skills := scoreSkills(job, profile, rules.Weights.Skills)
	experience := scoreExperience(job, rules.Weights.Experience)
	location := scoreLocation(job, profile, rules.Weights.Location)
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

func preview(job domain.Job, profile domain.CandidateProfile) Outcome {
	titleMatch := softwareRole(job.Title, profile)
	excludedRole := anyPhrase(job.Title, profile.ExcludedRoles) || nonSoftwareTitle(job.Title)
	excludedCompany := job.Company != nil && listed(*job.Company, profile.ExcludedCompanies)
	allowed, _ := locationAllowed(job, profile)
	return Outcome{
		Kind:   domain.DecisionPreview,
		Notify: titleMatch && !excludedRole && !excludedCompany && allowed,
	}
}

func scoreRole(job domain.Job, profile domain.CandidateProfile, weight int) *int {
	if anyPhrase(job.Title, profile.ExcludedRoles) || nonSoftwareTitle(job.Title) {
		return scoreValue(0)
	}
	if softwareRole(job.Title, profile) {
		return scoreValue(weight)
	}
	return nil
}

// softwareRole is true when the title is a software role the profile asked for.
// An internship synonym on its own is not enough, so "Executive Assistant Intern" stays out.
func softwareRole(title string, profile domain.CandidateProfile) bool {
	return anyPhrase(title, profile.PreferredRoles) || anyPhrase(title, softwareRoles)
}

func nonSoftwareTitle(title string) bool {
	if softwareRole(title, domain.CandidateProfile{}) {
		return false
	}
	return anyPhrase(title, nonSoftwareRoles)
}

func scoreSkills(job domain.Job, profile domain.CandidateProfile, weight int) *int {
	preferred := cleanList(profile.PreferredSkills)
	additional := cleanList(profile.AdditionalSkills)
	if weight <= 0 || (len(preferred) == 0 && len(additional) == 0) {
		return nil
	}
	text := job.Title + "\n" + job.Description
	if hits := countPhrases(text, preferred); hits > 0 {
		return scoreValue(points(weight, hits, len(preferred)))
	}
	if hits := countPhrases(text, additional); hits > 0 {
		value := points(weight, hits, len(additional)*2)
		if value == 0 {
			return nil
		}
		return scoreValue(value)
	}
	return nil
}

func scoreExperience(job domain.Job, weight int) *int {
	text := strings.Join([]string{job.Title, job.EmploymentType, job.Experience, job.Description}, "\n")
	if anyPhrase(text, studentExclusions) {
		return scoreValue(0)
	}
	if anyPhrase(text, internshipSignals) {
		return scoreValue(weight)
	}
	return nil
}

func scoreLocation(job domain.Job, profile domain.CandidateProfile, weight int) *int {
	rank, known := locationRank(job, profile)
	if !known {
		return nil
	}
	switch rank {
	case locationSerbia, locationPlace:
		return scoreValue(weight)
	case locationRemote:
		if weight < 2 {
			return scoreValue(weight)
		}
		return scoreValue(weight / 2)
	default:
		return scoreValue(0)
	}
}

func scoreCompany(job domain.Job, profile domain.CandidateProfile, weight int) *int {
	if job.Company == nil || strings.TrimSpace(*job.Company) == "" {
		return nil
	}
	if listed(*job.Company, profile.ExcludedCompanies) {
		return scoreValue(0)
	}
	if listed(*job.Company, profile.PreferredCompanies) {
		return scoreValue(weight)
	}
	return nil
}

const (
	locationNone = iota
	locationRemote
	locationPlace
	locationSerbia
)

// locationRank orders a known location. Serbia outranks a named preferred place, and both outrank remote.
// Remote alone receives half of the location weight in scoreLocation.
func locationRank(job domain.Job, profile domain.CandidateProfile) (int, bool) {
	jobs := usableLocations(job.Locations)
	preferred := usableLocations(profile.PreferredLocations)
	if len(jobs) == 0 || len(preferred) == 0 {
		return locationNone, false
	}
	jobText := locationText(jobs)
	if prefersSerbia(preferred) && inSerbia(jobText) {
		return locationSerbia, true
	}
	if overlapsNamed(jobText, preferred) {
		return locationPlace, true
	}
	if overlapsRemote(jobText, jobs, preferred) {
		return locationRemote, true
	}
	return locationNone, true
}

func locationText(locations []domain.Location) string {
	var parts []string
	for _, location := range locations {
		parts = append(parts, locationTokens(location)...)
	}
	return strings.Join(parts, " ")
}

func prefersSerbia(preferred []domain.Location) bool {
	text := locationText(preferred)
	return hasPhrase(text, "serbia") || hasPhrase(text, "srbija")
}

func inSerbia(jobText string) bool {
	return anyPhrase(jobText, []string{"serbia", "srbija", "belgrade", "beograd", "novi sad", "nis", "kragujevac", "subotica"})
}

// LocatedInSerbia reports whether a posting names Serbia or a city in Serbia.
func LocatedInSerbia(locations []domain.Location) bool {
	usable := usableLocations(locations)
	if len(usable) == 0 {
		return false
	}
	return inSerbia(locationText(usable))
}

func overlapsNamed(jobText string, preferred []domain.Location) bool {
	for _, location := range preferred {
		for _, token := range locationTokens(location) {
			if fold(token) == "remote" {
				continue
			}
			if hasPhrase(jobText, token) {
				return true
			}
		}
	}
	return false
}

func overlapsRemote(jobText string, jobs, preferred []domain.Location) bool {
	remoteJob := hasPhrase(jobText, "remote") || hasPhrase(jobText, "rad od kuce")
	for _, location := range jobs {
		if location.Remote {
			remoteJob = true
		}
	}
	if !remoteJob {
		return false
	}
	for _, location := range preferred {
		if location.Remote || hasPhrase(locationText([]domain.Location{location}), "remote") {
			return true
		}
	}
	return false
}

func locationAllowed(job domain.Job, profile domain.CandidateProfile) (allowed bool, known bool) {
	rank, known := locationRank(job, profile)
	if !known {
		return true, false
	}
	return rank != locationNone, true
}

func usableLocations(locations []domain.Location) []domain.Location {
	var usable []domain.Location
	for _, location := range locations {
		if len(locationTokens(location)) > 0 {
			usable = append(usable, location)
		}
	}
	return usable
}

func locationTokens(location domain.Location) []string {
	var tokens []string
	if raw := strings.TrimSpace(location.Raw); raw != "" {
		tokens = append(tokens, raw)
	}
	if location.City != nil {
		if city := strings.TrimSpace(*location.City); city != "" {
			tokens = append(tokens, city)
		}
	}
	if location.Country != nil {
		if country := strings.TrimSpace(*location.Country); country != "" {
			tokens = append(tokens, country)
		}
	}
	if location.Remote {
		tokens = append(tokens, "remote")
	}
	return tokens
}

func listed(name string, names []string) bool {
	for _, candidate := range names {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if hasPhrase(name, candidate) || hasPhrase(candidate, name) {
			return true
		}
	}
	return false
}

func cleanList(values []string) []string {
	var cleaned []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return cleaned
}

func overallScore(weights Weights, role, skills, experience, location, company *int) *int {
	earned := 0
	known := 0
	add(&earned, &known, role, weights.Role)
	add(&earned, &known, skills, weights.Skills)
	add(&earned, &known, experience, weights.Experience)
	add(&earned, &known, location, weights.Location)
	add(&earned, &known, company, weights.Company)
	if known == 0 {
		return nil
	}
	value := int(math.Round(float64(earned) * 100 / float64(known)))
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	return &value
}

func add(earned, known *int, score *int, weight int) {
	if score == nil || weight <= 0 {
		return
	}
	*earned += *score
	*known += weight
}

func points(weight, hits, slots int) int {
	if hits <= 0 || slots <= 0 || weight <= 0 {
		return 0
	}
	value := int(math.Round(float64(weight) * float64(hits) / float64(slots)))
	if value < 1 {
		value = 1
	}
	if value > weight {
		return weight
	}
	return value
}

func scoreValue(value int) *int {
	if value < 0 {
		value = 0
	}
	return &value
}

var studentExclusions = []string{
	"no students",
	"not open to students",
	"not for students",
	"students will not be considered",
	"will not consider students",
	"students are not eligible",
	"not eligible for students",
	"excluding students",
	"students need not apply",
	"do not accept students",
	"does not accept students",
	"cannot be a student",
	"must not be a student",
}

// softwareRoles are title phrases for software engineering work.
// "developer" does not match "business development" because the phrase must end on a word boundary.
var softwareRoles = []string{
	"software engineer",
	"software engineering",
	"software developer",
	"software development",
	"software intern",
	"swe",
	"backend",
	"back-end",
	"back end",
	"frontend",
	"front-end",
	"front end",
	"full stack",
	"fullstack",
	"full-stack",
	"developer",
	"programmer",
	"golang",
	"rust",
	"devops",
	"site reliability",
	"sre",
	"data engineer",
	"machine learning",
	"ml engineer",
	"ios",
	"android",
	"platform engineer",
	"infrastructure engineer",
	"security engineer",
}

// nonSoftwareRoles are titles outside software engineering.
// A title that also matches softwareRoles stays eligible.
var nonSoftwareRoles = []string{
	"executive assistant",
	"administrative assistant",
	"personal assistant",
	"assistant",
	"receptionist",
	"office manager",
	"recruiter",
	"talent acquisition",
	"sourcer",
	"sales",
	"account executive",
	"business development",
	"account manager",
	"marketing",
	"brand",
	"content writer",
	"copywriter",
	"customer success",
	"customer support",
	"customer service",
	"support specialist",
	"finance",
	"accountant",
	"accounting",
	"controller",
	"legal",
	"counsel",
	"attorney",
	"paralegal",
	"human resources",
	"people partner",
	"people operations",
	"product manager",
	"project manager",
	"program manager",
	"designer",
}

var internshipSignals = []string{
	"intern",
	"internship",
	"praksa",
	"staž",
	"working student",
	"open to students",
	"students welcome",
	"students are encouraged",
	"university internship",
}
