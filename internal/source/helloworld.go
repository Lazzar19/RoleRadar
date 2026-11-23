package source

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Lazzar19/RoleRadar/internal/domain"
)

var (
	helloAnchorPattern = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	helloHrefPattern   = regexp.MustCompile(`(?is)\bhref\s*=\s*"([^"]+)"`)
	helloIDPattern     = regexp.MustCompile(`/(\d+)/?$`)
)

// ParseHelloWorldAlert reads one HelloWorld saved-search email into preview listings.
// The posting page is not fetched. A card with no title is an error.
func ParseHelloWorldAlert(raw []byte) ([]DiscoveredPosting, error) {
	_, htmlBody, posted, err := splitMessage(raw)
	if err != nil {
		return nil, err
	}
	matches := helloAnchorPattern.FindAllStringSubmatchIndex(htmlBody, -1)
	var cards [][]int
	for _, match := range matches {
		attrs := anchorGroup(htmlBody, match, 1)
		if !strings.Contains(strings.ToLower(attrs), "card-title") || helloHrefPattern.FindStringSubmatch(attrs) == nil {
			continue
		}
		cards = append(cards, match)
	}
	if len(cards) == 0 {
		return nil, nil
	}
	var postings []DiscoveredPosting
	seen := map[string]bool{}
	for i, match := range cards {
		attrs := anchorGroup(htmlBody, match, 1)
		hrefMatch := helloHrefPattern.FindStringSubmatch(attrs)
		href := ""
		if len(hrefMatch) > 1 {
			href = strings.TrimSpace(html.UnescapeString(hrefMatch[1]))
		}
		title := strings.Join(htmlLines(anchorGroup(htmlBody, match, 2)), " ")
		if strings.TrimSpace(title) == "" {
			return nil, fmt.Errorf("helloworld alert listing had no title")
		}
		id := helloJobID(href)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		limit := len(htmlBody)
		if i+1 < len(cards) {
			limit = cards[i+1][0]
		}
		var follow []string
		for _, line := range htmlLines(htmlBody[match[1]:limit]) {
			if line == title {
				continue
			}
			follow = append(follow, line)
			if len(follow) == 2 {
				break
			}
		}
		company, location := "", ""
		if len(follow) > 0 {
			company = follow[0]
		}
		if len(follow) > 1 {
			location = follow[1]
		}
		postings = append(postings, helloPosting(id, title, company, location, href, posted))
	}
	return postings, nil
}

func helloJobID(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "helloworld.rs" && !strings.HasSuffix(host, ".helloworld.rs") {
		return ""
	}
	if !strings.Contains(strings.ToLower(parsed.Path), "/posao/") {
		return ""
	}
	match := helloIDPattern.FindStringSubmatch(parsed.Path)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func helloPosting(id, title, company, location, original string, posted *time.Time) DiscoveredPosting {
	jobID := id
	canonical := canonicalHelloURL(original)
	posting := DiscoveredPosting{
		ExternalID:   &jobID,
		OriginalURL:  strings.TrimSpace(original),
		CanonicalURL: canonical,
		Kind:         domain.KindListing,
		Completeness: domain.CompletenessPreview,
		Title:        title,
		PostedAt:     posted,
	}
	if cleaned := collapseSpace(company); cleaned != "" {
		posting.Company = &cleaned
	}
	if cleaned := collapseSpace(location); cleaned != "" {
		posting.Locations = []domain.Location{{Raw: cleaned}}
	}
	return posting
}

func canonicalHelloURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" {
		return strings.TrimSpace(raw)
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Scheme = "https"
	return parsed.String()
}
