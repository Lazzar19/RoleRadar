package pipeline

import (
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
	"github.com/Lazzar19/ScoutAI/internal/match"
)

// eveningHour is when postings outside Serbia are sent, in Europe/Belgrade.
const eveningHour = 20

// SendAfter is nil when a posting should be sent now.
// Serbia, and a posting with no location, are sent now.
// Any other known location waits until 20:00 Europe/Belgrade.
// A posting found after that hour waits until the next day.
func SendAfter(locations []domain.Location, now time.Time) *time.Time {
	if len(locations) == 0 || match.LocatedInSerbia(locations) {
		return nil
	}
	at := nextEvening(now)
	return &at
}

func nextEvening(now time.Time) time.Time {
	zone := belgrade
	local := now.In(zone)
	evening := time.Date(local.Year(), local.Month(), local.Day(), eveningHour, 0, 0, 0, zone)
	if !local.Before(evening) {
		evening = evening.Add(24 * time.Hour)
	}
	return evening.UTC()
}

var belgrade = loadBelgrade()

func loadBelgrade() *time.Location {
	zone, err := time.LoadLocation("Europe/Belgrade")
	if err != nil {
		return time.FixedZone("CET", 3600)
	}
	return zone
}
