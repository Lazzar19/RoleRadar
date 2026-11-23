package notify

import (
	"fmt"
	"html"
	"net/url"
	"strings"
	"time"

	"github.com/Lazzar19/RoleRadar/internal/domain"
	"github.com/Lazzar19/RoleRadar/internal/match"
)

// PreviewText is the HTML preview. It has no score.
func PreviewText(job domain.Job, sourceName, originalURL string) string {
	return strings.Join([]string{
		"PREVIEW — description not in the source",
		"",
		linkedTitle(job.Title, originalURL),
		"Company: " + html.EscapeString(companyLine(job)),
		"Location: " + html.EscapeString(locationLine(job.Locations)),
		"",
		html.EscapeString(match.PostingSummary(job.Title, job.Description, job.EmploymentType, job.Experience)),
		"Posted: " + postedLine(job.PostedAt),
		"",
		"Open the link to read the posting.",
		"Reply to this message with the text to score it.",
		"",
		"Source: " + html.EscapeString(orUnknown(sourceName)),
		postingLink(originalURL),
	}, "\n")
}

// EvaluatedText is the HTML scored message. The number is the rule score.
func EvaluatedText(job domain.Job, score int, sourceName, originalURL string) string {
	return strings.Join([]string{
		fmt.Sprintf("MATCH %d/100", score),
		"",
		linkedTitle(job.Title, originalURL),
		"Company: " + html.EscapeString(companyLine(job)),
		"Location: " + html.EscapeString(locationLine(job.Locations)),
		"",
		html.EscapeString(match.PostingSummary(job.Title, job.Description, job.EmploymentType, job.Experience)),
		"Posted: " + postedLine(job.PostedAt),
		"",
		"Source: " + html.EscapeString(orUnknown(sourceName)),
		postingLink(originalURL),
	}, "\n")
}

func linkedTitle(title, rawURL string) string {
	title = html.EscapeString(strings.TrimSpace(title))
	href, ok := postingHref(rawURL)
	if !ok {
		return title
	}
	return `<a href="` + href + `">` + title + `</a>`
}

func postingLink(rawURL string) string {
	href, ok := postingHref(rawURL)
	if !ok {
		return "Open posting: unavailable"
	}
	return `<a href="` + href + `">Open posting</a>`
}

func postingHref(rawURL string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil {
		return "", false
	}
	return html.EscapeString(parsed.String()), true
}

// FeedbackButtons are the four choices stored as feedback. They do not change weights.
func FeedbackButtons(notificationID string) [][]Button {
	return [][]Button{
		{
			{Text: "Interesting", Data: "fb:interesting:" + notificationID},
			{Text: "Not relevant", Data: "fb:not_relevant:" + notificationID},
		},
		{
			{Text: "Save for later", Data: "fb:saved_for_later:" + notificationID},
			{Text: "Already applied", Data: "fb:already_applied:" + notificationID},
		},
	}
}

func postedLine(posted *time.Time) string {
	if posted == nil || posted.IsZero() {
		return "not stated"
	}
	return posted.UTC().Format("2006-01-02")
}

func companyLine(job domain.Job) string {
	if job.Company == nil {
		return "Unknown"
	}
	company := strings.TrimSpace(*job.Company)
	if company == "" {
		return "Unknown"
	}
	return company
}

func locationLine(locations []domain.Location) string {
	var parts []string
	for _, location := range locations {
		if line := formatLocation(location); line != "" {
			parts = append(parts, line)
		}
	}
	if len(parts) == 0 {
		return "Unknown"
	}
	return strings.Join(parts, "; ")
}

func formatLocation(location domain.Location) string {
	raw := strings.TrimSpace(location.Raw)
	if raw == "" && location.City != nil {
		raw = strings.TrimSpace(*location.City)
	}
	if raw == "" && location.Country != nil {
		raw = strings.TrimSpace(*location.Country)
	}
	if raw == "" && location.Remote {
		return "Remote"
	}
	if raw == "" {
		return ""
	}
	if location.Remote && !strings.Contains(strings.ToLower(raw), "remote") {
		return "Remote / " + raw
	}
	return raw
}

func alertSourceName(name, rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return name
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case host == "helloworld.rs" || strings.HasSuffix(host, ".helloworld.rs"):
		return "HelloWorld"
	case host == "linkedin.com" || strings.HasSuffix(host, ".linkedin.com"):
		return "LinkedIn"
	default:
		return name
	}
}

func orUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Unknown"
	}
	return value
}

func profileText(profile domain.CandidateProfile) string {
	return strings.Join([]string{
		fmt.Sprintf("Profile: %s (version %d)", profile.Name, profile.Version),
		"",
		"Roles: " + listOrNone(profile.PreferredRoles),
		"Synonyms: " + listOrNone(profile.RoleSynonyms),
		"Skills: " + listOrNone(append(append([]string{}, profile.PreferredSkills...), profile.AdditionalSkills...)),
		"Locations: " + locationLine(profile.PreferredLocations),
		"Preferred companies: " + listOrNone(profile.PreferredCompanies),
		"Excluded companies: " + listOrNone(profile.ExcludedCompanies),
	}, "\n")
}

func statusText(openJobs, pending int, profile *domain.CandidateProfile) string {
	name := "none"
	if profile != nil {
		name = fmt.Sprintf("%s (version %d)", profile.Name, profile.Version)
	}
	return fmt.Sprintf("Open jobs: %d\nPending notifications: %d\nProfile: %s", openJobs, pending, name)
}

func sourcesText(sources []domain.JobSource) string {
	if len(sources) == 0 {
		return "No sources are configured."
	}
	lines := make([]string, 0, len(sources)*2)
	for _, source := range sources {
		state := "disabled"
		if source.Enabled {
			state = "enabled"
		}
		lines = append(lines, fmt.Sprintf("%s — %s, %s", source.Name, source.Type, state))
		if source.LastChecked == nil {
			lines = append(lines, "last check: never")
			continue
		}
		lines = append(lines, "last check: "+source.LastChecked.UTC().Format("2006-01-02 15:04 UTC"))
	}
	return strings.Join(lines, "\n")
}

func listOrNone(values []string) string {
	var cleaned []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}
	if len(cleaned) == 0 {
		return "none"
	}
	return strings.Join(cleaned, ", ")
}

const helpText = `RoleRadar watches for internships and sends a preview or a scored match.

/profile shows the active profile.
/status shows the queue.
/sources lists boards.
/add <url> saves a LinkedIn, HelloWorld, or configured board link and asks for the text.
Reply to a preview with the posting text to score it.
Use the buttons on a message to save feedback.`
