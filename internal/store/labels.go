package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

// LabeledExample is the latest relevance choice for one job.
// Saved and already-applied rows are not included.
type LabeledExample struct {
	Feedback domain.FeedbackType
	Job      domain.Job
	Profile  domain.CandidateProfile
	Facts    []byte
}

// ListLabeled reads the labeled set. It does not change profiles, weights, or evaluations.
func (p *Pool) ListLabeled(ctx context.Context) ([]LabeledExample, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT DISTINCT ON (job_id) job_id, profile_id, feedback_type
		FROM user_feedback
		WHERE feedback_type IN ('interesting', 'not_relevant')
		ORDER BY job_id, created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list labeled: %w", err)
	}
	defer rows.Close()
	var labeled []LabeledExample
	for rows.Next() {
		var example LabeledExample
		var kind string
		var jobID, profileID string
		if err := rows.Scan(&jobID, &profileID, &kind); err != nil {
			return nil, fmt.Errorf("list labeled: %w", err)
		}
		example.Feedback = domain.FeedbackType(kind)
		job, err := p.GetJob(ctx, jobID)
		if err != nil {
			return nil, err
		}
		profile, err := p.GetProfile(ctx, profileID)
		if err != nil {
			return nil, err
		}
		facts, err := p.latestFacts(ctx, job.ID, profile.Version, job.ContentHash)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		example.Job = job
		example.Profile = profile
		example.Facts = facts
		labeled = append(labeled, example)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list labeled: %w", err)
	}
	return labeled, nil
}

// FindExtraction returns the newest stored facts for one job text and model.
// A weight change can score those facts again without calling the model.
func (p *Pool) FindExtraction(ctx context.Context, jobID string, profileVersion int, contentHash, model string) ([]byte, error) {
	var facts []byte
	err := p.pool.QueryRow(ctx, `
		SELECT evaluation_data
		FROM job_evaluations
		WHERE job_id = $1::uuid
		  AND profile_version = $2
		  AND content_hash = $3
		  AND model = $4
		  AND evaluation_data IS NOT NULL
		ORDER BY evaluated_at DESC
		LIMIT 1`,
		jobID, profileVersion, contentHash, model,
	).Scan(&facts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find extraction: %w", err)
	}
	return facts, nil
}

func (p *Pool) latestFacts(ctx context.Context, jobID string, profileVersion int, contentHash string) ([]byte, error) {
	var facts []byte
	err := p.pool.QueryRow(ctx, `
		SELECT evaluation_data
		FROM job_evaluations
		WHERE job_id = $1::uuid
		  AND profile_version = $2
		  AND content_hash = $3
		  AND evaluation_data IS NOT NULL
		ORDER BY evaluated_at DESC
		LIMIT 1`,
		jobID, profileVersion, contentHash,
	).Scan(&facts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("latest facts: %w", err)
	}
	return facts, nil
}
