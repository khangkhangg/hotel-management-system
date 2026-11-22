// Package signin implements the two-step staff sign-in: password, then a code sent by email (spec 02).
package signin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/account"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/otp"
	"hotelpms/api/internal/password"
	"hotelpms/api/internal/session"
)

type handler struct {
	httpx.Deps
	// dummyHash lets an unknown email cost the same time as a wrong password, so timing reveals nothing.
	dummyHash string
}

type orgRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

func Mount(mux *http.ServeMux, d httpx.Deps) {
	h := &handler{Deps: d}
	h.dummyHash, _ = password.Hash("this is nobody's password")
	post := func(next http.Handler) http.HandlerFunc {
		return httpx.Methods(map[string]http.HandlerFunc{http.MethodPost: httpx.Wrap(next)})
	}
	mux.HandleFunc("/api/sign-in", post(http.HandlerFunc(h.signIn)))
	mux.HandleFunc("/api/sign-in/code", post(d.RequirePending(http.HandlerFunc(h.code))))
	mux.HandleFunc("/api/sign-in/code/resend", post(d.RequirePending(http.HandlerFunc(h.resend))))
	mux.HandleFunc("/api/sign-in/organization", post(d.RequireSession(http.HandlerFunc(h.chooseOrganization))))
	mux.HandleFunc("/api/sign-out", post(http.HandlerFunc(h.signOut)))
	mux.HandleFunc("/api/sign-out/everywhere", post(d.RequireSession(http.HandlerFunc(h.signOutEverywhere))))
	mux.HandleFunc("/api/me", httpx.Methods(map[string]http.HandlerFunc{http.MethodGet: httpx.Wrap(d.RequireSession(http.HandlerFunc(h.me)))}))
}

