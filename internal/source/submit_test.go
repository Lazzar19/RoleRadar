package source

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

func TestAcceptURLAllowsLinkedInAndConfiguredBoards(t *testing.T) {
	transport := &rejectTransport{}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })

	linkedin, err := AcceptURL("https://www.linkedin.com/posts/example?utm_source=share", nil)
	if err != nil || linkedin == "" {
		t.Fatalf("AcceptURL(linkedin) = %q, %v", linkedin, err)
	}
	if _, err := AcceptURL("https://www.helloworld.rs/posao/example", nil); err != nil {
		t.Fatalf("AcceptURL(helloworld) error = %v", err)
	}
	if _, err := AcceptURL("https://helloworld.rs.evil.test/posao/1", nil); !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("helloworld lookalike error = %v", err)
	}
	if _, err := AcceptURL("https://boards.greenhouse.io/acme/jobs/1", nil); !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("greenhouse without a source error = %v", err)
	}
	if _, err := AcceptURL("https://jobs.lever.co/acme/1", nil); !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("lever without a source error = %v", err)
	}
	if _, err := AcceptURL("https://example.com/jobs/1", nil); !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("example.com error = %v", err)
	}
	if _, err := AcceptURL("https://linkedin.com.evil.test/posts/1", nil); !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("lookalike host error = %v", err)
	}
	if _, err := AcceptURL("https://user:secret@www.linkedin.com/posts/1", nil); !errors.Is(err, ErrUserInfo) {
		t.Fatalf("userinfo error = %v", err)
	}
	if _, err := AcceptURL("javascript:alert(1)", nil); !errors.Is(err, ErrBadURL) {
		t.Fatalf("scheme error = %v", err)
	}

	disabled := []domain.JobSource{{
		Name: "greenhouse-off", Type: domain.SourceGreenhouse, Enabled: false,
		Config: domain.SourceConfig{BoardToken: "acme"},
	}}
	if _, err := AcceptURL("https://job-boards.greenhouse.io/acme/jobs/1", disabled); !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("disabled greenhouse error = %v", err)
	}

	enabled := []domain.JobSource{
		{Name: "greenhouse-on", Type: domain.SourceGreenhouse, Enabled: true, Config: domain.SourceConfig{BoardToken: "acme"}},
		{Name: "lever-on", Type: domain.SourceLever, Enabled: true, Config: domain.SourceConfig{Site: "acme"}},
	}
	if _, err := AcceptURL("https://boards.greenhouse.io/acme/jobs/1?gh_src=abc", enabled); err != nil {
		t.Fatalf("AcceptURL(greenhouse) error = %v", err)
	}
	if _, err := AcceptURL("https://jobs.lever.co/acme/123", enabled); err != nil {
		t.Fatalf("AcceptURL(lever) error = %v", err)
	}
	if transport.calls != 0 {
		t.Fatalf("allowlist made %d HTTP requests", transport.calls)
	}
}
