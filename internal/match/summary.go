package match

import "strings"

// PostingSummary lists languages and technologies stated in the posting, plus employment and study notes.
// It does not call a model. A term is included only when the posting text contains it.
func PostingSummary(title, description, employment, experience string) string {
	text := title + "\n" + description
	lines := []string{
		"Languages: " + listedOrMissing(findTerms(text, languageTerms)),
		"Technologies: " + listedOrMissing(findTerms(text, technologyTerms)),
	}
	if value := strings.TrimSpace(employment); value != "" {
		lines = append(lines, "Employment: "+value)
	}
	if value := strings.TrimSpace(experience); value != "" {
		lines = append(lines, "Experience: "+value)
	}
	if study := studyNote(text); study != "" {
		lines = append(lines, "Study: "+study)
	}
	return strings.Join(lines, "\n")
}

func listedOrMissing(items []string) string {
	if len(items) == 0 {
		return "not stated"
	}
	return strings.Join(items, ", ")
}

func findTerms(text string, terms []namedTerm) []string {
	var found []string
	seen := map[string]bool{}
	for _, term := range terms {
		if !hasPhrase(text, term.phrase) || seen[term.name] {
			continue
		}
		seen[term.name] = true
		found = append(found, term.name)
	}
	return found
}

func studyNote(text string) string {
	var levels []string
	for _, term := range []namedTerm{
		{"phd", "PhD"},
		{"master", "master's"},
		{"bachelor", "bachelor's"},
	} {
		if hasPhrase(text, term.phrase) {
			levels = append(levels, term.name)
		}
	}
	if len(levels) == 0 && hasPhrase(text, "university") {
		return "university study mentioned"
	}
	return strings.Join(levels, ", ")
}

type namedTerm struct {
	phrase string
	name   string
}

var languageTerms = []namedTerm{
	{"golang", "Go"},
	{"go", "Go"},
	{"python", "Python"},
	{"javascript", "JavaScript"},
	{"typescript", "TypeScript"},
	{"c++", "C++"},
	{"c#", "C#"},
	{"rust", "Rust"},
	{"kotlin", "Kotlin"},
	{"java", "Java"},
	{"ruby", "Ruby"},
	{"php", "PHP"},
	{"swift", "Swift"},
	{"scala", "Scala"},
	{"sql", "SQL"},
}

var technologyTerms = []namedTerm{
	{"kubernetes", "Kubernetes"},
	{"k8s", "Kubernetes"},
	{"docker", "Docker"},
	{"terraform", "Terraform"},
	{"postgresql", "PostgreSQL"},
	{"postgres", "PostgreSQL"},
	{"mysql", "MySQL"},
	{"mongodb", "MongoDB"},
	{"redis", "Redis"},
	{"kafka", "Kafka"},
	{"react", "React"},
	{"node.js", "Node.js"},
	{"nodejs", "Node.js"},
	{"django", "Django"},
	{"flask", "Flask"},
	{"aws", "AWS"},
	{"gcp", "GCP"},
	{"azure", "Azure"},
	{"linux", "Linux"},
	{"git", "Git"},
}
