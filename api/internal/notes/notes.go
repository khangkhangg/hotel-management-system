// Package notes holds the team notes on bookings and the tags that notify colleagues (spec 11).
// A note is internal. A housekeeper only ever gets the note's text, the room number and the author.
package notes

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/ids"
	"hotelpms/api/internal/notify"
)

var (
	ErrNotFound         = errors.New("notes: not found")
	ErrBad              = errors.New("notes: bad input")
	ErrCannotTag        = errors.New("notes: that person cannot be tagged")
	ErrGuestName        = errors.New("notes: the note names the guest")
	ErrForbidden        = errors.New("notes: not allowed")
	ErrEditWindowClosed = errors.New("notes: the editing time is over")
	ErrAlreadyRemoved   = errors.New("notes: already removed")
)

const (
	maxBody    = 2000
	editWindow = 5 * time.Minute
)

// Mention tags one colleague by ID, or everyone with a role at the property.
type Mention struct {
	UserID *uuid.UUID `json:"userId"`
	Role   string     `json:"role"`
}

type CreateInput struct {
	Body         string    `json:"body"`
	Mentions     []Mention `json:"mentions"`
	Pinned       bool      `json:"pinned"`
	Acknowledged bool      `json:"acknowledged"` // the author saw the reminder that the note names the guest
	ReplyTo      *string   `json:"replyTo"`
}

var taggableRoles = map[string]bool{"manager": true, "front_desk": true, "revenue": true, "housekeeping": true}

type recipient struct {
	id   uuid.UUID
	role string
}

// recipients expands tags into the people who get notified. A person tagged twice is one person. Only active members of
// the property (and owners, who work everywhere) can be tagged.
func recipients(ctx context.Context, tx pgx.Tx, orgID, propertyID uuid.UUID, mentions []Mention) ([]recipient, error) {
	seen := map[uuid.UUID]bool{}
	var out []recipient
	add := func(id uuid.UUID, role string) {
		if !seen[id] {
			seen[id] = true
			out = append(out, recipient{id, role})
		}
	}
	for _, m := range mentions {
		switch {
		case m.UserID != nil && m.Role == "":
			var role string
			err := tx.QueryRow(ctx,
				`select role from membership where user_id = $1 and organization_id = $2 and status = 'active' and (property_id = $3 or property_id is null) order by property_id nulls last limit 1`,
				*m.UserID, orgID, propertyID).Scan(&role)
			if err != nil {
				return nil, ErrCannotTag
			}
			add(*m.UserID, role)
		case m.UserID == nil && taggableRoles[m.Role]:
			rows, err := tx.Query(ctx, `select distinct user_id from membership where organization_id = $1 and property_id = $2 and role = $3 and status = 'active'`, orgID, propertyID, m.Role)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var id uuid.UUID
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return nil, err
				}
				add(id, m.Role)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return nil, err
			}
		default:
			return nil, ErrBad
		}
	}
	return out, nil
}

// Create writes a note on a reservation and notifies the people it tags, once each. Run it inside db.WithOrg.
func Create(ctx context.Context, tx pgx.Tx, a access.Actor, reservationID uuid.UUID, in CreateInput, now time.Time) (uuid.UUID, error) {
	body := strings.TrimSpace(in.Body)
	if body == "" || len([]rune(body)) > maxBody || len(in.Mentions) > 20 {
		return uuid.Nil, ErrBad
	}
	var code, guest string
	if err := tx.QueryRow(ctx, `select r.code, g.name from reservation r join guest g on g.id = r.guest_id where r.id = $1 and r.property_id = $2`, reservationID, a.PropertyID).Scan(&code, &guest); err != nil {
		return uuid.Nil, ErrNotFound
	}
	people, err := recipients(ctx, tx, a.OrgID, a.PropertyID, in.Mentions)
	if err != nil {
		return uuid.Nil, err
	}
	var tagsHousekeeping bool
	for _, p := range people {
		tagsHousekeeping = tagsHousekeeping || p.role == "housekeeping"
	}
	if tagsHousekeeping && !in.Acknowledged && namesGuest(body, guest) {
		return uuid.Nil, ErrGuestName
	}
	var roomID *uuid.UUID
	var roomNumber string
	if tagsHousekeeping {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `select rm.id, rm.number from reservation_room rr join room rm on rm.id = rr.room_id where rr.reservation_id = $1 and rr.active order by rr.id limit 1`, reservationID).Scan(&id, &roomNumber); err == nil {
			roomID = &id
		}
	}
	id := ids.New()
	if _, err := tx.Exec(ctx,
		`insert into note (id, organization_id, property_id, reservation_id, room_id, author_id, body, pinned, for_housekeeping, created_at) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		id, a.OrgID, a.PropertyID, reservationID, roomID, a.UserID, body, in.Pinned, tagsHousekeeping && roomID != nil, now); err != nil {
		return uuid.Nil, err
	}
	for _, m := range in.Mentions {
		if _, err := tx.Exec(ctx, `insert into note_mention (id, organization_id, note_id, user_id, role) values ($1, $2, $3, $4, nullif($5, ''))`, ids.New(), a.OrgID, id, m.UserID, m.Role); err != nil {
			return uuid.Nil, err
		}
	}
	var author string
	if err := tx.QueryRow(ctx, `select name from app_user where id = $1`, a.UserID).Scan(&author); err != nil {
		return uuid.Nil, err
	}
	for _, p := range people {
		if p.id == a.UserID {
			continue // tagging yourself tells you nothing
		}
		in := notify.Input{OrgID: a.OrgID, PropertyID: a.PropertyID, ToUser: &p.id, Level: notify.ActionNeeded, Key: "note:" + id.String(),
			Title: fmt.Sprintf("%s tagged you on booking %s", author, code), Body: body, ActionURL: "/calendar?reservation=" + reservationID.String(), Now: now}
		if p.role == "housekeeping" {
			// A housekeeper gets the text, the room and the author. Never the guest or the booking.
			in.Title, in.ActionURL = fmt.Sprintf("Note for room %s from %s", orEmpty(roomNumber), author), "/rooms"
		}
		if err := notify.Send(ctx, tx, in); err != nil {
			return uuid.Nil, err
		}
	}
	if _, err := tx.Exec(ctx, `select pg_notify('notes', $1)`, reservationID.String()); err != nil {
		return uuid.Nil, err
	}
	return id, audit.Record(ctx, tx, audit.Entry{Action: "note.created", TargetType: "note", TargetID: id, TargetLabel: code, After: map[string]any{"mentions": len(in.Mentions), "pinned": in.Pinned}})
}

func orEmpty(s string) string {
	if s == "" {
		return "(not assigned)"
	}
	return s
}

// namesGuest reports whether the text contains the guest's full name or any part of it of three letters or more.
func namesGuest(text, guest string) bool {
	t := strings.ToLower(text)
	if strings.Contains(t, strings.ToLower(guest)) {
		return true
	}
	for _, part := range strings.Fields(strings.ToLower(guest)) {
		if len([]rune(part)) >= 3 && strings.Contains(t, part) {
			return true
		}
	}
	return false
}
