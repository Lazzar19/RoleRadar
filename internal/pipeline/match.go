package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Lazzar19/ScoutAI/internal/domain"
	"github.com/Lazzar19/ScoutAI/internal/extract"
	"github.com/Lazzar19/ScoutAI/internal/match"
	"github.com/Lazzar19/ScoutAI/internal/store"
)

func runMatch(ctx context.Context, db TaskStore, scoring Scoring, recipient string, logger *slog.Logger, task domain.Task) error {
	if task.JobID == nil || strings.TrimSpace(*task.JobID) == "" || task.ContentHash == nil || strings.TrimSpace(*task.ContentHash) == "" || task.ProfileVersion == nil {
		return fmt.Errorf("match task %s is missing the job, hash, or profile version", task.ID)
	}
	job, err := db.GetJob(ctx, *task.JobID)
	if err != nil {
		return err
	}
	if job.ContentHash != *task.ContentHash {
		logger.Info("match task is stale", "task_id", task.ID, "job_id", job.ID)
		return nil
	}
	profile, err := db.GetProfileByVersion(ctx, *task.ProfileVersion)
	if err != nil {
		return err
	}

	promptVersion := scoring.Rules.Version()
	modelName := match.RulesModel
	var facts *domain.Extraction
	if job.Completeness == domain.CompletenessFull && scoring.Extractor != nil && longEnough(job.Description, scoring.MinChars) {
		promptVersion = extractPrompt(scoring.Rules)
		modelName = scoring.Model
		if strings.TrimSpace(modelName) == "" {
			modelName = extract.DefaultModel
		}
		hit, err := evaluationCached(ctx, db, job, profile.Version, promptVersion, modelName)
		if err != nil {
			return err
		}
		if hit {
			logger.Info("match evaluation reused", "task_id", task.ID, "job_id", job.ID, "model", modelName)
			return nil
		}
		if reused, err := storedFacts(ctx, db, job, profile.Version, modelName, scoring.MaxChars); err != nil {
			return err
		} else if reused != nil {
			logger.Info("extractor facts reused", "task_id", task.ID, "job_id", job.ID, "model", modelName)
			facts = reused
		} else {
			started := time.Now()
			extracted, extractErr := scoring.Extractor.Extract(ctx, job)
			elapsed := time.Since(started).Milliseconds()
			if errors.Is(extractErr, extract.ErrBudget) {
				logger.Info("extractor budget spent", "task_id", task.ID, "job_id", job.ID, "model", modelName, "duration_ms", elapsed)
				promptVersion = scoring.Rules.Version()
				modelName = match.RulesModel
			} else if extractErr != nil {
				logger.Warn("extractor finished", "task_id", task.ID, "job_id", job.ID, "model", modelName, "duration_ms", elapsed, "err", extractErr)
				return extractErr
			} else if err := extract.Validate(extracted, extract.PostingText(job, scoring.MaxChars)); err != nil {
				logger.Warn("extractor finished", "task_id", task.ID, "job_id", job.ID, "model", modelName, "duration_ms", elapsed, "err", err)
				return err
			} else {
				logger.Info("extractor finished", "task_id", task.ID, "job_id", job.ID, "model", modelName, "duration_ms", elapsed)
				facts = &extracted
			}
		}
	}
	if facts == nil {
		hit, err := evaluationCached(ctx, db, job, profile.Version, scoring.Rules.Version(), match.RulesModel)
		if err != nil {
			return err
		}
		if hit {
			logger.Info("match evaluation reused", "task_id", task.ID, "job_id", job.ID, "model", match.RulesModel)
			return nil
		}
		promptVersion = scoring.Rules.Version()
		modelName = match.RulesModel
	}

	var outcome match.Outcome
	var data []byte
	if facts != nil {
		outcome = match.DecideFacts(job, profile, scoring.Rules, *facts)
		raw, err := json.Marshal(facts)
		if err != nil {
			return fmt.Errorf("encode extraction: %w", err)
		}
		data = raw
	} else {
		outcome = match.Decide(job, profile, scoring.Rules)
	}
	evaluation := domain.JobEvaluation{
		JobID:           job.ID,
		ProfileID:       profile.ID,
		ProfileVersion:  profile.Version,
		ContentHash:     job.ContentHash,
		PromptVersion:   promptVersion,
		Model:           modelName,
		Kind:            outcome.Kind,
		OverallScore:    outcome.Overall,
		SkillsScore:     outcome.Skills,
		RoleScore:       outcome.Role,
		ExperienceScore: outcome.Experience,
		LocationScore:   outcome.Location,
		CompanyScore:    outcome.Company,
		EvaluationData:  data,
		EvaluatedAt:     time.Now().UTC(),
	}
	var notification *domain.Notification
	if outcome.Notify && job.Status != domain.JobClosed {
		recipient = strings.TrimSpace(recipient)
		if recipient == "" {
			recipient = "candidate"
		}
		notification = &domain.Notification{
			JobID:     job.ID,
			Recipient: recipient,
			Type:      outcome.Kind,
			Channel:   domain.ChannelTelegram,
			Status:    domain.DeliveryPending,
		}
	}
	if err := db.SaveDecision(ctx, &evaluation, notification); err != nil {
		return err
	}
	logger.Info("match decision stored", "task_id", task.ID, "job_id", job.ID, "kind", outcome.Kind, "notify", notification != nil)
	return nil
}

func evaluationCached(ctx context.Context, db TaskStore, job domain.Job, profileVersion int, promptVersion, modelName string) (bool, error) {
	_, err := db.FindEvaluationByKey(ctx, job.ID, profileVersion, job.ContentHash, promptVersion, modelName)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func longEnough(description string, minChars int) bool {
	if minChars <= 0 {
		minChars = extract.DefaultMinChars
	}
	return utf8.RuneCountInString(strings.TrimSpace(description)) >= minChars
}

func extractPrompt(rules match.Rules) string {
	version := rules.Version()
	if version == match.RulesPrompt {
		return extract.PromptVersion
	}
	return extract.PromptVersion + "+" + strings.TrimPrefix(version, match.RulesPrompt+"+")
}

func storedFacts(ctx context.Context, db TaskStore, job domain.Job, profileVersion int, modelName string, maxChars int) (*domain.Extraction, error) {
	raw, err := db.FindExtraction(ctx, job.ID, profileVersion, job.ContentHash, modelName)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var facts domain.Extraction
	if err := json.Unmarshal(raw, &facts); err != nil {
		return nil, nil
	}
	if err := extract.Validate(facts, extract.PostingText(job, maxChars)); err != nil {
		return nil, nil
	}
	return &facts, nil
}
