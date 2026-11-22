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

	"github.com/Lazzar19/RoleRadar/internal/domain"
)

const careerTimeout = 30 * time.Second

// Career reads one JSON feed that an operator configured.
// It does not follow links and it does not read an HTML career site.
type Career struct {
	Client *http.Client
}

// Fetch requests the feed URL once.
// A missing jobs list is a failed fetch. An empty list is a complete snapshot.
func (c Career) Fetch(ctx context.Context, feedURL string) (Snapshot, error) {
	parsed, err := careerURL(feedURL)
	if err != nil {
		return Snapshot{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, careerTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return Snapshot{}, fmt.Errorf("career: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "RoleRadar")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Snapshot{}, fmt.Errorf("career: request feed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return Snapshot{}, fmt.Errorf("career: read feed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Snapshot{}, fmt.Errorf("career: feed returned %s", resp.Status)
	}
	if kind := resp.Header.Get("Content-Type"); strings.Contains(strings.ToLower(kind), "html") {
		return Snapshot{}, fmt.Errorf("career: feed is not json")
	}
	var payload struct {
		Jobs []json.RawMessage `json:"jobs"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Jobs == nil {
		return Snapshot{}, fmt.Errorf("career: jobs list is missing")
	}
	snapshot := Snapshot{Complete: true}
	for _, raw := range payload.Jobs {
		posting, itemErr := decodeCareerJob(raw)
		if itemErr != nil {
			snapshot.ItemErrors = append(snapshot.ItemErrors, *itemErr)
			continue
		}
		snapshot.Postings = append(snapshot.Postings, posting)
	}
	return snapshot, nil
}

func (c Career) httpClient() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: careerTimeout, CheckRedirect: sameHostRedirect}
}

func careerURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil {
		return nil, fmt.Errorf("career: feed url must be https and have no user info")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	loopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if parsed.User != nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopback)) {
		return nil, fmt.Errorf("career: feed url must be https and have no user info")
	}
	if host == "helloworld.rs" || strings.HasSuffix(host, ".helloworld.rs") || host == "linkedin.com" || strings.HasSuffix(host, ".linkedin.com") {
		return nil, fmt.Errorf("career: that host is not a configured json feed")
	}
	return parsed, nil
}

type careerJob struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	URL            string `json:"url"`
	Description    string `json:"description"`
	Company        string `json:"company"`
	Location       string `json:"location"`
	EmploymentType string `json:"employment_type"`
	PostedAt       string `json:"posted_at"`
}

func decodeCareerJob(raw json.RawMessage) (DiscoveredPosting, *ItemError) {
	var job careerJob
	if err := json.Unmarshal(raw, &job); err != nil {
		return DiscoveredPosting{}, &ItemError{Message: "job could not be decoded"}
	}
	externalID := strings.TrimSpace(job.ID)
	title := collapseSpace(job.Title)
	description := htmlText(job.Description)
	if description == "" {
		description = collapseSpace(job.Description)
	}
	page, err := url.Parse(strings.TrimSpace(job.URL))
	if title == "" || err != nil || page.Hostname() == "" || (page.Scheme != "https" && page.Scheme != "http") || description == "" {
		return DiscoveredPosting{}, &ItemError{ExternalID: externalID, Message: "job is missing a title, url, or description"}
	}
	posting := DiscoveredPosting{
		Kind:           domain.KindListing,
		Completeness:   domain.CompletenessFull,
		Title:          title,
		OriginalURL:    job.URL,
		CanonicalURL:   job.URL,
		Description:    description,
		EmploymentType: collapseSpace(job.EmploymentType),
	}
	if externalID != "" {
		posting.ExternalID = &externalID
	}
	if company := collapseSpace(job.Company); company != "" {
		posting.Company = &company
	}
	if location := collapseSpace(job.Location); location != "" {
		folded := strings.ToLower(location)
		posting.Locations = []domain.Location{{
			Raw:    location,
			Remote: strings.Contains(folded, "remote"),
			Hybrid: strings.Contains(folded, "hybrid"),
		}}
	}
	if posted := strings.TrimSpace(job.PostedAt); posted != "" {
		when, err := time.Parse(time.RFC3339, posted)
		if err != nil {
			return DiscoveredPosting{}, &ItemError{ExternalID: externalID, Message: "posted_at could not be read"}
		}
		utc := when.UTC()
		posting.PostedAt = &utc
	}
	return posting, nil
}
