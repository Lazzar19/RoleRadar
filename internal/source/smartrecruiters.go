package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

const (
	defaultSmartRecruitersBase = "https://api.smartrecruiters.com"
	smartPageSize              = 100
	smartMaxPages              = 20
	smartTimeout               = 30 * time.Second
)

// SmartRecruiters calls the public postings API. The company identifier is not a secret.
// The list has no description. Each posting is read again for the text and the public URL.
type SmartRecruiters struct {
	BaseURL  string
	Client   *http.Client
	PageSize int
}

// Fetch reads every public page, then each posting.
// A short page ends the list. Stopping at the page cap leaves the snapshot incomplete.
// A failed detail stays an item error and the snapshot is not complete, so missing jobs are not closed.
func (s SmartRecruiters) Fetch(ctx context.Context, company string) (Snapshot, error) {
	company = strings.TrimSpace(company)
	if company == "" || strings.ContainsAny(company, "/?#") {
		return Snapshot{}, fmt.Errorf("smartrecruiters: company identifier is required")
	}
	pageSize := s.PageSize
	if pageSize <= 0 {
		pageSize = smartPageSize
	}
	var listed []smartListItem
	complete := false
	for page := 0; page < smartMaxPages; page++ {
		batch, total, err := s.page(ctx, company, page*pageSize, pageSize)
		if err != nil {
			return Snapshot{}, err
		}
		listed = append(listed, batch...)
		if len(batch) < pageSize || len(listed) >= total {
			complete = true
			break
		}
	}
	snapshot := Snapshot{Complete: complete}
	for _, item := range listed {
		posting, itemErr := s.detail(ctx, company, item)
		if itemErr != nil {
			snapshot.ItemErrors = append(snapshot.ItemErrors, *itemErr)
			snapshot.Complete = false
			continue
		}
		snapshot.Postings = append(snapshot.Postings, posting)
	}
	return snapshot, nil
}

