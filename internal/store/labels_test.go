package store

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/Lazzar19/RoleRadar/internal/domain"
)

func TestListLabeledUsesTheLatestRelevanceChoice(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := Migrate(ctx, databaseURL, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	pool, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(pool.Close)

	profile := domain.CandidateProfile{Name: "labels-" + domain.NewID(), Version: 3, Active: true, RoleSynonyms: []string{"intern"}}
	if err := pool.InsertProfile(ctx, &profile); err != nil {
		t.Fatalf("InsertProfile() error = %v", err)
	}
	relevant := insertLabelJob(t, ctx, pool, "Relevant Intern")
	ignored := insertLabelJob(t, ctx, pool, "Saved Intern")
	note := insertLabelNote(t, ctx, pool, relevant.ID)
	saved := insertLabelNote(t, ctx, pool, ignored.ID)
	if err := pool.InsertFeedback(ctx, relevant.ID, profile.ID, note.ID, domain.FeedbackInteresting); err != nil {
		t.Fatalf("InsertFeedback() error = %v", err)
	}
	if err := pool.InsertFeedback(ctx, relevant.ID, profile.ID, note.ID, domain.FeedbackNotRelevant); err != nil {
		t.Fatalf("InsertFeedback() error = %v", err)
	}
	if err := pool.InsertFeedback(ctx, ignored.ID, profile.ID, saved.ID, domain.FeedbackSaved); err != nil {
		t.Fatalf("InsertFeedback() error = %v", err)
	}

	labeled, err := pool.ListLabeled(ctx)
	if err != nil {
		t.Fatalf("ListLabeled() error = %v", err)
	}
	var found *LabeledExample
	for i := range labeled {
		if labeled[i].Job.ID == relevant.ID {
			found = &labeled[i]
		}
		if labeled[i].Job.ID == ignored.ID {
			t.Fatal("saved feedback was labeled as relevance")
		}
	}
	if found == nil || found.Feedback != domain.FeedbackNotRelevant || found.Profile.Version != profile.Version {
		t.Fatalf("labeled = %+v", found)
	}
	got, err := pool.GetProfile(ctx, profile.ID)
	if err != nil || got.Version != profile.Version {
		t.Fatalf("profile version = %d error = %v", got.Version, err)
	}

	evaluation := domain.JobEvaluation{
		JobID:          relevant.ID,
		ProfileID:      profile.ID,
		ProfileVersion: profile.Version,
		ContentHash:    relevant.ContentHash,
		PromptVersion:  "rules-v1",
		Model:          "rules",
		Kind:           domain.DecisionEvaluated,
		EvaluatedAt:    time.Now().UTC(),
	}
	if err := pool.SaveDecision(ctx, &evaluation, nil); err != nil {
		t.Fatalf("SaveDecision() error = %v", err)
	}
	profile.Version++
	if err := pool.UpdateProfile(ctx, profile); err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if _, err := pool.FindEvaluationByKey(ctx, relevant.ID, 3, relevant.ContentHash, "rules-v1", "rules"); err != nil {
		t.Fatalf("old evaluation error = %v", err)
	}
}

func insertLabelJob(t *testing.T, ctx context.Context, pool *Pool, title string) domain.Job {
	t.Helper()
	src := domain.JobSource{Name: "labels-" + domain.NewID(), Type: domain.SourceManual, Enabled: true}
	if err := pool.UpsertSource(ctx, &src); err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	now := time.Now().UTC()
	job := domain.Job{
		Kind:         domain.KindPost,
		Completeness: domain.CompletenessFull,
		Title:        title,
		URL:          "https://example.com/jobs/" + domain.NewID(),
		ApplyMethod:  domain.ApplyURL,
		DiscoveredAt: now,
		ContentHash:  domain.NewID(),
		Status:       domain.JobOpen,
	}
	ref := domain.SourceReference{SourceID: src.ID, OriginalURL: job.URL, CanonicalURL: job.URL, DiscoveredAt: now}
	if err := pool.InsertJobWithReference(ctx, &job, &ref); err != nil {
		t.Fatalf("InsertJobWithReference() error = %v", err)
	}
	return job
}

func insertLabelNote(t *testing.T, ctx context.Context, pool *Pool, jobID string) domain.Notification {
	t.Helper()
	note := domain.Notification{
		JobID:     jobID,
		Recipient: "labels-" + domain.NewID(),
		Type:      domain.DecisionEvaluated,
		Channel:   domain.ChannelTelegram,
		Status:    domain.DeliveryPending,
	}
	if err := pool.InsertNotification(ctx, &note); err != nil {
		t.Fatalf("InsertNotification() error = %v", err)
	}
	return note
}
