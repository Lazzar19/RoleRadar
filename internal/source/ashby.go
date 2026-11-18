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
	defaultAshbyBase = "https://api.ashbyhq.com"
	ashbyTimeout     = 30 * time.Second
)

// Ashby calls the public job-board API. The board name is not a secret.
// One response is the whole board. There is no paging.
type Ashby struct {
	BaseURL string
	Client  *http.Client
}

// Fetch reads one board. A transport or HTTP failure is not a partial snapshot.
// A body that fills the read limit is rejected so a truncated board is not stored.
func (a Ashby) Fetch(ctx context.Context, board string) (Snapshot, error) {
	board = strings.TrimSpace(board)
	if board == "" || strings.ContainsAny(board, "/?#") {
		return Snapshot{}, fmt.Errorf("ashby: board name is required")
	}
	ctx, cancel := context.WithTimeout(ctx, ashbyTimeout)
	defer cancel()
	endpoint, err := a.boardURL(board)
	if err != nil {
		return Snapshot{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Snapshot{}, fmt.Errorf("ashby: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ScoutAI")
	resp, err := a.httpClient().Do(req)
	if err != nil {
		return Snapshot{}, fmt.Errorf("ashby: request board: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return Snapshot{}, fmt.Errorf("ashby: read board: %w", err)
	}
	if len(body) == 16<<20 {
		return Snapshot{}, fmt.Errorf("ashby: board is larger than 16MB")
	}
	if resp.StatusCode != http.StatusOK {
		return Snapshot{}, fmt.Errorf("ashby: board returned %s", resp.Status)
	}
	var payload struct {
		Jobs []json.RawMessage `json:"jobs"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Jobs == nil {
		return Snapshot{}, fmt.Errorf("ashby: jobs list is missing")
	}
	snapshot := Snapshot{Complete: true}
	company := collapseSpace(board)
	for _, raw := range payload.Jobs {
		posting, skip, itemErr := decodeAshbyJob(raw, company)
		if skip {
			continue
		}
		if itemErr != nil {
			snapshot.ItemErrors = append(snapshot.ItemErrors, *itemErr)
			continue
		}
		snapshot.Postings = append(snapshot.Postings, posting)
	}
	return snapshot, nil
}

func (a Ashby) boardURL(board string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(a.BaseURL), "/")
	if base == "" {
		base = defaultAshbyBase
	}
	joined, err := url.JoinPath(base, "posting-api", "job-board", board)
	if err != nil {
		return "", fmt.Errorf("ashby: build board url: %w", err)
	}
	return joined, nil
}

func (a Ashby) httpClient() *http.Client {
	if a.Client != nil {
		return a.Client
	}
	return &http.Client{Timeout: ashbyTimeout, CheckRedirect: sameHostRedirect}
}

type ashbyJob struct {
	ID                 string           `json:"id"`
	Title              string           `json:"title"`
	Location           string           `json:"location"`
	SecondaryLocations []ashbySecondary `json:"secondaryLocations"`
	IsListed           *bool            `json:"isListed"`
	WorkplaceType      string           `json:"workplaceType"`
	DescriptionPlain   string           `json:"descriptionPlain"`
	PublishedAt        string           `json:"publishedAt"`
	EmploymentType     string           `json:"employmentType"`
	Address            ashbyAddress     `json:"address"`
	JobURL             string           `json:"jobUrl"`
	ApplyURL           string           `json:"applyUrl"`
}

type ashbySecondary struct {
	Location string      `json:"location"`
	Address  ashbyPostal `json:"address"`
}

type ashbyAddress struct {
	PostalAddress ashbyPostal `json:"postalAddress"`
}

type ashbyPostal struct {
	AddressLocality string `json:"addressLocality"`
	AddressRegion   string `json:"addressRegion"`
	AddressCountry  string `json:"addressCountry"`
}

func decodeAshbyJob(raw json.RawMessage, company string) (DiscoveredPosting, bool, *ItemError) {
	var job ashbyJob
	if err := json.Unmarshal(raw, &job); err != nil {
		return DiscoveredPosting{}, false, &ItemError{Message: "job could not be decoded"}
	}
	if job.IsListed != nil && !*job.IsListed {
		return DiscoveredPosting{}, true, nil
	}
	externalID := strings.TrimSpace(job.ID)
	page := strings.TrimSpace(job.JobURL)
	if page == "" {
		page = strings.TrimSpace(job.ApplyURL)
	}
	if externalID == "" || strings.TrimSpace(job.Title) == "" || page == "" {
		return DiscoveredPosting{}, false, &ItemError{ExternalID: externalID, Message: "posting is missing an id, title, or url"}
	}
	description := strings.TrimSpace(job.DescriptionPlain)
	if description == "" {
		return DiscoveredPosting{}, false, &ItemError{ExternalID: externalID, Message: "description was not in the board response"}
	}
	original := strings.TrimSpace(job.ApplyURL)
	if original == "" {
		original = page
	}
	posting := DiscoveredPosting{
		ExternalID:     &externalID,
		Kind:           domain.KindListing,
		Completeness:   domain.CompletenessFull,
		Title:          collapseSpace(job.Title),
		OriginalURL:    original,
		CanonicalURL:   page,
		Description:    description,
		Locations:      ashbyLocations(job),
		EmploymentType: collapseSpace(job.EmploymentType),
	}
	if company != "" {
		posting.Company = &company
	}
	if posted, err := time.Parse(time.RFC3339, strings.TrimSpace(job.PublishedAt)); err == nil {
		utc := posted.UTC()
		posting.PostedAt = &utc
	}
	return posting, false, nil
}

func ashbyLocations(job ashbyJob) []domain.Location {
	remote := strings.EqualFold(job.WorkplaceType, "remote")
	hybrid := strings.EqualFold(job.WorkplaceType, "hybrid")
	var raws []string
	add := func(raw string) {
		raw = collapseSpace(raw)
		if raw == "" {
			return
		}
		raws = append(raws, raw)
	}
	add(job.Location)
	add(ashbyPlace(job.Address.PostalAddress))
	for _, extra := range job.SecondaryLocations {
		add(extra.Location)
		add(ashbyPlace(extra.Address))
	}
	if len(raws) == 0 && (remote || hybrid) {
		raw := "remote"
		if hybrid {
			raw = "hybrid"
		}
		return []domain.Location{{Raw: raw, Remote: remote, Hybrid: hybrid}}
	}
	seen := map[string]bool{}
	var locations []domain.Location
	for _, raw := range raws {
		key := strings.ToLower(raw)
		if seen[key] {
			continue
		}
		seen[key] = true
		folded := strings.ToLower(raw)
		locations = append(locations, domain.Location{
			Raw:    raw,
			Remote: remote || strings.Contains(folded, "remote"),
			Hybrid: hybrid || strings.Contains(folded, "hybrid"),
		})
	}
	return locations
}

func ashbyPlace(postal ashbyPostal) string {
	var parts []string
	for _, part := range []string{postal.AddressLocality, postal.AddressRegion, postal.AddressCountry} {
		part = collapseSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, ", ")
}
