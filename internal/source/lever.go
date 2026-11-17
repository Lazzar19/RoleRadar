package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

const (
	defaultLeverBase = "https://api.lever.co"
	leverPageSize    = 100
	leverMaxPages    = 50
	leverTimeout     = 30 * time.Second
)

// Lever calls the public postings API. The site slug is not a secret.
type Lever struct {
	BaseURL  string
	Client   *http.Client
	PageSize int
}

// Fetch reads every page of one Lever site.
// A short page ends the snapshot. A transport or HTTP failure is not a partial snapshot.
// Stopping at the page cap leaves the snapshot incomplete so missing jobs are not closed.
func (l Lever) Fetch(ctx context.Context, site string) (Snapshot, error) {
	site = strings.TrimSpace(site)
	if site == "" || strings.ContainsAny(site, "/?#") {
		return Snapshot{}, fmt.Errorf("lever: site is required")
	}
	pageSize := l.PageSize
	if pageSize <= 0 {
		pageSize = leverPageSize
	}
	var snapshot Snapshot
	for page := 0; page < leverMaxPages; page++ {
		batch, err := l.page(ctx, site, page*pageSize, pageSize)
		if err != nil {
			return Snapshot{}, err
		}
		for _, raw := range batch {
			posting, itemErr := decodeLeverPosting(raw)
			if itemErr != nil {
				snapshot.ItemErrors = append(snapshot.ItemErrors, *itemErr)
				continue
			}
			snapshot.Postings = append(snapshot.Postings, posting)
		}
		if len(batch) < pageSize {
			snapshot.Complete = true
			return snapshot, nil
		}
	}
	return snapshot, nil
}

func (l Lever) page(ctx context.Context, site string, skip, limit int) ([]json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, leverTimeout)
	defer cancel()
	endpoint, err := l.pageURL(site, skip, limit)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("lever: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ScoutAI")
	resp, err := l.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("lever: request site: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("lever: read site: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lever: site returned %s", resp.Status)
	}
	var batch []json.RawMessage
	if err := json.Unmarshal(body, &batch); err != nil || batch == nil {
		return nil, fmt.Errorf("lever: postings list is missing")
	}
	return batch, nil
}

func (l Lever) pageURL(site string, skip, limit int) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(l.BaseURL), "/")
	if base == "" {
		base = defaultLeverBase
	}
	joined, err := url.JoinPath(base, "v0", "postings", site)
	if err != nil {
		return "", fmt.Errorf("lever: build site url: %w", err)
	}
	parsed, err := url.Parse(joined)
	if err != nil {
		return "", fmt.Errorf("lever: parse site url: %w", err)
	}
	query := parsed.Query()
	query.Set("mode", "json")
	query.Set("skip", fmt.Sprintf("%d", skip))
	query.Set("limit", fmt.Sprintf("%d", limit))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (l Lever) httpClient() *http.Client {
	if l.Client != nil {
		return l.Client
	}
	return &http.Client{Timeout: leverTimeout, CheckRedirect: sameHostRedirect}
}

type leverPosting struct {
	ID               string        `json:"id"`
	Text             string        `json:"text"`
	Categories       leverCategory `json:"categories"`
	DescriptionPlain string        `json:"descriptionPlain"`
	Lists            []leverList   `json:"lists"`
	AdditionalPlain  string        `json:"additionalPlain"`
	HostedURL        string        `json:"hostedUrl"`
	WorkplaceType    string        `json:"workplaceType"`
	CreatedAt        int64         `json:"createdAt"`
}

type leverCategory struct {
	Commitment   string   `json:"commitment"`
	Location     string   `json:"location"`
	AllLocations []string `json:"allLocations"`
}

type leverList struct {
	Text    string `json:"text"`
	Content string `json:"content"`
}

func decodeLeverPosting(raw json.RawMessage) (DiscoveredPosting, *ItemError) {
	var job leverPosting
	if err := json.Unmarshal(raw, &job); err != nil {
		return DiscoveredPosting{}, &ItemError{Message: "posting could not be decoded"}
	}
	externalID := strings.TrimSpace(job.ID)
	if externalID == "" || strings.TrimSpace(job.Text) == "" || strings.TrimSpace(job.HostedURL) == "" {
		return DiscoveredPosting{}, &ItemError{ExternalID: externalID, Message: "posting is missing an id, title, or url"}
	}
	description := leverDescription(job)
	if description == "" {
		return DiscoveredPosting{}, &ItemError{ExternalID: externalID, Message: "description was not in the board response"}
	}
	posting := DiscoveredPosting{
		ExternalID:     &externalID,
		Kind:           domain.KindListing,
		Completeness:   domain.CompletenessFull,
		Title:          collapseSpace(job.Text),
		OriginalURL:    job.HostedURL,
		CanonicalURL:   job.HostedURL,
		Description:    description,
		Locations:      leverLocations(job),
		EmploymentType: collapseSpace(job.Categories.Commitment),
	}
	if job.CreatedAt > 0 {
		posted := time.UnixMilli(job.CreatedAt).UTC()
		posting.PostedAt = &posted
	}
	return posting, nil
}

func leverDescription(job leverPosting) string {
	var parts []string
	if opening := strings.TrimSpace(job.DescriptionPlain); opening != "" {
		parts = append(parts, opening)
	}
	for _, list := range job.Lists {
		heading := collapseSpace(list.Text)
		body := htmlText(list.Content)
		switch {
		case heading != "" && body != "":
			parts = append(parts, heading+": "+body)
		case body != "":
			parts = append(parts, body)
		case heading != "":
			parts = append(parts, heading)
		}
	}
	if extra := strings.TrimSpace(job.AdditionalPlain); extra != "" {
		parts = append(parts, extra)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func leverLocations(job leverPosting) []domain.Location {
	remote := strings.EqualFold(job.WorkplaceType, "remote")
	hybrid := strings.EqualFold(job.WorkplaceType, "hybrid")
	var raws []string
	seen := map[string]bool{}
	add := func(raw string) {
		raw = collapseSpace(raw)
		if raw == "" {
			return
		}
		key := strings.ToLower(raw)
		if seen[key] {
			return
		}
		seen[key] = true
		raws = append(raws, raw)
	}
	add(job.Categories.Location)
	for _, raw := range job.Categories.AllLocations {
		add(raw)
	}
	if len(raws) == 0 && (remote || hybrid) {
		raw := "remote"
		if hybrid {
			raw = "hybrid"
		}
		return []domain.Location{{Raw: raw, Remote: remote, Hybrid: hybrid}}
	}
	locations := make([]domain.Location, 0, len(raws))
	for _, raw := range raws {
		folded := strings.ToLower(raw)
		locations = append(locations, domain.Location{
			Raw:    raw,
			Remote: remote || strings.Contains(folded, "remote"),
			Hybrid: hybrid || strings.Contains(folded, "hybrid"),
		})
	}
	return locations
}

func sameHostRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 || req.URL.Host == via[0].URL.Host {
		return nil
	}
	return fmt.Errorf("redirect left the source host")
}
