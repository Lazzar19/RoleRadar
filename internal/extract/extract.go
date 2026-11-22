// Package extract turns a full posting into quoted facts.
// An extractor does not score the job and does not insert a notification.
package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/Lazzar19/RoleRadar/internal/domain"
	"github.com/Lazzar19/RoleRadar/prompts"
)

const (
	// PromptVersion is the cache-key version of the extraction prompt.
	PromptVersion = prompts.Version
	// DefaultModel is the model name used when configuration leaves it empty.
	DefaultModel = "gpt-4o-mini"
	// DefaultMinChars is the shortest description that is sent to a model.
	DefaultMinChars = 80
	// DefaultMaxChars is the description length cap for one model call.
	DefaultMaxChars = 12000
	// DefaultDailyTokens is the personal-agent token budget for one UTC day.
	DefaultDailyTokens = 200000
)

// Extraction is the validated fact schema. The domain type is the schema.
type Extraction = domain.Extraction

// Extractor reads facts from one posting.
// The result is not an action: the caller scores it and decides whether to notify.
type Extractor interface {
	Extract(ctx context.Context, job domain.Job) (Extraction, error)
}

// ErrBudget means today's token budget is already spent.
// The caller scores the description with the rules and does not treat this as a failed extraction.
var ErrBudget = errors.New("extractor daily token budget is spent")

// PostingText is the title and the capped description that a model is allowed to see.
func PostingText(job domain.Job, maxChars int) string {
	return strings.TrimSpace(job.Title) + "\n" + capRunes(job.Description, maxChars)
}

// Parse decodes one model response and rejects anything outside the fact schema.
// posting is the text the model was given. Every quote must appear in it.
func Parse(raw []byte, posting string) (Extraction, error) {
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(raw)))
	dec.DisallowUnknownFields()
	var facts Extraction
	if err := dec.Decode(&facts); err != nil {
		return Extraction{}, fmt.Errorf("extractor response does not match the schema: %w", err)
	}
	var extra struct{}
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Extraction{}, errors.New("extractor response does not match the schema")
	}
	if err := Validate(facts, posting); err != nil {
		return Extraction{}, err
	}
	return facts, nil
}

// Validate checks evidence quotes against the posting text.
// A claim that can change the score needs a quote. A quote that is not in the posting rejects the response.
func Validate(facts Extraction, posting string) error {
	if len(facts.Evidence) == 0 {
		return errors.New("extractor response has no evidence")
	}
	posting = normalizeQuote(posting)
	if posting == "" {
		return errors.New("extractor posting is empty")
	}
	required := map[string]bool{}
	mark := func(claim string) {
		claim = strings.TrimSpace(claim)
		if claim != "" {
			required[claim] = false
		}
	}
	if strings.TrimSpace(facts.RoleCategory) != "" {
		mark("role_category")
	}
	if facts.InternshipShaped {
		mark("internship_shaped")
	}
	if strings.TrimSpace(facts.Seniority) != "" {
		mark("seniority")
	}
	for _, skill := range facts.RequiredSkills {
		mark("required_skills:" + strings.TrimSpace(skill))
	}
	for _, skill := range facts.PreferredSkills {
		mark("preferred_skills:" + strings.TrimSpace(skill))
	}
	for _, location := range facts.Locations {
		mark("locations:" + strings.TrimSpace(location))
	}
	for _, concern := range facts.Concerns {
		mark("concerns:" + strings.TrimSpace(concern))
	}

	for _, evidence := range facts.Evidence {
		claim := strings.TrimSpace(evidence.Claim)
		quote := normalizeQuote(evidence.Quote)
		if claim == "" || quote == "" {
			return errors.New("extractor evidence is missing a claim or a quote")
		}
		if !strings.Contains(posting, quote) {
			return errors.New("extractor quote is not in the posting")
		}
		if _, ok := required[claim]; ok {
			required[claim] = true
		}
	}
	for claim, seen := range required {
		if !seen {
			return fmt.Errorf("extractor claim %s has no quote", claim)
		}
	}
	if len(required) == 0 {
		return errors.New("extractor response has no facts")
	}
	return nil
}

func normalizeQuote(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func capRunes(text string, max int) string {
	if max <= 0 {
		max = DefaultMaxChars
	}
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	return string([]rune(text)[:max])
}
