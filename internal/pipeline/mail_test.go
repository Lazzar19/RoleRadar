package pipeline

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Lazzar19/RoleRadar/internal/domain"
	"github.com/Lazzar19/RoleRadar/internal/match"
	"github.com/Lazzar19/RoleRadar/internal/source"
)

func TestLinkedInAlertStoresOneJobPerListing(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()
	alert, err := os.ReadFile("../source/testdata/alert.eml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	received := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	box := &replayMailbox{messages: []source.MailMessage{
		{ID: "10", ReceivedAt: received, Raw: alert},
		{ID: "11", ReceivedAt: received.Add(time.Minute), Raw: []byte(untitledAlert)},
		{ID: "12", ReceivedAt: received.Add(2 * time.Minute), Raw: []byte(laterAlert)},
	}}
	src := domain.JobSource{
		Name:    "linkedin-" + domain.NewID(),
		Type:    domain.SourceEmail,
		Enabled: true,
		Config:  domain.SourceConfig{SecretEnv: "MAILBOX_URL"},
	}
	if err := pool.UpsertSource(ctx, &src); err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}

	transport := &rejectTransport{}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handle := Handler(pool, source.Greenhouse{}, source.Lever{}, source.Career{}, source.Ashby{}, source.SmartRecruiters{}, staticOpener{box}, Scoring{Rules: match.DefaultRules()}, "candidate", logger)
	task := domain.Task{Type: domain.TaskPoll, SourceID: &src.ID}
	if err := handle(ctx, task); err != nil {
		t.Fatalf("poll error = %v", err)
	}
	if transport.calls != 0 {
		t.Fatalf("ingest made %d HTTP requests", transport.calls)
	}
	if strings.Contains(logs.String(), "Build services in Go") || strings.Contains(logs.String(), "trk=") {
		t.Fatalf("log contains the email: %s", logs.String())
	}

	first := requireJob(t, ctx, pool, src.ID, "3988776655")
	second := requireJob(t, ctx, pool, src.ID, "3988777001")
	third := requireJob(t, ctx, pool, src.ID, "4000000001")
	if first.ID == second.ID || first.ID == third.ID {
		t.Fatal("listings were stored as one job")
	}
	if first.Completeness != domain.CompletenessPreview || first.Description != "" || first.Status != domain.JobOpen {
		t.Fatalf("first job = %+v", first)
	}
	if strings.Contains(first.Description, "Build services in Go") {
		t.Fatalf("snippet was stored as the description: %q", first.Description)
	}
	if first.URL != "https://www.linkedin.com/jobs/view/3988776655" {
		t.Fatalf("url = %s", first.URL)
	}
	if first.Company == nil || *first.Company != "Acme" || len(first.Locations) != 1 || !first.Locations[0].Hybrid {
		t.Fatalf("first fields = company %v locations %+v", first.Company, first.Locations)
	}
	if first.PostedAt == nil || !first.PostedAt.Equal(received) {
		t.Fatalf("posted = %v", first.PostedAt)
	}
	ref, err := pool.FindReferenceByExternalID(ctx, src.ID, "3988776655")
	if err != nil {
		t.Fatalf("FindReferenceByExternalID() error = %v", err)
	}
	if !strings.Contains(ref.OriginalURL, "trk=") || ref.CanonicalURL != first.URL {
		t.Fatalf("reference = %+v", ref)
	}
	if third.Title != "Data Intern" || third.Status != domain.JobOpen {
		t.Fatalf("later job = %+v", third)
	}

	failed, err := pool.FindRawMessage(ctx, src.ID, "11")
	if err != nil {
		t.Fatalf("FindRawMessage() error = %v", err)
	}
	if failed.ParseStatus != source.MessageFailed || failed.ParseError == "" {
		t.Fatalf("failed message = %+v", failed)
	}
	stored, err := pool.GetSource(ctx, src.ID)
	if err != nil {
		t.Fatalf("GetSource() error = %v", err)
	}
	if stored.Cursor != "12" {
		t.Fatalf("cursor = %q, want 12", stored.Cursor)
	}

	if err := handle(ctx, task); err != nil {
		t.Fatalf("replay error = %v", err)
	}
	replayed := requireJob(t, ctx, pool, src.ID, "3988776655")
	if replayed.ID != first.ID {
		t.Fatalf("replay job id = %s, want %s", replayed.ID, first.ID)
	}
	again := requireJob(t, ctx, pool, src.ID, "4000000001")
	if again.ID != third.ID || again.Status != domain.JobOpen {
		t.Fatalf("later job changed: %+v", again)
	}
	if len(box.after) < 2 || box.after[1] != "12" {
		t.Fatalf("replay cursor = %v", box.after)
	}
}

func TestMailboxFetchErrorLeavesTheCursor(t *testing.T) {
	pool := openPool(t)
	ctx := context.Background()
	src := domain.JobSource{
		Name:    "linkedin-" + domain.NewID(),
		Type:    domain.SourceEmail,
		Enabled: true,
		Config:  domain.SourceConfig{SecretEnv: "MAILBOX_URL"},
	}
	if err := pool.UpsertSource(ctx, &src); err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	box := &replayMailbox{err: errors.New("mailbox unavailable")}
	handle := Handler(pool, source.Greenhouse{}, source.Lever{}, source.Career{}, source.Ashby{}, source.SmartRecruiters{}, staticOpener{box}, Scoring{Rules: match.DefaultRules()}, "candidate", slog.New(slog.DiscardHandler))
	task := domain.Task{Type: domain.TaskPoll, SourceID: &src.ID}
	if err := handle(ctx, task); err == nil {
		t.Fatal("poll error = nil, want a fetch error")
	}
	stored, err := pool.GetSource(ctx, src.ID)
	if err != nil {
		t.Fatalf("GetSource() error = %v", err)
	}
	if stored.Cursor != "" {
		t.Fatalf("cursor = %q, want it unchanged", stored.Cursor)
	}
}

type replayMailbox struct {
	messages []source.MailMessage
	after    []string
	err      error
}

func (m *replayMailbox) Fetch(_ context.Context, afterID string) ([]source.MailMessage, error) {
	m.after = append(m.after, afterID)
	if m.err != nil {
		return nil, m.err
	}
	return m.messages, nil
}

type staticOpener struct {
	box source.Mailbox
}

func (o staticOpener) Open(context.Context, string) (source.Mailbox, error) {
	return o.box, nil
}

type rejectTransport struct {
	calls int
}

func (t *rejectTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls++
	return nil, http.ErrNotSupported
}

const untitledAlert = `From: LinkedIn Job Alerts <jobalerts-noreply@linkedin.com>
Subject: alert
Date: Fri, 13 Nov 2026 09:05:00 +0000
Content-Type: text/plain; charset=UTF-8

View job: https://www.linkedin.com/comm/jobs/view/111222333?trk=eml
`

const laterAlert = `From: LinkedIn Job Alerts <jobalerts-noreply@linkedin.com>
Subject: 1 new job
Date: Fri, 13 Nov 2026 09:06:00 +0000
Content-Type: text/plain; charset=UTF-8

Data Intern
Initech
Berlin, Germany
View job: https://www.linkedin.com/jobs/view/4000000001?trk=eml&trackingId=ZZZ
`
