package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"log/slog"

	"github.com/Lazzar19/ScoutAI/internal/domain"
	"github.com/Lazzar19/ScoutAI/internal/extract"
	"github.com/Lazzar19/ScoutAI/internal/health"
	"github.com/Lazzar19/ScoutAI/internal/match"
	"github.com/Lazzar19/ScoutAI/internal/pipeline"
	"github.com/Lazzar19/ScoutAI/internal/schedule"
	"github.com/Lazzar19/ScoutAI/internal/source"
	"github.com/Lazzar19/ScoutAI/internal/store"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestWorkflow walks one candidate through discovery, scoring, delivery, feedback, and a read-only weight comparison.
// Boards and the model are local stand-ins. The test does not claim the shared queue, so it cannot fetch a real board.
func TestWorkflow(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	pool := openNotifyPool(t)
	ctx := context.Background()
	transport := &rejectTransport{}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })

	userID, user := testUser(t)
	profile := notifyProfile(userID)
	if err := pool.InsertProfile(ctx, &profile); err != nil {
		t.Fatalf("InsertProfile() error = %v", err)
	}
	if err := pool.UpdateProfile(ctx, profile); err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}

	internID := time.Now().UnixNano()
	otherID := internID + 1
	description := "Backend Intern opening. Students are welcome to apply for this internship. The team wants strong Go skills and Linux experience. Work is remote in Europe."
	var greenhouseStatus int
	var greenhouseBody string
	greenhouseStatus = http.StatusOK
	greenhouseBody = `{"jobs":[` + greenhouseJob(internID, "Backend Intern", "Example", description) + `,` + greenhouseJob(otherID, "Senior Frontend Engineer", "Other", "Students are not eligible. React only.") + `]}`
	greenhouse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "content=true") {
			t.Errorf("greenhouse query = %s", r.URL.RawQuery)
		}
		w.WriteHeader(greenhouseStatus)
		_, _ = w.Write([]byte(greenhouseBody))
	}))
	t.Cleanup(greenhouse.Close)

	leverHits := 0
	lever := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leverHits++
		if r.Header.Get("User-Agent") != "ScoutAI" {
			t.Errorf("lever user agent = %q", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{
			"id":"lev-1",
			"text":"Platform Intern",
			"categories":{"location":"Belgrade","commitment":"Intern"},
			"descriptionPlain":"Short Go role.",
			"hostedUrl":"https://jobs.lever.co/acme/lev-1",
			"workplaceType":"on-site",
			"createdAt":1407774751234
		}]`))
	}))
	t.Cleanup(lever.Close)

	career := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("career accept = %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jobs":[{
			"id":"c1",
			"title":"Data Intern",
			"url":"https://careers.example/jobs/c1",
			"description":"SQL intern.",
			"company":"Example",
			"location":"Remote",
			"employment_type":"Intern",
			"posted_at":"2026-11-01T00:00:00Z"
		}]}`))
	}))
	t.Cleanup(career.Close)

	extractCalls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		extractCalls++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("extractor body: %v", err)
		}
		raw, _ := json.Marshal(payload)
		if strings.Contains(string(raw), profile.Name) || strings.Contains(strings.ToLower(string(raw)), "threshold") {
			t.Errorf("extractor request included the profile or a threshold: %s", raw)
		}
		if payload["temperature"] != float64(0) {
			t.Errorf("temperature = %v", payload["temperature"])
		}
		facts := `{
			"role_category":"backend",
			"internship_shaped":true,
			"seniority":"intern",
			"required_skills":["Go","Linux"],
			"preferred_skills":[],
			"locations":["remote"],
			"language":"en",
			"concerns":[],
			"evidence":[
				{"claim":"role_category","quote":"Backend Intern"},
				{"claim":"internship_shaped","quote":"Students are welcome"},
				{"claim":"seniority","quote":"internship"},
				{"claim":"required_skills:Go","quote":"strong Go skills"},
				{"claim":"required_skills:Linux","quote":"Linux experience"},
				{"claim":"locations:remote","quote":"remote"}
			]
		}`
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": facts}}},
			"usage":   map[string]int{"prompt_tokens": 20, "completion_tokens": 20},
		})
	}))
	t.Cleanup(model.Close)

	token := "wf-" + domain.NewID()
	boards := upsertWorkflowSource(t, ctx, pool, domain.JobSource{
		Name: "greenhouse-" + token, Type: domain.SourceGreenhouse, Enabled: true,
		Config: domain.SourceConfig{BoardToken: token},
	})
	levers := upsertWorkflowSource(t, ctx, pool, domain.JobSource{
		Name: "lever-" + token, Type: domain.SourceLever, Enabled: true,
		Config: domain.SourceConfig{Site: "acme"},
	})
	feeds := upsertWorkflowSource(t, ctx, pool, domain.JobSource{
		Name: "career-" + token, Type: domain.SourceCareer, Enabled: true,
		Config: domain.SourceConfig{FeedURL: "http://" + career.Listener.Addr().String() + "/jobs.json"},
	})
	mails := upsertWorkflowSource(t, ctx, pool, domain.JobSource{
		Name: "mail-" + token, Type: domain.SourceEmail, Enabled: true,
		Config: domain.SourceConfig{SecretEnv: "MAILBOX_URL"},
	})
	disabled := upsertWorkflowSource(t, ctx, pool, domain.JobSource{
		Name: "disabled-" + token, Type: domain.SourceGreenhouse, Enabled: false,
		Config: domain.SourceConfig{BoardToken: "disabled"},
	})
	t.Cleanup(func() {
		finishWorkflowTasks(t, databaseURL, []string{boards.ID, levers.ID, feeds.ID, mails.ID, disabled.ID})
	})

	scheduler := schedule.Scheduler{
		Store: onlyWorkflowSources{Pool: pool, allow: map[string]bool{
			boards.ID: true, levers.ID: true, feeds.ID: true, mails.ID: true, disabled.ID: true,
		}},
		Interval: time.Hour, Jitter: 0,
		MailboxInterval: time.Minute, MailboxJitter: 0,
		CareerInterval: 6 * time.Hour, CareerJitter: 0,
	}
	if err := scheduler.EnqueueDue(ctx); err != nil {
		t.Fatalf("EnqueueDue() error = %v", err)
	}
	for _, id := range []string{boards.ID, levers.ID, feeds.ID, mails.ID} {
		if _, err := pool.ActivePoll(ctx, id); err != nil {
			t.Fatalf("ActivePoll(%s) error = %v", id, err)
		}
	}
	if _, err := pool.ActivePoll(ctx, disabled.ID); err == nil {
		t.Fatal("a disabled source was scheduled")
	}
	firstPoll, err := pool.ActivePoll(ctx, feeds.ID)
	if err != nil {
		t.Fatalf("ActivePoll() error = %v", err)
	}
	if err := scheduler.EnqueueDue(ctx); err != nil {
		t.Fatalf("second EnqueueDue() error = %v", err)
	}
	again, err := pool.ActivePoll(ctx, feeds.ID)
	if err != nil || again.ID != firstPoll.ID {
		t.Fatalf("second poll = %+v, want %s", again, firstPoll.ID)
	}

	alert, err := os.ReadFile(filepath.Join("..", "source", "testdata", "alert.eml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	box := &workflowMailbox{messages: []source.MailMessage{
		{ID: "wf-1", ReceivedAt: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC), Raw: alert},
		{ID: "wf-2", ReceivedAt: time.Date(2026, 11, 13, 9, 1, 0, 0, time.UTC), Raw: []byte(remoteAlert)},
	}}
	extractor := extract.Client{
		BaseURL: model.URL + "/v1", APIKey: "test-key", Model: "workflow-model",
		MaxChars: 12000, DailyTokens: 0, HTTP: model.Client(), Logger: slog.New(slog.DiscardHandler),
	}
	handle := pipeline.Handler(
		pool,
		source.Greenhouse{BaseURL: greenhouse.URL, Client: greenhouse.Client()},
		source.Lever{BaseURL: lever.URL, Client: lever.Client(), PageSize: 2},
		source.Career{Client: career.Client()},
		source.Ashby{}, source.SmartRecruiters{},
		workflowOpener{box},
		pipeline.Scoring{Rules: match.DefaultRules(), Extractor: extractor, Model: "workflow-model", MinChars: 80, MaxChars: 12000},
		userID,
		slog.New(slog.DiscardHandler),
	)
	for _, src := range []domain.JobSource{boards, levers, feeds, mails} {
		poll, err := pool.ActivePoll(ctx, src.ID)
		if err != nil {
			t.Fatalf("ActivePoll() error = %v", err)
		}
		if err := handle(ctx, poll); err != nil {
			t.Fatalf("poll %s error = %v", src.Type, err)
		}
	}
	if transport.calls != 0 {
		t.Fatalf("workflow made %d unexpected HTTP requests", transport.calls)
	}
	if leverHits != 1 {
		t.Fatalf("lever requests = %d", leverHits)
	}

	intern := workflowJob(t, ctx, pool, boards.ID, strconv.FormatInt(internID, 10))
	other := workflowJob(t, ctx, pool, boards.ID, strconv.FormatInt(otherID, 10))
	if intern.ID == other.ID || intern.Completeness != domain.CompletenessFull || strings.Contains(intern.Description, "<p>") {
		t.Fatalf("greenhouse intern = %+v", intern)
	}
	if intern.Company == nil || *intern.Company != "Example" {
		t.Fatalf("company = %v", intern.Company)
	}
	leverJob := workflowJob(t, ctx, pool, levers.ID, "lev-1")
	if leverJob.Company != nil {
		t.Fatalf("lever company = %v, want it empty", leverJob.Company)
	}
	careerJob := workflowJob(t, ctx, pool, feeds.ID, "c1")
	if careerJob.Company == nil || *careerJob.Company != "Example" || careerJob.Completeness != domain.CompletenessFull {
		t.Fatalf("career job = %+v", careerJob)
	}
	mailJob := workflowJob(t, ctx, pool, mails.ID, "3988776655")
	if mailJob.Completeness != domain.CompletenessPreview || mailJob.Description != "" || mailJob.URL != "https://www.linkedin.com/jobs/view/3988776655" {
		t.Fatalf("mail job = %+v", mailJob)
	}
	remoteJob := workflowJob(t, ctx, pool, mails.ID, "555000111")
	if remoteJob.Completeness != domain.CompletenessPreview || remoteJob.Description != "" {
		t.Fatalf("remote preview = %+v", remoteJob)
	}
	if err := handle(ctx, mustPoll(t, ctx, pool, mails.ID)); err != nil {
		t.Fatalf("replay error = %v", err)
	}
	if replayed := workflowJob(t, ctx, pool, mails.ID, "3988776655"); replayed.ID != mailJob.ID {
		t.Fatalf("replay id = %s, want %s", replayed.ID, mailJob.ID)
	}

	for _, job := range []domain.Job{intern, other, leverJob, careerJob, mailJob, remoteJob} {
		task, err := pool.FindActiveMatch(ctx, job.ID, job.ContentHash, profile.Version)
		if err != nil {
			t.Fatalf("FindActiveMatch(%s) error = %v", job.Title, err)
		}
		if err := handle(ctx, task); err != nil {
			t.Fatalf("match %s error = %v", job.Title, err)
		}
	}
	if extractCalls != 1 {
		t.Fatalf("extractor calls = %d, want 1", extractCalls)
	}
	scored, err := pool.FindEvaluationByKey(ctx, intern.ID, profile.Version, intern.ContentHash, extract.PromptVersion, "workflow-model")
	if err != nil || scored.OverallScore == nil || *scored.OverallScore < match.DefaultRules().Threshold {
		t.Fatalf("evaluation = %+v error = %v", scored, err)
	}
	if _, err := pool.FindNotification(ctx, intern.ID, userID, domain.DecisionEvaluated); err != nil {
		t.Fatalf("evaluated notification error = %v", err)
	}
	if _, err := pool.FindNotification(ctx, mailJob.ID, userID, domain.DecisionPreview); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Belgrade preview notification error = %v, want not found", err)
	}
	previewNote, err := pool.FindNotification(ctx, remoteJob.ID, userID, domain.DecisionPreview)
	if err != nil {
		t.Fatalf("preview notification error = %v", err)
	}
	if previewNote.Type != domain.DecisionPreview {
		t.Fatalf("preview = %+v", previewNote)
	}

	europeNote, err := pool.FindNotification(ctx, intern.ID, userID, domain.DecisionEvaluated)
	if err != nil {
		t.Fatalf("evaluated notification error = %v", err)
	}
	if europeNote.DeliverAfter == nil {
		t.Fatal("a Europe match was queued for immediate delivery")
	}
	if previewNote.DeliverAfter == nil {
		t.Fatal("a remote preview was queued for immediate delivery")
	}
	sends, telegram := telegramServer(t, http.StatusOK)
	t.Cleanup(telegram.Close)
	bot := testBot(pool, userID, telegram)
	if err := bot.DeliverOnce(ctx); err != nil {
		t.Fatalf("DeliverOnce() error = %v", err)
	}
	if len(*sends) != 0 {
		t.Fatalf("evening postings were sent immediately: %+v", *sends)
	}

	versionBefore := profile.Version
	if err := bot.handle(ctx, Update{Callback: &Callback{
		ID: "cb-1", UserID: user, ChatID: user,
		Data: "fb:" + string(domain.FeedbackInteresting) + ":" + previewNote.ID,
	}}); err != nil {
		t.Fatalf("interesting error = %v", err)
	}
	if err := bot.handle(ctx, Update{Callback: &Callback{
		ID: "cb-2", UserID: user, ChatID: user,
		Data: "fb:" + string(domain.FeedbackSaved) + ":" + previewNote.ID,
	}}); err != nil {
		t.Fatalf("saved error = %v", err)
	}
	if err := bot.handle(ctx, Update{Callback: &Callback{
		ID: "cb-3", UserID: user, ChatID: user,
		Data: "fb:" + string(domain.FeedbackNotRelevant) + ":" + previewNote.ID,
	}}); err != nil {
		t.Fatalf("not relevant error = %v", err)
	}
	storedProfile, err := pool.GetProfile(ctx, profile.ID)
	if err != nil || storedProfile.Version != versionBefore {
		t.Fatalf("profile version = %d error = %v", storedProfile.Version, err)
	}
	labeled, err := pool.ListLabeled(ctx)
	if err != nil {
		t.Fatalf("ListLabeled() error = %v", err)
	}
	var found bool
	for _, row := range labeled {
		if row.Job.ID == remoteJob.ID {
			found = row.Feedback == domain.FeedbackNotRelevant
		}
	}
	if !found {
		t.Fatal("latest relevance choice was not not_relevant")
	}

	rejected := "https://example.com/jobs/" + token
	if err := bot.handle(ctx, command(user, "/add "+rejected)); err != nil {
		t.Fatalf("rejected add error = %v", err)
	}
	if !hasText(*sends, "That host is not allowed") {
		t.Fatalf("sends = %+v", *sends)
	}
	hello := "https://www.helloworld.rs/posao/" + token
	if err := bot.handle(ctx, command(user, "/add "+hello)); err != nil {
		t.Fatalf("helloworld add error = %v", err)
	}
	if transport.calls != 0 {
		t.Fatalf("/add made %d HTTP requests", transport.calls)
	}
	manual, ok := findSource(mustSources(t, ctx, pool), source.ManualSourceName)
	if !ok {
		t.Fatal("manual source was not stored")
	}
	if _, err := pool.ActivePoll(ctx, manual.ID); err == nil {
		t.Fatal("manual /add was scheduled as a poll")
	}
	canonical, err := pipeline.CanonicalURL(hello)
	if err != nil {
		t.Fatalf("CanonicalURL() error = %v", err)
	}
	ref, err := pool.FindReferenceByCanonicalURL(ctx, manual.ID, canonical)
	if err != nil {
		t.Fatalf("FindReferenceByCanonicalURL() error = %v", err)
	}
	if ref.OriginalURL != hello {
		t.Fatalf("original url = %s", ref.OriginalURL)
	}
	added, err := pool.GetJob(ctx, ref.JobID)
	if err != nil || added.Completeness != domain.CompletenessPreview || added.Description != "" {
		t.Fatalf("added job = %+v error = %v", added, err)
	}

	greenhouseStatus = http.StatusBadGateway
	if err := handle(ctx, mustPoll(t, ctx, pool, boards.ID)); err == nil {
		t.Fatal("failed greenhouse poll returned nil")
	}
	if workflowJob(t, ctx, pool, boards.ID, strconv.FormatInt(internID, 10)).Status != domain.JobOpen {
		t.Fatal("a failed fetch closed an open job")
	}
	greenhouseStatus = http.StatusOK
	greenhouseBody = `{"jobs":[` + greenhouseJob(internID, "Backend Intern", "Example", description) + `,{"id":9,"title":""}]}`
	if err := handle(ctx, mustPoll(t, ctx, pool, boards.ID)); err != nil {
		t.Fatalf("partial poll error = %v", err)
	}
	if workflowJob(t, ctx, pool, boards.ID, strconv.FormatInt(otherID, 10)).Status != domain.JobOpen {
		t.Fatal("a bad posting closed a missing job")
	}
	greenhouseBody = `{"jobs":[` + greenhouseJob(internID, "Backend Intern", "Example", description) + `]}`
	if err := handle(ctx, mustPoll(t, ctx, pool, boards.ID)); err != nil {
		t.Fatalf("clean poll error = %v", err)
	}
	if workflowJob(t, ctx, pool, boards.ID, strconv.FormatInt(otherID, 10)).Status != domain.JobClosed {
		t.Fatal("a complete board left a missing job open")
	}
	if workflowJob(t, ctx, pool, boards.ID, strconv.FormatInt(internID, 10)).Status != domain.JobOpen {
		t.Fatal("the job still on the board was closed")
	}

	changed := match.DefaultRules()
	changed.Threshold = 101
	second := pipeline.Handler(
		pool,
		source.Greenhouse{}, source.Lever{}, source.Career{}, source.Ashby{}, source.SmartRecruiters{}, nil,
		pipeline.Scoring{Rules: changed, Extractor: extractor, Model: "workflow-model", MinChars: 80, MaxChars: 12000},
		userID, slog.New(slog.DiscardHandler),
	)
	if err := second(ctx, matchTask(intern, profile.Version)); err != nil {
		t.Fatalf("rescore error = %v", err)
	}
	if extractCalls != 1 {
		t.Fatalf("extractor calls after a weight change = %d, want 1", extractCalls)
	}
	if _, err := pool.FindEvaluationByKey(ctx, intern.ID, profile.Version, intern.ContentHash, extract.PromptVersion, "workflow-model"); err != nil {
		t.Fatalf("old evaluation error = %v", err)
	}
	if _, err := pool.FindEvaluationByKey(ctx, intern.ID, profile.Version, intern.ContentHash, extract.PromptVersion+"+30.30.20.10.10.t101", "workflow-model"); err != nil {
		t.Fatalf("new evaluation error = %v", err)
	}

	if err := pool.MarkSourceChecked(ctx, feeds.ID, time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatalf("MarkSourceChecked() error = %v", err)
	}
	finishWorkflowTasks(t, databaseURL, []string{feeds.ID})
	if err := scheduler.EnqueueDue(ctx); err != nil {
		t.Fatalf("EnqueueDue() error = %v", err)
	}
	if _, err := pool.ActivePoll(ctx, feeds.ID); err == nil {
		t.Fatal("a career source was due two hours after its last check")
	}
	if err := pool.MarkSourceChecked(ctx, feeds.ID, time.Now().Add(-7*time.Hour)); err != nil {
		t.Fatalf("MarkSourceChecked() error = %v", err)
	}
	if err := scheduler.EnqueueDue(ctx); err != nil {
		t.Fatalf("EnqueueDue() error = %v", err)
	}
	if _, err := pool.ActivePoll(ctx, feeds.ID); err != nil {
		t.Fatalf("career poll after six hours error = %v", err)
	}

	ready := httptest.NewServer(health.Handler(pool, slog.New(slog.DiscardHandler)))
	t.Cleanup(ready.Close)
	for _, path := range []string{"/health", "/ready"} {
		response, err := ready.Client().Get(ready.URL + path)
		if err != nil {
			t.Fatalf("GET %s error = %v", path, err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %s", path, response.Status)
		}
		response.Body.Close()
	}

	evalsBefore := countEvaluations(t, databaseURL, intern.ID)
	root := filepath.Join("..", "..")
	binary := filepath.Join(t.TempDir(), "scout")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build error = %v\n%s", err, output)
	}
	missing := exec.Command(binary, "compare")
	missing.Dir = root
	missing.Env = compareEnv(databaseURL, "")
	if err := missing.Run(); err == nil {
		t.Fatal("compare without a proposed weight exited 0")
	}
	run := exec.Command(binary, "compare")
	run.Dir = root
	run.Env = compareEnv(databaseURL, "101")
	output, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("compare error = %v\n%s", err, output)
	}
	text := string(output)
	if !strings.Contains(text, "labeled ") || !strings.Contains(text, "proposed version rules-v1+30.30.20.10.10.t101") {
		t.Fatalf("compare output = %s", text)
	}
	if countEvaluations(t, databaseURL, intern.ID) != evalsBefore {
		t.Fatal("compare wrote an evaluation")
	}
	if got, err := pool.GetProfile(ctx, profile.ID); err != nil || got.Version != versionBefore {
		t.Fatalf("profile after compare = %+v error = %v", got, err)
	}
}

