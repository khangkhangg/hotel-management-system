package staff

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/account"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/ids"
	"hotelpms/api/internal/mail"
	"hotelpms/api/internal/password"
	"hotelpms/api/internal/session"
)

const (
	resetLifetime = 30 * time.Minute
	resetsPerHour = 3
)

// requestReset always answers 204, so nobody can use it to find out which email addresses have accounts.
// An unknown or deactivated address gets no email, and neither does a fourth request within the hour.
func (h *handler) requestReset(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email string }
	if !httpx.Decode(w, r, &in) {
		return
	}
	ctx, now := r.Context(), h.Clock()
	var msg *mail.Message
	var resetID uuid.UUID
	err := db.WithoutOrg(ctx, h.Pool, func(tx pgx.Tx) error {
		user, err := account.ByEmail(ctx, tx, in.Email)
		if errors.Is(err, account.ErrNotFound) || (err == nil && user.Status != "active") {
			return nil
		}
		if err != nil {
			return err
		}
		var recent int
		if err := tx.QueryRow(ctx, `select count(*) from password_reset where user_id = $1 and created_at > $2`, user.ID, now.Add(-time.Hour)).Scan(&recent); err != nil {
			return err
		}
		if recent >= resetsPerHour {
			return nil
		}
		token, hash, err := newToken()
		if err != nil {
			return err
		}
		id := ids.New()
		if _, err := tx.Exec(ctx, `insert into password_reset (id, user_id, token_hash, created_at, expires_at) values ($1, $2, $3, $4, $5)`,
			id, user.ID, hash, now, now.Add(resetLifetime)); err != nil {
			return err
		}
		msg = &mail.Message{To: user.Email, Subject: "Reset your password", Text: fmt.Sprintf("Someone asked to reset the password for this account.\n\nOpen this link to choose a new one. It works for 30 minutes and only once:\n%s/reset/%s\n\nIf it was not you, ignore this email. Your password stays as it is.\n", h.BaseURL, token)}
		resetID = id
		return nil
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	if msg != nil {
		// Sent after the answer is decided, so the response takes the same time whether or not the account exists.
		bg := context.WithoutCancel(ctx)
		h.Spawn(func() { h.sendReset(bg, *msg, resetID) })
	}
	w.WriteHeader(http.StatusNoContent)
}

// sendReset emails the link. If that fails the reset is voided and no longer counts toward the hourly limit.
func (h *handler) sendReset(ctx context.Context, m mail.Message, id uuid.UUID) {
	if err := h.Mail.Send(ctx, m); err == nil {
		return
	} else {
		log.Printf("password reset email failed: %v", err)
	}
	err := db.WithoutOrg(ctx, h.Pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update password_reset set used_at = now(), created_at = to_timestamp(0) where id = $1`, id)
		return err
	})
	if err != nil {
		log.Printf("password reset void failed: %v", err)
	}
}

var errLinkInvalid = errors.New("staff: reset link invalid")

func (h *handler) useReset(w http.ResponseWriter, r *http.Request) {
	var in struct{ Password string }
	if !httpx.Decode(w, r, &in) {
		return
	}
	token, ctx, now := r.PathValue("token"), r.Context(), h.Clock()
	var problem func(http.ResponseWriter)
	err := db.WithoutOrg(ctx, h.Pool, func(tx pgx.Tx) error {
		var id, userID uuid.UUID
		err := tx.QueryRow(ctx, `select id, user_id from password_reset where token_hash = $1 and used_at is null and expires_at > $2 for update`, hashOf(token), now).Scan(&id, &userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return errLinkInvalid
		}
		if err != nil {
			return err
		}
		if problem = h.passwordProblem(ctx, in.Password); problem != nil {
			return errRefused // roll back, so the link stays usable
		}
		hash, err := password.Hash(in.Password)
		if err != nil {
			return err
		}
		if err := account.SetPassword(ctx, tx, userID, hash); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `update password_reset set used_at = $2 where id = $1`, id, now); err != nil {
			return err
		}
		if err := session.EndAll(ctx, tx, userID); err != nil {
			return err
		}
		if err := session.ForgetDevices(ctx, tx, userID); err != nil {
			return err
		}
		return audit.RecordForUser(ctx, tx, userID, audit.Entry{Action: "password.changed", TargetType: "user", TargetID: userID})
	})
	switch {
	case errors.Is(err, errLinkInvalid):
		httpx.Error(w, http.StatusBadRequest, "link_invalid", "This link has expired. Ask for a new one.")
	case errors.Is(err, errRefused):
		problem(w)
	case err != nil:
		httpx.InternalError(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