func (s SmartRecruiters) page(ctx context.Context, company string, offset, limit int) ([]smartListItem, int, error) {
	endpoint, err := s.listURL(company, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	body, err := s.get(ctx, endpoint)
	if err != nil {
		return nil, 0, err
	}
	var payload struct {
		TotalFound int             `json:"totalFound"`
		Content    []smartListItem `json:"content"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Content == nil {
		return nil, 0, fmt.Errorf("smartrecruiters: postings list is missing")
	}
	return payload.Content, payload.TotalFound, nil
}

func (s SmartRecruiters) detail(ctx context.Context, company string, item smartListItem) (DiscoveredPosting, *ItemError) {
	externalID := strings.TrimSpace(item.ID)
	if externalID == "" || strings.TrimSpace(item.Name) == "" {
		return DiscoveredPosting{}, &ItemError{ExternalID: externalID, Message: "posting is missing an id or title"}
	}
	endpoint, err := s.detailURL(company, externalID)
	if err != nil {
		return DiscoveredPosting{}, &ItemError{ExternalID: externalID, Message: err.Error()}
	}
	body, err := s.get(ctx, endpoint)
	if err != nil {
		return DiscoveredPosting{}, &ItemError{ExternalID: externalID, Message: err.Error()}
	}
	var job smartDetail
	if err := json.Unmarshal(body, &job); err != nil {
		return DiscoveredPosting{}, &ItemError{ExternalID: externalID, Message: "posting could not be decoded"}
	}
	if strings.TrimSpace(job.ID) == "" {
		job.ID = externalID
	}
	if strings.TrimSpace(job.Name) == "" {
		job.Name = item.Name
	}
	return decodeSmartPosting(job, item)
}

func (s SmartRecruiters) get(ctx context.Context, endpoint string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, smartTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("smartrecruiters: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ScoutAI")
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("smartrecruiters: request posting: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("smartrecruiters: read posting: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("smartrecruiters: posting returned %s", resp.Status)
	}
	return body, nil
}

func (s SmartRecruiters) listURL(company string, offset, limit int) (string, error) {
	base := s.base()
	joined, err := url.JoinPath(base, "v1", "companies", company, "postings")
	if err != nil {
		return "", fmt.Errorf("smartrecruiters: build list url: %w", err)
	}
	parsed, err := url.Parse(joined)
	if err != nil {
		return "", fmt.Errorf("smartrecruiters: parse list url: %w", err)
	}
	query := parsed.Query()
	query.Set("limit", fmt.Sprintf("%d", limit))
	query.Set("offset", fmt.Sprintf("%d", offset))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (s SmartRecruiters) detailURL(company, id string) (string, error) {
	joined, err := url.JoinPath(s.base(), "v1", "companies", company, "postings", id)
	if err != nil {
		return "", fmt.Errorf("smartrecruiters: build posting url: %w", err)
	}
	return joined, nil
}

func (s SmartRecruiters) base() string {
	base := strings.TrimRight(strings.TrimSpace(s.BaseURL), "/")
	if base == "" {
		return defaultSmartRecruitersBase
	}
	return base
}

func (s SmartRecruiters) httpClient() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: smartTimeout, CheckRedirect: sameHostRedirect}
}

type smartListItem struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	ReleasedDate     string        `json:"releasedDate"`
	Location         smartLocation `json:"location"`
	TypeOfEmployment smartLabel    `json:"typeOfEmployment"`
	ExperienceLevel  smartLabel    `json:"experienceLevel"`
	Company          smartCompany  `json:"company"`
}

type smartDetail struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	ReleasedDate     string        `json:"releasedDate"`
	Location         smartLocation `json:"location"`
	TypeOfEmployment smartLabel    `json:"typeOfEmployment"`
	ExperienceLevel  smartLabel    `json:"experienceLevel"`
	Company          smartCompany  `json:"company"`
	PostingURL       string        `json:"postingUrl"`
	ApplyURL         string        `json:"applyUrl"`
	JobAd            smartJobAd    `json:"jobAd"`
}

type smartLocation struct {
	City         string `json:"city"`
	Region       string `json:"region"`
	Country      string `json:"country"`
	FullLocation string `json:"fullLocation"`
	Remote       bool   `json:"remote"`
	Hybrid       bool   `json:"hybrid"`
}

type smartLabel struct {
	Label string `json:"label"`
}

type smartCompany struct {
	Name string `json:"name"`
}

type smartJobAd struct {
	Sections map[string]smartSection `json:"sections"`
}

type smartSection struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

func decodeSmartPosting(job smartDetail, listed smartListItem) (DiscoveredPosting, *ItemError) {
	externalID := strings.TrimSpace(job.ID)
	page := strings.TrimSpace(job.PostingURL)
	if page == "" {
		page = strings.TrimSpace(job.ApplyURL)
	}
	if externalID == "" || strings.TrimSpace(job.Name) == "" || page == "" {
		return DiscoveredPosting{}, &ItemError{ExternalID: externalID, Message: "posting is missing an id, title, or url"}
	}
	description := smartDescription(job.JobAd)
	if description == "" {
		return DiscoveredPosting{}, &ItemError{ExternalID: externalID, Message: "description was not in the posting"}
	}
	original := strings.TrimSpace(job.ApplyURL)
	if original == "" {
		original = page
	}
	location := job.Location
	if location.FullLocation == "" && location.City == "" {
		location = listed.Location
	}
	released := job.ReleasedDate
	if strings.TrimSpace(released) == "" {
		released = listed.ReleasedDate
	}
	employment := job.TypeOfEmployment.Label
	if strings.TrimSpace(employment) == "" {
		employment = listed.TypeOfEmployment.Label
	}
	experience := job.ExperienceLevel.Label
	if strings.TrimSpace(experience) == "" {
		experience = listed.ExperienceLevel.Label
	}
	companyName := job.Company.Name
	if strings.TrimSpace(companyName) == "" {
		companyName = listed.Company.Name
	}
	posting := DiscoveredPosting{
		ExternalID:     &externalID,
		Kind:           domain.KindListing,
		Completeness:   domain.CompletenessFull,
		Title:          collapseSpace(job.Name),
		OriginalURL:    original,
		CanonicalURL:   page,
		Description:    description,
		Locations:      smartLocations(location),
		EmploymentType: collapseSpace(employment),
		Experience:     collapseSpace(experience),
	}
	if company := collapseSpace(companyName); company != "" {
		posting.Company = &company
	}
	if posted, err := time.Parse(time.RFC3339, strings.TrimSpace(released)); err == nil {
		utc := posted.UTC()
		posting.PostedAt = &utc
	}
	return posting, nil
}

func smartDescription(ad smartJobAd) string {
	if len(ad.Sections) == 0 {
		return ""
	}
	order := []string{"jobDescription", "qualifications", "additionalInformation", "companyDescription"}
	seen := map[string]bool{}
	var parts []string
	add := func(key string) {
		section, ok := ad.Sections[key]
		if !ok || seen[key] {
			return
		}
		seen[key] = true
		text := htmlText(section.Text)
		title := collapseSpace(section.Title)
		switch {
		case title != "" && text != "":
			parts = append(parts, title+": "+text)
		case text != "":
			parts = append(parts, text)
		}
	}
	for _, key := range order {
		add(key)
	}
	var rest []string
	for key := range ad.Sections {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	for _, key := range rest {
		add(key)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func smartLocations(location smartLocation) []domain.Location {
	raw := collapseSpace(location.FullLocation)
	if raw == "" {
		var parts []string
		for _, part := range []string{location.City, location.Region, location.Country} {
			part = collapseSpace(part)
			if part != "" {
				parts = append(parts, part)
			}
		}
		raw = strings.Join(parts, ", ")
	}
	if raw == "" && !location.Remote && !location.Hybrid {
		return nil
	}
	if raw == "" {
		raw = "remote"
		if location.Hybrid {
			raw = "hybrid"
		}
	}
	folded := strings.ToLower(raw)
	return []domain.Location{{
		Raw:    raw,
		Remote: location.Remote || strings.Contains(folded, "remote"),
		Hybrid: location.Hybrid || strings.Contains(folded, "hybrid"),
	}}
}