func greenhouseJob(id int64, title, company, content string) string {
	return `{"id":` + strconv.FormatInt(id, 10) + `,"title":` + strconv.Quote(title) +
		`,"company_name":` + strconv.Quote(company) +
		`,"absolute_url":"https://boards.example/jobs/` + strconv.FormatInt(id, 10) +
		`","content":` + strconv.Quote(content) + `,"location":{"name":"Remote, Europe"}}`
}

func upsertWorkflowSource(t *testing.T, ctx context.Context, pool *store.Pool, src domain.JobSource) domain.JobSource {
	t.Helper()
	if err := pool.UpsertSource(ctx, &src); err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	return src
}

func workflowJob(t *testing.T, ctx context.Context, pool *store.Pool, sourceID, externalID string) domain.Job {
	t.Helper()
	ref, err := pool.FindReferenceByExternalID(ctx, sourceID, externalID)
	if err != nil {
		t.Fatalf("FindReferenceByExternalID(%s) error = %v", externalID, err)
	}
	job, err := pool.GetJob(ctx, ref.JobID)
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	return job
}

func mustPoll(t *testing.T, ctx context.Context, pool *store.Pool, sourceID string) domain.Task {
	t.Helper()
	if err := pool.EnqueuePoll(ctx, sourceID); err != nil {
		t.Fatalf("EnqueuePoll() error = %v", err)
	}
	poll, err := pool.ActivePoll(ctx, sourceID)
	if err != nil {
		t.Fatalf("ActivePoll() error = %v", err)
	}
	return poll
}

