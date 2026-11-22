package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Lazzar19/RoleRadar/internal/domain"
	"github.com/Lazzar19/RoleRadar/prompts"
)

// TokenLedger records how many tokens one UTC day has already used.
type TokenLedger interface {
	ExtractorTokens(ctx context.Context, day time.Time) (int, error)
	AddExtractorTokens(ctx context.Context, day time.Time, n int) error
}

// Client is one OpenAI-compatible chat provider.
// Extract makes one HTTP call. The queue attempt cap is the retry cap.
type Client struct {
	BaseURL     string
	APIKey      string
	Model       string
	MaxChars    int
	DailyTokens int
	Budget      TokenLedger
	HTTP        *http.Client
	Logger      *slog.Logger
}

type chatRequest struct {
	Model          string            `json:"model"`
	Temperature    float64           `json:"temperature"`
	ResponseFormat map[string]string `json:"response_format"`
	Messages       []chatMessage     `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Extract sends the capped posting and returns validated facts.
// The request does not include a profile, a rule score, or a notification threshold.
func (c Client) Extract(ctx context.Context, job domain.Job) (Extraction, error) {
	if strings.TrimSpace(c.APIKey) == "" {
		return Extraction{}, fmt.Errorf("extractor API key is empty")
	}
	model := strings.TrimSpace(c.Model)
	if model == "" {
		model = DefaultModel
	}
	day := time.Now().UTC()
	if c.Budget != nil && c.DailyTokens > 0 {
		used, err := c.Budget.ExtractorTokens(ctx, day)
		if err != nil {
			return Extraction{}, err
		}
		if used >= c.DailyTokens {
			return Extraction{}, ErrBudget
		}
	}

	posting := PostingText(job, c.MaxChars)
	payload, err := json.Marshal(chatRequest{
		Model:          model,
		Temperature:    0,
		ResponseFormat: map[string]string{"type": "json_object"},
		Messages: []chatMessage{
			{Role: "system", Content: prompts.Text},
			{Role: "user", Content: posting},
		},
	})
	if err != nil {
		return Extraction{}, fmt.Errorf("encode extractor request: %w", err)
	}
	endpoint, err := url.JoinPath(c.baseURL(), "chat/completions")
	if err != nil {
		return Extraction{}, fmt.Errorf("extractor endpoint: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Extraction{}, fmt.Errorf("extractor request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "RoleRadar")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Extraction{}, fmt.Errorf("extractor request failed")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Extraction{}, fmt.Errorf("extractor response could not be read")
	}
	if resp.StatusCode != http.StatusOK {
		c.log().Warn("extractor request failed", "job_id", job.ID, "model", model, "status", resp.StatusCode)
		return Extraction{}, fmt.Errorf("extractor request failed with status %d", resp.StatusCode)
	}

	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Choices) == 0 {
		return Extraction{}, fmt.Errorf("extractor response has no message")
	}
	content := envelope.Choices[0].Message.Content
	tokens := envelope.Usage.PromptTokens + envelope.Usage.CompletionTokens
	if tokens <= 0 {
		tokens = (len(posting) + len(content)) / 4
		if tokens < 1 {
			tokens = 1
		}
	}
	if c.Budget != nil {
		if err := c.Budget.AddExtractorTokens(ctx, day, tokens); err != nil {
			return Extraction{}, err
		}
	}
	c.log().Info("extractor response", "job_id", job.ID, "model", model, "tokens", tokens)
	return Parse([]byte(content), posting)
}

func (c Client) baseURL() string {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		return "https://api.openai.com/v1"
	}
	return base
}

func (c Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 45 * time.Second, CheckRedirect: refuseRedirect}
}

func refuseRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

func (c Client) log() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.New(slog.DiscardHandler)
}
