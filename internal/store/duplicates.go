package store

import (
	"context"
	"fmt"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

// SameRoleNotification is an earlier outbox row for the same title and company.
type SameRoleNotification struct {
	ID        string
	Status    domain.DeliveryStatus
	Locations []domain.Location
}

// ListSameRoleNotifications returns pending or sent notifications for the same role.
// The same role is the same title and company. An empty company matches the same source only.
func (p *Pool) ListSameRoleNotifications(ctx context.Context, jobID, recipient string) ([]SameRoleNotification, error) {
	rows, err := p.pool.Query(ctx, `
		WITH current AS (
			SELECT j.title, j.company, r.source_id
			FROM jobs j
			JOIN job_source_references r ON r.job_id = j.id
			WHERE j.id = $1::uuid
			ORDER BY r.discovered_at
			LIMIT 1
		)
		SELECT n.id, n.status, j.locations
		FROM notifications n
		JOIN jobs j ON j.id = n.job_id
		JOIN current c ON lower(btrim(j.title)) = lower(btrim(c.title))
		WHERE n.recipient = $2
		  AND n.status IN ('pending', 'sent')
		  AND j.id <> $1::uuid
		  AND (
		    (btrim(coalesce(c.company, '')) <> '' AND lower(btrim(coalesce(j.company, ''))) = lower(btrim(c.company)))
		    OR (
		      btrim(coalesce(c.company, '')) = ''
		      AND btrim(coalesce(j.company, '')) = ''
		      AND EXISTS (
		        SELECT 1
		        FROM job_source_references same
		        WHERE same.job_id = j.id
		          AND same.source_id = c.source_id
		      )
		    )
		  )`, jobID, recipient)
	if err != nil {
		return nil, fmt.Errorf("list same role notifications: %w", err)
	}
	defer rows.Close()
	var found []SameRoleNotification
	for rows.Next() {
		var item SameRoleNotification
		var status string
		var locations []byte
		if err := rows.Scan(&item.ID, &status, &locations); err != nil {
			return nil, fmt.Errorf("list same role notifications: %w", err)
		}
		item.Status = domain.DeliveryStatus(status)
		item.Locations, err = decodeLocations(locations)
		if err != nil {
			return nil, err
		}
		found = append(found, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list same role notifications: %w", err)
	}
	return found, nil
}
