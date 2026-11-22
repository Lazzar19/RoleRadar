package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Lazzar19/RoleRadar/internal/domain"
	"github.com/Lazzar19/RoleRadar/internal/extract"
	"github.com/Lazzar19/RoleRadar/internal/match"
	"github.com/Lazzar19/RoleRadar/internal/source"
	"github.com/Lazzar19/RoleRadar/internal/store"
	"github.com/Lazzar19/RoleRadar/internal/worker"
)

// PollStore is the database access for one board or mailbox poll.
type PollStore interface {
	Store
	GetSource(ctx context.Context, id string) (domain.JobSource, error)
	StartSourceRun(ctx context.Context, sourceID string, startedAt time.Time) (string, error)
	FinishSourceRun(ctx context.Context, runID, status string, discovered, processed int, runErr string, finishedAt time.Time) error
	MarkSourceChecked(ctx context.Context, sourceID string, checkedAt time.Time) error
	MarkSourceSnapshot(ctx context.Context, sourceID string, at time.Time) error
	FindRawMessage(ctx context.Context, sourceID, providerMessageID string) (domain.RawMessage, error)
	InsertRawMessage(ctx context.Context, msg *domain.RawMessage) error
	SetSourceCursor(ctx context.Context, sourceID, cursor string, at time.Time) error
}

// TaskStore is the database access for polling a board and scoring a match task.
type TaskStore interface {
	PollStore
	GetJob(ctx context.Context, id string) (domain.Job, error)
	GetProfileByVersion(ctx context.Context, version int) (domain.CandidateProfile, error)
	FindEvaluationByKey(ctx context.Context, jobID string, profileVersion int, contentHash, promptVersion, model string) (domain.JobEvaluation, error)
	FindExtraction(ctx context.Context, jobID string, profileVersion int, contentHash, model string) ([]byte, error)
	SaveDecision(ctx context.Context, evaluation *domain.JobEvaluation, notify *domain.Notification) error
	ListSameRoleNotifications(ctx context.Context, jobID, recipient string) ([]store.SameRoleNotification, error)
	MarkNotificationFailed(ctx context.Context, id, reason string) error
}

// Scoring is the rule configuration and the optional fact extractor.
// A nil Extractor keeps the rules-only path.
type Scoring struct {
	Rules     match.Rules
	Extractor extract.Extractor
	Model     string
	MinChars  int
	MaxChars  int
}

// Handler polls Greenhouse boards and alert mail, and scores match tasks.
// The fetch and the score run after the claim transaction has committed.
func Handler(db TaskStore, boards source.Greenhouse, lever source.Lever, career source.Career, ashby source.Ashby, smart source.SmartRecruiters, mail source.MailOpener, scoring Scoring, recipient string, logger *slog.Logger) worker.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context, task domain.Task) error {
		switch task.Type {
		case domain.TaskPoll:
			return runPoll(ctx, db, boards, lever, career, ashby, smart, mail, logger, task)
		case domain.TaskMatch:
			return runMatch(ctx, db, scoring, recipient, logger, task)
		default:
			return fmt.Errorf("unknown task type %s", task.Type)
		}
	}
}

func runPoll(ctx context.Context, db PollStore, boards source.Greenhouse, lever source.Lever, career source.Career, ashby source.Ashby, smart source.SmartRecruiters, mail source.MailOpener, logger *slog.Logger, task domain.Task) error {
	if task.SourceID == nil || *task.SourceID == "" {
		return fmt.Errorf("poll task %s has no source", task.ID)
	}
	src, err := db.GetSource(ctx, *task.SourceID)
	if err != nil {
		return err
	}
	if !src.Enabled {
		logger.Info("skip disabled source", "source", src.Name)
		return nil
	}
	switch src.Type {
	case domain.SourceGreenhouse:
		return runSnapshot(ctx, db, logger, task.ID, src, func(ctx context.Context) (source.Snapshot, error) {
			return boards.Fetch(ctx, src.Config.BoardToken)
		})
	case domain.SourceLever:
		return runSnapshot(ctx, db, logger, task.ID, src, func(ctx context.Context) (source.Snapshot, error) {
			return lever.Fetch(ctx, src.Config.Site)
		})
	case domain.SourceCareer:
		return runSnapshot(ctx, db, logger, task.ID, src, func(ctx context.Context) (source.Snapshot, error) {
			return career.Fetch(ctx, src.Config.FeedURL)
		})
	case domain.SourceAshby:
		return runSnapshot(ctx, db, logger, task.ID, src, func(ctx context.Context) (source.Snapshot, error) {
			return ashby.Fetch(ctx, src.Config.Site)
		})
	case domain.SourceSmartRecruiters:
		return runSnapshot(ctx, db, logger, task.ID, src, func(ctx context.Context) (source.Snapshot, error) {
			return smart.Fetch(ctx, src.Config.Site)
		})
	case domain.SourceEmail:
		return runMailbox(ctx, db, mail, logger, task.ID, src)
	default:
		return fmt.Errorf("source %s type %s is not polled yet", src.Name, src.Type)
	}
}

func runSnapshot(ctx context.Context, db PollStore, logger *slog.Logger, taskID string, src domain.JobSource, fetch func(context.Context) (source.Snapshot, error)) error {
	started := time.Now().UTC()
	runID, err := db.StartSourceRun(ctx, src.ID, started)
	if err != nil {
		return err
	}
	snapshot, fetchErr := fetch(ctx)
	if fetchErr != nil {
		logger.Error("source run failed", "run_id", runID, "task_id", taskID, "source", src.Name, "err", fetchErr)
		_ = db.FinishSourceRun(ctx, runID, "failed", 0, 0, fetchErr.Error(), time.Now().UTC())
		_ = db.MarkSourceChecked(ctx, src.ID, time.Now().UTC())
		return fetchErr
	}
	result, err := Apply(ctx, db, src, snapshot, time.Now().UTC())
	finished := time.Now().UTC()
	if err != nil {
		logger.Error("source run failed", "run_id", runID, "task_id", taskID, "source", src.Name, "err", err)
		_ = db.FinishSourceRun(ctx, runID, "failed", len(snapshot.Postings)+len(snapshot.ItemErrors), result.Stored, err.Error(), finished)
		_ = db.MarkSourceChecked(ctx, src.ID, finished)
		return err
	}
	note := ""
	if len(result.ItemErrors) > 0 {
		note = fmt.Sprintf("%d postings could not be read", len(result.ItemErrors))
	}
	if err := db.FinishSourceRun(ctx, runID, "succeeded", len(snapshot.Postings)+len(snapshot.ItemErrors), result.Stored, note, finished); err != nil {
		return err
	}
	if err := db.MarkSourceChecked(ctx, src.ID, finished); err != nil {
		return err
	}
	if snapshot.Complete && len(result.ItemErrors) == 0 {
		if err := db.MarkSourceSnapshot(ctx, src.ID, finished); err != nil {
			return err
		}
	}
	logger.Info("snapshot stored", "run_id", runID, "task_id", taskID, "source", src.Name, "discovered", len(snapshot.Postings)+len(snapshot.ItemErrors), "stored", result.Stored, "closed", result.Closed)
	return nil
}
