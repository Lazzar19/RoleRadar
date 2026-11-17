package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/config"
	"github.com/Lazzar19/ScoutAI/internal/domain"
	"github.com/Lazzar19/ScoutAI/internal/extract"
	"github.com/Lazzar19/ScoutAI/internal/health"
	"github.com/Lazzar19/ScoutAI/internal/match"
	"github.com/Lazzar19/ScoutAI/internal/notify"
	"github.com/Lazzar19/ScoutAI/internal/pipeline"
	"github.com/Lazzar19/ScoutAI/internal/schedule"
	"github.com/Lazzar19/ScoutAI/internal/source"
	"github.com/Lazzar19/ScoutAI/internal/store"
	"github.com/Lazzar19/ScoutAI/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if len(os.Args) > 1 && os.Args[1] == "compare" {
		if err := compareRules(context.Background()); err != nil {
			logger.Error("compare rules", "err", err)
			os.Exit(1)
		}
		return
	}
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration is invalid", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg, logger); err != nil {
		logger.Error("process stopped", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	logger.Info("applying migrations")
	if err := store.Migrate(ctx, cfg.DatabaseURL, logger); err != nil {
		return err
	}

	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := upsertSources(ctx, pool, cfg.Sources); err != nil {
		return err
	}

	workerCtx, cancelWorkers := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer func() {
		cancelWorkers()
		wg.Wait()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = schedule.Scheduler{
			Store:           pool,
			Interval:        cfg.GreenhouseInterval,
			Jitter:          cfg.GreenhouseJitter,
			MailboxInterval: cfg.MailboxInterval,
			MailboxJitter:   cfg.MailboxJitter,
			CareerInterval:  cfg.CareerInterval,
			CareerJitter:    cfg.CareerJitter,
			Logger:          logger,
		}.Run(workerCtx)
	}()

	boards := source.Greenhouse{BaseURL: cfg.GreenhouseBaseURL}
	rules := match.Rules{
		Weights: match.Weights{
			Role:       cfg.MatchWeightRole,
			Skills:     cfg.MatchWeightSkills,
			Experience: cfg.MatchWeightExperience,
			Location:   cfg.MatchWeightLocation,
			Company:    cfg.MatchWeightCompany,
		},
		Threshold: cfg.MatchThreshold,
	}
	scoring := pipeline.Scoring{
		Rules:    rules,
		Model:    cfg.LLMModel,
		MinChars: cfg.ExtractMinChars,
		MaxChars: cfg.ExtractMaxChars,
	}
	if cfg.LLMAPIKey != "" {
		scoring.Extractor = extract.Client{
			BaseURL:     cfg.LLMAPIBase,
			APIKey:      cfg.LLMAPIKey,
			Model:       cfg.LLMModel,
			MaxChars:    cfg.ExtractMaxChars,
			DailyTokens: cfg.ExtractDailyTokens,
			Budget:      pool,
			Logger:      logger,
		}
	}
	logger.Info("starting queue workers", "count", cfg.WorkerCount)
	for i := range cfg.WorkerCount {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := worker.ID(i)
			err := worker.Worker{
				Queue:       pool,
				Handler:     pipeline.Handler(pool, boards, source.Lever{BaseURL: cfg.LeverBaseURL}, source.Career{}, source.EnvMailboxes{}, scoring, cfg.NotifyRecipient, logger),
				ID:          id,
				TaskTimeout: cfg.TaskTimeout,
				MaxAttempts: cfg.TaskMaxAttempts,
				Poll:        cfg.WorkerPoll,
				RetryBase:   cfg.TaskRetryBase,
				Logger:      logger,
			}.Run(workerCtx)
			if err != nil {
				logger.Error("worker stopped", "worker", id, "err", err)
			}
		}(i)
	}

	if cfg.TelegramToken != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := (&notify.Bot{
				Token:     cfg.TelegramToken,
				UserID:    cfg.NotifyRecipient,
				BaseURL:   cfg.TelegramAPIBase,
				Store:     pool,
				Preview:   cfg.NotifyPreview,
				Evaluated: cfg.NotifyEvaluated,
				Logger:    logger,
			}).Run(workerCtx)
			if err != nil && workerCtx.Err() == nil {
				logger.Error("telegram stopped", "err", err)
			}
		}()
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           health.Handler(pool, logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.HTTPAddr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func upsertSources(ctx context.Context, pool *store.Pool, sources []config.ConfiguredSource) error {
	for _, spec := range sources {
		src := domain.JobSource{
			Name:    spec.Name,
			Type:    domain.SourceType(spec.Type),
			Enabled: spec.Enabled,
			Config: domain.SourceConfig{
				BoardToken: spec.BoardToken,
				Site:       spec.Site,
				FeedURL:    spec.FeedURL,
				SecretEnv:  spec.SecretEnv,
			},
		}
		if err := pool.UpsertSource(ctx, &src); err != nil {
			return err
		}
	}
	return nil
}

func compareRules(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	current := match.Rules{
		Weights: match.Weights{
			Role:       cfg.MatchWeightRole,
			Skills:     cfg.MatchWeightSkills,
			Experience: cfg.MatchWeightExperience,
			Location:   cfg.MatchWeightLocation,
			Company:    cfg.MatchWeightCompany,
		},
		Threshold: cfg.MatchThreshold,
	}
	proposed, ok, err := proposedRules(current)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("set at least one PROPOSED_MATCH_WEIGHT_* value or PROPOSED_MATCH_THRESHOLD")
	}
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	rows, err := pool.ListLabeled(ctx)
	if err != nil {
		return err
	}
	examples := make([]match.Example, 0, len(rows))
	for _, row := range rows {
		label, ok := relevanceLabel(row.Feedback)
		if !ok {
			continue
		}
		example := match.Example{Job: row.Job, Profile: row.Profile, Label: label}
		if len(row.Facts) > 0 {
			var facts domain.Extraction
			if err := json.Unmarshal(row.Facts, &facts); err == nil {
				example.Facts = &facts
			}
		}
		examples = append(examples, example)
	}
	before := match.Score(examples, current)
	after := match.Score(examples, proposed)
	fmt.Printf("labeled %d\n", before.Examples)
	fmt.Printf("current relevant notified %d/%d\n", before.RelevantNotified, before.Relevant)
	fmt.Printf("current not relevant notified %d/%d\n", before.NotRelevantNotified, before.NotRelevant)
	fmt.Printf("proposed relevant notified %d/%d\n", after.RelevantNotified, after.Relevant)
	fmt.Printf("proposed not relevant notified %d/%d\n", after.NotRelevantNotified, after.NotRelevant)
	fmt.Printf("current version %s\n", current.Version())
	fmt.Printf("proposed version %s\n", proposed.Version())
	return nil
}

