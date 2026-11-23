package staff

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/session"
)

type staffRow struct {
	ID           uuid.UUID  `json:"id"`
	Name         string     `json:"name"`
	Email        string     `json:"email"`
	Role         string     `json:"role"`
	PropertyID   *uuid.UUID `json:"propertyId"`
	PropertyName string     `json:"propertyName"`
	LastSignInAt *time.Time `json:"lastSignInAt"`
	Status       string     `json:"status"`
}

const allProperties = "All properties"

// list shows the people in the property named in X-Property-Id, plus the organization's owners for an owner,
// and the invites still waiting. An owner who names no property sees the whole organization.
func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	ctx, a := r.Context(), access.ActorFrom(r.Context())
	var property *uuid.UUID
	if a.PropertyID != uuid.Nil {
		property = &a.PropertyID
	}
	isOwner := a.Role == access.Owner
	rows := []staffRow{}
	err := db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		people, err := tx.Query(ctx,
			`select u.id, u.name, u.email, m.role, m.property_id, coalesce(p.name, ''), u.last_sign_in_at at time zone 'utc', m.status
			 from membership m join app_user u on u.id = m.user_id left join property p on p.id = m.property_id
			 where m.organization_id = $1
			   and ($2::uuid is null or m.property_id = $2 or m.property_id is null)
			   and ($3 or m.role <> 'owner')
			 order by (m.role = 'owner') desc, p.name nulls first, u.email`, a.OrgID, property, isOwner)
		if err != nil {
			return err
		}
		for people.Next() {
			var s staffRow
			if err := people.Scan(&s.ID, &s.Name, &s.Email, &s.Role, &s.PropertyID, &s.PropertyName, &s.LastSignInAt, &s.Status); err != nil {
				people.Close()
				return err
			}
			rows = append(rows, s)
		}
		people.Close()
		if err := people.Err(); err != nil {
			return err
		}
		invites, err := tx.Query(ctx,
			`select i.id, i.email, i.role, i.property_id, coalesce(p.name, '')
			 from invite i left join property p on p.id = i.property_id
			 where i.organization_id = $1 and i.accepted_at is null and i.expires_at > $4
			   and ($2::uuid is null or i.property_id = $2 or i.property_id is null)
			   and ($3 or i.role <> 'owner')
			 order by i.created_at`, a.OrgID, property, isOwner, h.Clock())
		if err != nil {
			return err
		}
		defer invites.Close()
		for invites.Next() {
			s := staffRow{Status: "invited"}
			if err := invites.Scan(&s.ID, &s.Email, &s.Role, &s.PropertyID, &s.PropertyName); err != nil {
				return err
			}
			rows = append(rows, s)
		}
		return invites.Err()
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	for i := range rows {
		if rows[i].PropertyID == nil {
			rows[i].PropertyName = allProperties
		}
	}
	httpx.JSON(w, http.StatusOK, rows)
}

type target struct {
	membershipID uuid.UUID
	userID       uuid.UUID
	role         string
	status       string
	name         string
}

var errNoTarget = errors.New("staff: no such person here")

// findTarget locates the one membership an action is about: the person's membership at the property in
// X-Property-Id, or their organization-wide one (an owner) when no property is named. Anyone outside
// that scope, or in another organization, does not exist as far as the caller can tell.
func findTarget(r *http.Request, tx pgx.Tx, a access.Actor) (target, error) {
	var t target
	userID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return t, errNoTarget
	}
	var property *uuid.UUID
	if a.PropertyID != uuid.Nil {
		property = &a.PropertyID
	}
	err = tx.QueryRow(r.Context(),
		`select m.id, m.user_id, m.role, m.status, u.name from membership m join app_user u on u.id = m.user_id
		 where m.user_id = $1 and m.organization_id = $2 and (m.property_id is not distinct from $3::uuid or m.property_id is null)`, userID, a.OrgID, property).
		Scan(&t.membershipID, &t.userID, &t.role, &t.status, &t.name)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, errNoTarget
	}
	if err == nil && t.role == string(access.Owner) && !access.Can(a.Role, access.OwnersManage) {
		return t, errNoTarget
	}
	return t, err
}