func (h *handler) signIn(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if !httpx.Decode(w, r, &in) {
		return
	}
	ctx, now := r.Context(), h.Clock()
	email := account.NormalizeEmail(in.Email)
	acctKey, ipKey := accountKey(email), clientIP(r)

	var lockedOut bool
	var user account.User
	var found bool
	err := db.WithoutOrg(ctx, h.Pool, func(tx pgx.Tx) (e error) {
		if lockedOut, e = locked(ctx, tx, acctKey, ipKey, now); e != nil || lockedOut {
			return
		}
		user, e = account.ByEmail(ctx, tx, email)
		found = e == nil
		if errors.Is(e, account.ErrNotFound) {
			e = nil
		}
		return
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	if lockedOut {
		httpx.Error(w, http.StatusTooManyRequests, "too_many_attempts", "Too many sign-in attempts. Try again in 15 minutes.")
		return
	}

	hash := h.dummyHash
	if found && user.PasswordHash != "" {
		hash = user.PasswordHash
	}
	matches := password.Verify(hash, in.Password)
	if !matches || !found || user.Status != "active" || user.PasswordHash == "" {
		if err := db.WithoutOrg(ctx, h.Pool, func(tx pgx.Tx) error { return recordFailure(ctx, tx, now, acctKey, ipKey) }); err != nil {
			httpx.InternalError(w, err)
			return
		}
		httpx.Error(w, http.StatusUnauthorized, "invalid_credentials", "The email or password is not right.")
		return
	}

	sess, trusted, err := h.begin(r, user)
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	session.SetCookie(w, sess.ID, h.CookieSecure)
	if trusted {
		h.finish(w, r, user, sess)
		return
	}
	if !h.sendCode(w, r, user) {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"next": "code"})
}

// begin creates the session for a person whose first step passed: full on a trusted device, otherwise pending.
func (h *handler) begin(r *http.Request, user account.User) (sess session.Session, trusted bool, err error) {
	ctx, now := r.Context(), h.Clock()
	err = db.WithoutOrg(ctx, h.Pool, func(tx pgx.Tx) (e error) {
		trusted = session.IsTrusted(ctx, tx, user.ID, session.ReadDeviceCookie(r), now)
		stage := session.StagePending
		if trusted {
			stage = session.StageFull
		}
		sess, e = session.Create(ctx, tx, user.ID, stage, DescribeBrowser(r.UserAgent()), now)
		return
	})
	return
}

func (h *handler) issueCode(r *http.Request, user account.User) error {
	return otp.Issue(r.Context(), h.Pool, h.Mail, user, otp.Where{Browser: DescribeBrowser(r.UserAgent())}, h.Clock())
}

// sendCode emails a code and answers the error itself. It reports whether the code was sent.
func (h *handler) sendCode(w http.ResponseWriter, r *http.Request, user account.User) bool {
	err := h.issueCode(r, user)
	if err == nil {
		return true
	}
	switch {
	case errors.Is(err, otp.ErrTooMany):
		httpx.Error(w, http.StatusTooManyRequests, "too_many_codes", "You asked for 5 codes this hour. Try again later.")
	case errors.Is(err, otp.ErrSendFailed):
		httpx.Error(w, http.StatusServiceUnavailable, "email_failed", "We could not send the code. Try again.")
	default:
		httpx.InternalError(w, err)
	}
	return false
}

func (h *handler) code(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code        string
		TrustDevice bool
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	ctx, now, pending := r.Context(), h.Clock(), httpx.SessionFrom(r.Context())
	switch err := otp.Verify(ctx, h.Pool, pending.UserID, strings.TrimSpace(in.Code), now); {
	case err == nil:
	case errors.Is(err, otp.ErrWrong):
		httpx.Error(w, http.StatusBadRequest, "code_wrong", "That code is not right. Check the email and try again.")
		return
	case errors.Is(err, otp.ErrExpired):
		httpx.Error(w, http.StatusBadRequest, "code_expired", "That code has expired. We can send a new one.")
		return
	case errors.Is(err, otp.ErrCancelled):
		httpx.Error(w, http.StatusBadRequest, "code_cancelled", "Too many wrong tries. We can send a new code.")
		return
	default:
		httpx.InternalError(w, err)
		return
	}

	var full session.Session
	var user account.User
	var deviceToken string
	err := db.WithoutOrg(ctx, h.Pool, func(tx pgx.Tx) (e error) {
		if user, e = account.ByID(ctx, tx, pending.UserID); e != nil {
			return
		}
		if user.Status != "active" {
			return session.ErrNotFound
		}
		if full, e = session.Promote(ctx, tx, pending.ID, now); e != nil {
			return
		}
		if in.TrustDevice {
			deviceToken, e = session.TrustDevice(ctx, tx, user.ID, now)
		}
		return
	})
	if errors.Is(err, session.ErrNotFound) {
		httpx.Unauthenticated(w)
		return
	}
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	session.SetCookie(w, full.ID, h.CookieSecure)
	if deviceToken != "" {
		session.SetDeviceCookie(w, deviceToken, h.CookieSecure)
	}
	h.finish(w, r, user, full)
}

// finish runs once a full session exists: it records the sign-in and settles which organization the person works in.
func (h *handler) finish(w http.ResponseWriter, r *http.Request, user account.User, s session.Session) {
	orgs, err := h.settle(r, user, s)
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	if len(orgs) > 1 {
		httpx.JSON(w, http.StatusOK, map[string]any{"next": "choose_organization", "organizations": orgs})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"next": "done"})
}

// settle records the sign-in and, when the person belongs to exactly one organization, chooses it.
// It returns the organizations the person belongs to.
func (h *handler) settle(r *http.Request, user account.User, s session.Session) ([]orgRef, error) {
	ctx := r.Context()
	orgs, err := h.organizations(r, user.ID)
	if err != nil {
		return nil, err
	}
	err = db.WithoutOrg(ctx, h.Pool, func(tx pgx.Tx) error {
		if err := account.TouchSignIn(ctx, tx, user.ID, h.Clock()); err != nil {
			return err
		}
		if len(orgs) == 1 {
			return session.SetOrganization(ctx, tx, s.ID, orgs[0].ID)
		}
		return nil
	})
	return orgs, err
}

func (h *handler) organizations(r *http.Request, userID uuid.UUID) (orgs []orgRef, err error) {
	ctx := r.Context()
	err = db.WithUser(ctx, h.Pool, userID, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `select id, name from organization order by name`)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var o orgRef
			if e := rows.Scan(&o.ID, &o.Name); e != nil {
				return e
			}
			orgs = append(orgs, o)
		}
		return rows.Err()
	})
	return
}