func proposedRules(current match.Rules) (match.Rules, bool, error) {
	keys := []string{
		"PROPOSED_MATCH_WEIGHT_ROLE",
		"PROPOSED_MATCH_WEIGHT_SKILLS",
		"PROPOSED_MATCH_WEIGHT_EXPERIENCE",
		"PROPOSED_MATCH_WEIGHT_LOCATION",
		"PROPOSED_MATCH_WEIGHT_COMPANY",
		"PROPOSED_MATCH_THRESHOLD",
	}
	set := false
	for _, key := range keys {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			set = true
			break
		}
	}
	if !set {
		return match.Rules{}, false, nil
	}
	role, err := proposedInt("PROPOSED_MATCH_WEIGHT_ROLE", current.Weights.Role)
	if err != nil {
		return match.Rules{}, false, err
	}
	skills, err := proposedInt("PROPOSED_MATCH_WEIGHT_SKILLS", current.Weights.Skills)
	if err != nil {
		return match.Rules{}, false, err
	}
	experience, err := proposedInt("PROPOSED_MATCH_WEIGHT_EXPERIENCE", current.Weights.Experience)
	if err != nil {
		return match.Rules{}, false, err
	}
	location, err := proposedInt("PROPOSED_MATCH_WEIGHT_LOCATION", current.Weights.Location)
	if err != nil {
		return match.Rules{}, false, err
	}
	company, err := proposedInt("PROPOSED_MATCH_WEIGHT_COMPANY", current.Weights.Company)
	if err != nil {
		return match.Rules{}, false, err
	}
	threshold, err := proposedInt("PROPOSED_MATCH_THRESHOLD", current.Threshold)
	if err != nil {
		return match.Rules{}, false, err
	}
	return match.Rules{
		Weights:   match.Weights{Role: role, Skills: skills, Experience: experience, Location: location, Company: company},
		Threshold: threshold,
	}, true, nil
}

func proposedInt(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", key)
	}
	return n, nil
}

func relevanceLabel(kind domain.FeedbackType) (match.Label, bool) {
	switch kind {
	case domain.FeedbackInteresting:
		return match.LabelRelevant, true
	case domain.FeedbackNotRelevant:
		return match.LabelNotRelevant, true
	default:
		return "", false
	}
}
