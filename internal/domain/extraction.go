package domain

// Evidence is one claim tied to a quote copied from the posting.
type Evidence struct {
	Claim string `json:"claim"`
	Quote string `json:"quote"`
}

// Extraction is the fact schema returned by an extractor.
// It has no recommendation, confidence, or numeric score.
type Extraction struct {
	RoleCategory     string     `json:"role_category"`
	InternshipShaped bool       `json:"internship_shaped"`
	Seniority        string     `json:"seniority"`
	RequiredSkills   []string   `json:"required_skills"`
	PreferredSkills  []string   `json:"preferred_skills"`
	Locations        []string   `json:"locations"`
	Language         string     `json:"language"`
	Concerns         []string   `json:"concerns"`
	Evidence         []Evidence `json:"evidence"`
}
