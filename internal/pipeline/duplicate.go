package pipeline

import (
	"github.com/Lazzar19/RoleRadar/internal/domain"
	"github.com/Lazzar19/RoleRadar/internal/match"
	"github.com/Lazzar19/RoleRadar/internal/store"
)

// keepRoleNotification reports whether this posting should be sent.
// A Serbia posting replaces a pending copy outside Serbia.
// Any other copy is dropped when one notification already exists.
func keepRoleNotification(locations []domain.Location, existing []store.SameRoleNotification) (bool, []string) {
	if len(existing) == 0 {
		return true, nil
	}
	if !match.LocatedInSerbia(locations) {
		return false, nil
	}
	for _, item := range existing {
		if item.Status == domain.DeliverySent || match.LocatedInSerbia(item.Locations) {
			return false, nil
		}
	}
	cancel := make([]string, 0, len(existing))
	for _, item := range existing {
		if item.Status == domain.DeliveryPending {
			cancel = append(cancel, item.ID)
		}
	}
	return true, cancel
}