func mustSources(t *testing.T, ctx context.Context, pool *store.Pool) []domain.JobSource {
	t.Helper()
	sources, err := pool.ListSources(ctx)
	if err != nil {
		t.Fatalf("ListSources() error = %v", err)
	}
	return sources
}

func finishWorkflowTasks(t *testing.T, databaseURL string, sourceIDs []string) {
	t.Helper()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("sql open: %v", err)
	}
	defer db.Close()
	for _, id := range sourceIDs {
		if _, err := db.Exec(`UPDATE processing_jobs SET status = 'completed', updated_at = now() WHERE source_id = $1::uuid AND status IN ('pending', 'processing')`, id); err != nil {
			t.Fatalf("finish tasks: %v", err)
		}
		if _, err := db.Exec(`
			UPDATE processing_jobs SET status = 'completed', updated_at = now()
			WHERE status IN ('pending', 'processing')
			  AND job_id IN (SELECT job_id FROM job_source_references WHERE source_id = $1::uuid)`, id); err != nil {
			t.Fatalf("finish matches: %v", err)
		}
	}
}

func countEvaluations(t *testing.T, databaseURL, jobID string) int {
	t.Helper()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("sql open: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM job_evaluations WHERE job_id = $1::uuid`, jobID).Scan(&n); err != nil {
		t.Fatalf("count evaluations: %v", err)
	}
	return n
}

