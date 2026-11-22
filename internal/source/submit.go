package source

import (
	"errors"
	"net/url"
	"strings"

	"github.com/Lazzar19/RoleRadar/internal/domain"
)

const (
	// ManualSourceName is the source row for Telegram /add submissions.
	ManualSourceName = "manual"
	// SubmittedTitle is the title stored until the user pastes the posting text.
	SubmittedTitle = "Submitted post"
)

var (
	// ErrBadURL means the argument is not an absolute http(s) URL.
	ErrBadURL = errors.New("submission url is invalid")
	// ErrUserInfo means the URL carries a username or password.
	ErrUserInfo = errors.New("submission url includes user info")
	// ErrHostNotAllowed means the host is outside the allowlist.
	ErrHostNotAllowed = errors.New("submission host is not allowed")
)

// AcceptURL checks a submitted URL against the allowlist and does not fetch it.
// linkedin.com and helloworld.rs are always allowed. Greenhouse and Lever hosts are allowed when an enabled source of that type is configured.
// helloworld.rs has no documented API or feed, so a submitted URL is stored and not requested.
func AcceptURL(raw string, sources []domain.JobSource) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return "", ErrBadURL
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return "", ErrBadURL
	}
	if parsed.User != nil {
		return "", ErrUserInfo
	}
	if !hostAllowed(parsed.Hostname(), submissionHosts(sources)) {
		return "", ErrHostNotAllowed
	}
	return raw, nil
}

func submissionHosts(sources []domain.JobSource) []string {
	hosts := []string{"linkedin.com", "helloworld.rs"}
	var greenhouse, lever, ashby, smart bool
	for _, src := range sources {
		if !src.Enabled {
			continue
		}
		switch src.Type {
		case domain.SourceGreenhouse:
			greenhouse = true
		case domain.SourceLever:
			lever = true
		case domain.SourceAshby:
			ashby = true
		case domain.SourceSmartRecruiters:
			smart = true
		}
	}
	if greenhouse {
		hosts = append(hosts, "boards.greenhouse.io", "job-boards.greenhouse.io")
	}
	if lever {
		hosts = append(hosts, "jobs.lever.co")
	}
	if ashby {
		hosts = append(hosts, "jobs.ashbyhq.com")
	}
	if smart {
		hosts = append(hosts, "jobs.smartrecruiters.com")
	}
	return hosts
}

func hostAllowed(host string, allowed []string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" {
		return false
	}
	for _, name := range allowed {
		name = strings.TrimSuffix(strings.ToLower(name), ".")
		if host == name || strings.HasSuffix(host, "."+name) {
			return true
		}
	}
	return false
}