func (h *handler) patch(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role       *string `json:"role"`
		PropertyID *string `json:"propertyId"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	ctx, a := r.Context(), access.ActorFrom(r.Context())
	if in.Role == nil && in.PropertyID == nil {
		badRequest(w, "Choose a role or a property to change.")
		return
	}
	if in.Role != nil && (!validRole(*in.Role) || *in.Role == string(access.Owner)) {
		badRequest(w, "Choose manager, front desk, revenue or housekeeping. Owners are invited as owners.")
		return
	}
	var newProperty *uuid.UUID
	if in.PropertyID != nil {
		if a.Role != access.Owner {
			forbidden(w)
			return
		}
		id, err := uuid.Parse(*in.PropertyID)
		if err != nil {
			badRequest(w, "The property is not valid.")
			return
		}
		newProperty = &id
	}

	err := db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		t, err := findTarget(r, tx, a)
		if err != nil {
			return err
		}
		if t.role == string(access.Owner) {
			return errOwnerFixed
		}
		if newProperty != nil {
			var exists bool
			// Row-level security hides other organizations' properties, so a missing row means "not yours".
			if err := tx.QueryRow(ctx, `select exists (select 1 from property where id = $1)`, *newProperty).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return errNoTarget
			}
			if err := tx.QueryRow(ctx, `select exists (select 1 from membership where user_id = $1 and organization_id = $2 and property_id = $3)`, t.userID, a.OrgID, *newProperty).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return errAlreadyMember
			}
			if _, err := tx.Exec(ctx, `update membership set property_id = $2 where id = $1`, t.membershipID, *newProperty); err != nil {
				return err
			}
		}
		if in.Role != nil {
			if _, err := tx.Exec(ctx, `update membership set role = $2 where id = $1`, t.membershipID, *in.Role); err != nil {
				return err
			}
		}
		action, afterRole := "user.moved", t.role
		if in.Role != nil {
			action, afterRole = "user.role_changed", *in.Role
		}
		var beforeProperty, afterProperty any
		if a.PropertyID != uuid.Nil {
			beforeProperty = a.PropertyID
		}
		afterProperty = beforeProperty
		if newProperty != nil {
			afterProperty = *newProperty
		}
		return audit.Record(ctx, tx, audit.Entry{Action: action, TargetType: "user", TargetID: t.userID, TargetLabel: t.name,
			Before: map[string]any{"role": t.role, "propertyId": beforeProperty}, After: map[string]any{"role": afterRole, "propertyId": afterProperty}})
	})
	switch {
	case errors.Is(err, errNoTarget):
		notFound(w)
	case errors.Is(err, errOwnerFixed):
		badRequest(w, "An owner's role cannot be changed here.")
	case errors.Is(err, errAlreadyMember):
		httpx.Error(w, http.StatusConflict, "already_member", "That person already works there.")
	case err != nil:
		httpx.InternalError(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

var (
	errOwnerFixed = errors.New("staff: owner role is fixed")
	errLastOwner  = errors.New("staff: last owner")
)

// setStatus returns the handler that deactivates or reactivates a membership. Deactivating ends that
// person's sessions in this organization at once, and only there.
func (h *handler) setStatus(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, a := r.Context(), access.ActorFrom(r.Context())
		err := db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
			t, err := findTarget(r, tx, a)
			if err != nil {
				return err
			}
			if status == "deactivated" && t.role == string(access.Owner) && t.status == "active" {
				var owners int
				if err := tx.QueryRow(ctx, `select count(*) from membership where organization_id = $1 and role = 'owner' and status = 'active'`, a.OrgID).Scan(&owners); err != nil {
					return err
				}
				if owners <= 1 {
					return errLastOwner
				}
			}
			if _, err := tx.Exec(ctx, `update membership set status = $2 where id = $1`, t.membershipID, status); err != nil {
				return err
			}
			action := "user.reactivated"
			if status == "deactivated" {
				if err := session.EndInOrg(ctx, tx, t.userID, a.OrgID); err != nil {
					return err
				}
				action = "user.deactivated"
			}
			return audit.Record(ctx, tx, audit.Entry{Action: action, TargetType: "user", TargetID: t.userID, TargetLabel: t.name,
				Before: map[string]string{"status": t.status}, After: map[string]string{"status": status}})
		})
		switch {
		case errors.Is(err, errNoTarget):
			notFound(w)
		case errors.Is(err, errLastOwner):
			httpx.Error(w, http.StatusConflict, "last_owner", "An organization needs at least one owner.")
		case err != nil:
			httpx.InternalError(w, err)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// resetSignIn ends all of a person's sessions and forgets their trusted devices, so they sign in from scratch.
// It reaches every organization the person works in, because a session or a trusted device belongs to the person.
func (h *handler) resetSignIn(w http.ResponseWriter, r *http.Request) {
	ctx, a := r.Context(), access.ActorFrom(r.Context())
	err := db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		t, err := findTarget(r, tx, a)
		if err != nil {
			return err
		}
		if err := session.EndAll(ctx, tx, t.userID); err != nil {
			return err
		}
		if err := session.ForgetDevices(ctx, tx, t.userID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Action: "user.signin_reset", TargetType: "user", TargetID: t.userID, TargetLabel: t.name})
	})
	switch {
	case errors.Is(err, errNoTarget):
		notFound(w)
	case err != nil:
		httpx.InternalError(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
