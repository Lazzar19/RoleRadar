package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
	"github.com/Lazzar19/ScoutAI/internal/source"
	"github.com/Lazzar19/ScoutAI/internal/store"
)

func runMailbox(ctx context.Context, db PollStore, mail source.MailOpener, logger *slog.Logger, src domain.JobSource) error {
	if mail == nil {
		return fmt.Errorf("source %s has no mailbox", src.Name)
	}
	started := time.Now().UTC()
	runID, err := db.StartSourceRun(ctx, src.ID, started)
	if err != nil {
		return err
	}
	box, err := mail.Open(ctx, src.Config.SecretEnv)
	if err != nil {
		_ = db.FinishSourceRun(ctx, runID, "failed", 0, 0, err.Error(), time.Now().UTC())
		_ = db.MarkSourceChecked(ctx, src.ID, time.Now().UTC())
		return err
	}
	discovered, stored, note, ingestErr := ingestMailbox(ctx, db, src, box, logger)
	finished := time.Now().UTC()
	if ingestErr != nil {
		_ = db.FinishSourceRun(ctx, runID, "failed", discovered, stored, ingestErr.Error(), finished)
		_ = db.MarkSourceChecked(ctx, src.ID, finished)
		return ingestErr
	}
	if err := db.FinishSourceRun(ctx, runID, "succeeded", discovered, stored, note, finished); err != nil {
		return err
	}
	if err := db.MarkSourceChecked(ctx, src.ID, finished); err != nil {
		return err
	}
	logger.Info("mailbox messages stored", "source", src.Name, "stored", stored)
	return nil
}

// ingestMailbox stores each new email, then its preview listings.
// A parser error is stored on that email and the cursor still advances.
// A fetch or database error leaves the cursor on the last email that was stored.
func ingestMailbox(ctx context.Context, db PollStore, src domain.JobSource, box source.Mailbox, logger *slog.Logger) (int, int, string, error) {
	messages, err := box.Fetch(ctx, src.Cursor)
	if err != nil {
		return 0, 0, "", err
	}
	sort.Slice(messages, func(i, j int) bool {
		return lessMessageID(messages[i].ID, messages[j].ID)
	})
	var discovered, stored, parseFailures int
	for _, msg := range messages {
		if strings.TrimSpace(msg.ID) == "" {
			return discovered, stored, "", fmt.Errorf("mailbox message has no provider id")
		}
		if _, err := db.FindRawMessage(ctx, src.ID, msg.ID); err == nil {
			if err := db.SetSourceCursor(ctx, src.ID, msg.ID, time.Now().UTC()); err != nil {
				return discovered, stored, "", err
			}
			continue
		} else if !errors.Is(err, store.ErrNotFound) {
			return discovered, stored, "", err
		}

		postings, parseErr := source.ParseLinkedInAlert(msg.Raw)
		received := msg.ReceivedAt.UTC()
		if received.IsZero() {
			received = time.Now().UTC()
		}
		if parseErr != nil {
			raw := &domain.RawMessage{
				SourceID:          src.ID,
				ProviderMessageID: msg.ID,
				ReceivedAt:        received,
				ParseStatus:       source.MessageFailed,
				ParseError:        parseErr.Error(),
				RawBody:           string(msg.Raw),
			}
			if err := db.InsertRawMessage(ctx, raw); err != nil {
				return discovered, stored, "", err
			}
			if err := db.SetSourceCursor(ctx, src.ID, msg.ID, time.Now().UTC()); err != nil {
				return discovered, stored, "", err
			}
			parseFailures++
			logger.Info("mailbox message could not be parsed", "source", src.Name, "message", msg.ID)
			continue
		}

		result, err := Apply(ctx, db, src, source.Snapshot{Postings: postings, Complete: false}, time.Now().UTC())
		if err != nil {
			return discovered, stored, "", err
		}
		raw := &domain.RawMessage{
			SourceID:          src.ID,
			ProviderMessageID: msg.ID,
			ReceivedAt:        received,
			ParseStatus:       source.MessageParsed,
			RawBody:           string(msg.Raw),
		}
		if err := db.InsertRawMessage(ctx, raw); err != nil {
			return discovered, stored, "", err
		}
		if err := db.SetSourceCursor(ctx, src.ID, msg.ID, time.Now().UTC()); err != nil {
			return discovered, stored, "", err
		}
		discovered += len(postings)
		stored += result.Stored
	}
	note := ""
	if parseFailures > 0 {
		note = fmt.Sprintf("%d messages could not be parsed", parseFailures)
	}
	return discovered, stored, note, nil
}

func lessMessageID(left, right string) bool {
	a, aErr := strconv.ParseUint(left, 10, 64)
	b, bErr := strconv.ParseUint(right, 10, 64)
	if aErr == nil && bErr == nil {
		return a < b
	}
	return left < right
}
