package match

import "github.com/Lazzar19/RoleRadar/internal/domain"

// Label is a relevance judgment taken from feedback.
// Saved and already-applied choices are not relevance labels.
type Label string

const (
	LabelRelevant    Label = "relevant"
	LabelNotRelevant Label = "not_relevant"
)

// Example is one labeled job. Facts are set when an extraction was stored for this text.
type Example struct {
	Job     domain.Job
	Profile domain.CandidateProfile
	Label   Label
	Facts   *domain.Extraction
}

// Scorecard counts how a rule set treats a labeled set.
// RelevantNotified should stay high. NotRelevantNotified should stay low.
type Scorecard struct {
	Examples            int
	Relevant            int
	NotRelevant         int
	RelevantNotified    int
	NotRelevantNotified int
}

// Score applies the rules to a labeled set and does not write anything.
func Score(examples []Example, rules Rules) Scorecard {
	var card Scorecard
	for _, example := range examples {
		card.Examples++
		notified := exampleNotifies(example, rules)
		switch example.Label {
		case LabelRelevant:
			card.Relevant++
			if notified {
				card.RelevantNotified++
			}
		case LabelNotRelevant:
			card.NotRelevant++
			if notified {
				card.NotRelevantNotified++
			}
		}
	}
	return card
}

func exampleNotifies(example Example, rules Rules) bool {
	if example.Job.Status == domain.JobClosed {
		return false
	}
	if example.Facts != nil && example.Job.Completeness == domain.CompletenessFull {
		return DecideFacts(example.Job, example.Profile, rules, *example.Facts).Notify
	}
	return Decide(example.Job, example.Profile, rules).Notify
}
