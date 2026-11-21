package pipeline

import (
	"testing"

	"github.com/Lazzar19/ScoutAI/internal/domain"
	"github.com/Lazzar19/ScoutAI/internal/store"
)

func TestKeepRoleNotificationPrefersSerbia(t *testing.T) {
	serbia := []domain.Location{{Raw: "Belgrade, Serbia"}}
	europe := []domain.Location{{Raw: "Munich, Germany"}}
	pendingEurope := []store.SameRoleNotification{{
		ID:        "europe",
		Status:    domain.DeliveryPending,
		Locations: europe,
	}}

	keep, cancel := keepRoleNotification(serbia, nil)
	if !keep || len(cancel) != 0 {
		t.Fatalf("first posting keep=%v cancel=%v", keep, cancel)
	}
	keep, cancel = keepRoleNotification(europe, []store.SameRoleNotification{{
		ID: "serbia", Status: domain.DeliverySent, Locations: serbia,
	}})
	if keep || len(cancel) != 0 {
		t.Fatalf("europe after Serbia keep=%v cancel=%v", keep, cancel)
	}
	keep, cancel = keepRoleNotification(serbia, pendingEurope)
	if !keep || len(cancel) != 1 || cancel[0] != "europe" {
		t.Fatalf("Serbia replacing Europe keep=%v cancel=%v", keep, cancel)
	}
	keep, cancel = keepRoleNotification(serbia, []store.SameRoleNotification{{
		ID: "sent-europe", Status: domain.DeliverySent, Locations: europe,
	}})
	if keep {
		t.Fatal("a second copy was kept after one was already sent")
	}
	keep, _ = keepRoleNotification(europe, pendingEurope)
	if keep {
		t.Fatal("a second European copy was kept")
	}
}
