// Package housekeeping holds cleaning tasks, room status and maintenance tickets (spec 03 and 01).
// Nothing here reads a guest's name, contact details or prices: housekeeping must never see them.
package housekeeping

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/ids"
)

var (
	ErrNotFound      = errors.New("housekeeping: not found")
	ErrBad           = errors.New("housekeeping: bad input")
	ErrAlreadyClosed = errors.New("housekeeping: already closed")
)

// EnsureCheckoutTask makes sure the room has an open cleaning task for the day. Calling it again changes nothing.
func EnsureCheckoutTask(ctx context.Context, tx pgx.Tx, orgID, propertyID, roomID uuid.UUID, date, now time.Time) error {
	_, err := tx.Exec(ctx,
		`insert into housekeeping_task (id, organization_id, property_id, room_id, date, created_at) values ($1, $2, $3, $4, $5, $6)
		 on conflict (room_id, date, kind) where status <> 'done' do nothing`, ids.New(), orgID, propertyID, roomID, date, now)
	return err
}
