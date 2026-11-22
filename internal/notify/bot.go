// Package notify delivers the outbox to Telegram and reads replies from the allowed user.
package notify

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Lazzar19/RoleRadar/internal/domain"
)

// Store is the database access for delivery, replies, and feedback.
type Store interface {
	ListPendingNotifications(ctx context.Context, recipient string, types []domain.DecisionKind, limit int) ([]domain.Notification, error)
	MarkNotificationSent(ctx context.Context, id, messageID string, sentAt time.Time) error
	MarkNotificationRetry(ctx context.Context, id, reason string) error
	MarkNotificationFailed(ctx context.Context, id, reason string) error
	FindNotificationByMessage(ctx context.Context, messageID string) (domain.Notification, error)
	GetNotification(ctx context.Context, id string) (domain.Notification, error)
	GetJob(ctx context.Context, id string) (domain.Job, error)
	GetEvaluation(ctx context.Context, id string) (domain.JobEvaluation, error)
	JobOrigin(ctx context.Context, jobID string) (string, string, error)
	UpgradeJobText(ctx context.Context, jobID, description, contentHash string) error
	GetActiveProfile(ctx context.Context) (domain.CandidateProfile, error)
	EnqueueMatch(ctx context.Context, jobID, contentHash string, profileVersion int) error
	InsertFeedback(ctx context.Context, jobID, profileID, notificationID string, kind domain.FeedbackType) error
	ListSources(ctx context.Context) ([]domain.JobSource, error)
	NotifyStatus(ctx context.Context) (int, int, error)
	UpsertSource(ctx context.Context, source *domain.JobSource) error
	FindReferenceByExternalID(ctx context.Context, sourceID, externalID string) (domain.SourceReference, error)
	FindReferenceByCanonicalURL(ctx context.Context, sourceID, canonicalURL string) (domain.SourceReference, error)
	InsertJobWithReference(ctx context.Context, job *domain.Job, ref *domain.SourceReference) error
	UpdateJobSnapshot(ctx context.Context, job domain.Job, ref domain.SourceReference) error
	CloseMissingJobs(ctx context.Context, sourceID string, seenExternalIDs []string) (int, error)
	FindNotification(ctx context.Context, jobID, recipient string, kind domain.DecisionKind) (domain.Notification, error)
	InsertNotification(ctx context.Context, notification *domain.Notification) error
}

// Bot long-polls Telegram and sends pending notifications.
// Messages from any other user are ignored.
type Bot struct {
	Token       string
	UserID      string
	BaseURL     string
	Client      API
	Store       Store
	Preview     bool
	Evaluated   bool
	Interval    time.Duration
	RetryBase   time.Duration
	MaxAttempts int
	Logger      *slog.Logger
	Now         func() time.Time

	mu     sync.Mutex
	next   map[string]time.Time
	tries  map[string]int
	offset int64
}

// Run sends the outbox and reads updates until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	if err := b.prepare(); err != nil {
		return err
	}
	b.Logger.Info("telegram bot started")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		b.deliverLoop(ctx)
	}()
	go func() {
		defer wg.Done()
		b.pollLoop(ctx)
	}()
	wg.Wait()
	return nil
}

func (b *Bot) prepare() error {
	if b.Logger == nil {
		b.Logger = slog.New(slog.DiscardHandler)
	}
	if b.Token == "" || b.UserID == "" {
		return errConfig("telegram token and user id are required")
	}
	if b.Store == nil {
		return errConfig("telegram store is required")
	}
	if b.Client == nil {
		b.Client = Client{Token: b.Token, BaseURL: b.BaseURL}
	}
	if b.Interval <= 0 {
		b.Interval = 5 * time.Second
	}
	if b.RetryBase <= 0 {
		b.RetryBase = 15 * time.Second
	}
	if b.MaxAttempts <= 0 {
		b.MaxAttempts = 5
	}
	if b.Now == nil {
		b.Now = time.Now
	}
	if b.next == nil {
		b.next = map[string]time.Time{}
		b.tries = map[string]int{}
	}
	return nil
}

type configError string

func (e configError) Error() string { return string(e) }

func errConfig(message string) error { return configError(message) }
