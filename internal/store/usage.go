package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ExtractorTokens is the recorded model usage for one UTC day.
// A missing row is zero, not an error.
func (p *Pool) ExtractorTokens(ctx context.Context, day time.Time) (int, error) {
	var tokens int
	err := p.pool.QueryRow(ctx, `
		SELECT tokens
		FROM extractor_usage
		WHERE usage_day = $1::date`,
		day.UTC().Format("2006-01-02"),
	).Scan(&tokens)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("extractor tokens: %w", err)
	}
	return tokens, nil
}

// AddExtractorTokens adds a completed model call to that UTC day's budget.
func (p *Pool) AddExtractorTokens(ctx context.Context, day time.Time, n int) error {
	if n == 0 {
		return nil
	}
	if n < 0 {
		return fmt.Errorf("extractor tokens: a call cannot return tokens")
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO extractor_usage (usage_day, tokens)
		VALUES ($1::date, $2)
		ON CONFLICT (usage_day) DO UPDATE
		SET tokens = extractor_usage.tokens + EXCLUDED.tokens`,
		day.UTC().Format("2006-01-02"),
		n,
	)
	if err != nil {
		return fmt.Errorf("add extractor tokens: %w", err)
	}
	return nil
}
