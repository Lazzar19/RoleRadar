package store

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestExtractorTokensAccumulateForOneDay(t *testing.T) {
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

	day := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	for range 40 {
		n, err := pool.ExtractorTokens(ctx, day)
		if err != nil {
			t.Fatalf("ExtractorTokens() error = %v", err)
		}
		if n == 0 {
			break
		}
		day = day.AddDate(0, 0, 1)
	}
	other := day.AddDate(0, 0, 1)
	if n, err := pool.ExtractorTokens(ctx, day); err != nil || n != 0 {
		t.Fatalf("tokens = %d error = %v", n, err)
	}
	if err := pool.AddExtractorTokens(ctx, day, 10); err != nil {
		t.Fatalf("AddExtractorTokens() error = %v", err)
	}
	if err := pool.AddExtractorTokens(ctx, day, 5); err != nil {
		t.Fatalf("AddExtractorTokens() error = %v", err)
	}
	if n, err := pool.ExtractorTokens(ctx, day); err != nil || n != 15 {
		t.Fatalf("tokens = %d error = %v", n, err)
	}
	if n, err := pool.ExtractorTokens(ctx, other); err != nil || n != 0 {
		t.Fatalf("other day = %d error = %v", n, err)
	}
}