func (h *handler) resend(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var user account.User
	err := db.WithoutOrg(ctx, h.Pool, func(tx pgx.Tx) (e error) { user, e = account.ByID(ctx, tx, httpx.SessionFrom(ctx).UserID); return })
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	if h.sendCode(w, r, user) {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *handler) chooseOrganization(w http.ResponseWriter, r *http.Request) {
	var in struct {
		OrganizationID string `json:"organizationId"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	id, err := uuid.Parse(in.OrganizationID)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "The request could not be read.")
		return
	}
	ctx, s := r.Context(), httpx.SessionFrom(r.Context())
	orgs, err := h.organizations(r, s.UserID)
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	for _, o := range orgs {
		if o.ID == id {
			if err := db.WithoutOrg(ctx, h.Pool, func(tx pgx.Tx) error { return session.SetOrganization(ctx, tx, s.ID, id) }); err != nil {
				httpx.InternalError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	httpx.Error(w, http.StatusForbidden, "forbidden", "You do not have access to that organization.")
}

func (h *handler) me(w http.ResponseWriter, r *http.Request) {
	ctx, s := r.Context(), httpx.SessionFrom(r.Context())
	if s.OrgID == nil {
		orgs, err := h.organizations(r, s.UserID)
		switch {
		case err != nil:
			httpx.InternalError(w, err)
		case len(orgs) == 0:
			noAccess(w)
		default:
			httpx.Error(w, http.StatusUnauthorized, "organization_required", "Choose an organization to continue.")
		}
		return
	}
	type membership struct {
		PropertyID   string `json:"propertyId"`
		PropertyName string `json:"propertyName"`
		Role         string `json:"role"`
	}
	var user account.User
	var org orgRef
	memberships := []membership{}
	err := db.WithOrgUser(ctx, h.Pool, *s.OrgID, s.UserID, func(tx pgx.Tx) (e error) {
		if user, e = account.ByID(ctx, tx, s.UserID); e != nil {
			return
		}
		org.ID = *s.OrgID
		if e = tx.QueryRow(ctx, `select name from organization where id = $1`, org.ID).Scan(&org.Name); e != nil {
			return
		}
		rows, e := tx.Query(ctx,
			`select coalesce(m.property_id::text, ''), coalesce(p.name, ''), m.role
			 from membership m left join property p on p.id = m.property_id
			 where m.user_id = $1 and m.organization_id = $2 order by p.name nulls first`, s.UserID, org.ID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var m membership
			if e = rows.Scan(&m.PropertyID, &m.PropertyName, &m.Role); e != nil {
				return e
			}
			memberships = append(memberships, m)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	if len(memberships) == 0 {
		noAccess(w)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"user":         map[string]any{"id": user.ID, "name": user.Name, "email": user.Email},
		"organization": org,
		"memberships":  memberships,
		"viewAs":       s.ViewAs,
	})
}

func noAccess(w http.ResponseWriter) {
	httpx.Error(w, http.StatusForbidden, "no_access", "Your account has no access. Ask your manager.")
}

func (h *handler) signOut(w http.ResponseWriter, r *http.Request) {
	if id := session.ReadCookie(r); id != "" {
		ctx := r.Context()
		if err := db.WithoutOrg(ctx, h.Pool, func(tx pgx.Tx) error { return session.End(ctx, tx, id) }); err != nil {
			httpx.InternalError(w, err)
			return
		}
	}
	session.ClearCookie(w, h.CookieSecure)
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) signOutEverywhere(w http.ResponseWriter, r *http.Request) {
	ctx, s := r.Context(), httpx.SessionFrom(r.Context())
	err := db.WithoutOrg(ctx, h.Pool, func(tx pgx.Tx) error {
		if err := session.EndAll(ctx, tx, s.UserID); err != nil {
			return err
		}
		return session.ForgetDevices(ctx, tx, s.UserID)
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	session.ClearCookie(w, h.CookieSecure)
	session.ClearDeviceCookie(w, h.CookieSecure)
	w.WriteHeader(http.StatusNoContent)
}