func compareEnv(databaseURL, threshold string) []string {
	var env []string
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "SOURCES=") || strings.HasPrefix(entry, "PROPOSED_MATCH_") || strings.HasPrefix(entry, "TELEGRAM_TOKEN=") || strings.HasPrefix(entry, "LLM_API_KEY=") {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "DATABASE_URL="+databaseURL, "SOURCES=")
	if threshold != "" {
		env = append(env, "PROPOSED_MATCH_THRESHOLD="+threshold)
	}
	return env
}

type onlyWorkflowSources struct {
	*store.Pool
	allow map[string]bool
}

func (s onlyWorkflowSources) ListSources(ctx context.Context) ([]domain.JobSource, error) {
	sources, err := s.Pool.ListSources(ctx)
	if err != nil {
		return nil, err
	}
	kept := make([]domain.JobSource, 0, len(s.allow))
	for _, src := range sources {
		if s.allow[src.ID] {
			kept = append(kept, src)
		}
	}
	return kept, nil
}

type workflowMailbox struct {
	messages []source.MailMessage
}

func (m *workflowMailbox) Fetch(context.Context, string) ([]source.MailMessage, error) {
	return m.messages, nil
}

type workflowOpener struct {
	box source.Mailbox
}

func (o workflowOpener) Open(context.Context, string) (source.Mailbox, error) {
	return o.box, nil
}

const remoteAlert = `From: LinkedIn Job Alerts <jobalerts-noreply@linkedin.com>
Subject: alert
Date: Fri, 13 Nov 2026 09:01:00 +0000
Content-Type: text/plain; charset=UTF-8

Backend Intern
Example
Remote
View job: https://www.linkedin.com/comm/jobs/view/555000111?trk=eml-test&utm_source=job_alert
`
