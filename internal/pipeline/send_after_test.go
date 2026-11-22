package pipeline

import (
	"testing"
	"time"

	"github.com/Lazzar19/RoleRadar/internal/domain"
)

func TestSendAfterKeepsSerbiaImmediate(t *testing.T) {
	now := time.Date(2026, 11, 18, 16, 0, 0, 0, time.UTC)
	if SendAfter(nil, now) != nil {
		t.Fatal("a posting with no location was delayed")
	}
	if SendAfter([]domain.Location{{Raw: "Belgrade, Serbia"}}, now) != nil {
		t.Fatal("a Serbia posting was delayed")
	}
	europe := SendAfter([]domain.Location{{Raw: "Berlin, Germany"}}, now)
	usa := SendAfter([]domain.Location{{Raw: "Remote", Remote: true}}, now)
	if europe == nil || usa == nil {
		t.Fatal("a posting outside Serbia was immediate")
	}
	want := time.Date(2026, 11, 18, 19, 0, 0, 0, time.UTC)
	if !europe.Equal(want) || !usa.Equal(want) {
		t.Fatalf("europe = %s usa = %s", europe.UTC(), usa.UTC())
	}
	late := time.Date(2026, 11, 18, 19, 30, 0, 0, time.UTC)
	next := SendAfter([]domain.Location{{Raw: "New York, United States"}}, late)
	if next == nil || !next.Equal(time.Date(2026, 11, 19, 19, 0, 0, 0, time.UTC)) {
		t.Fatalf("next evening = %v", next)
	}
}
