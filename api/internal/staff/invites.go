package staff

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/account"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/ids"
	"hotelpms/api/internal/mail"
	"hotelpms/api/internal/otp"
	"hotelpms/api/internal/password"
	"hotelpms/api/internal/session"
	"hotelpms/api/internal/signin"
	"hotelpms/api/internal/tenant"
)

const inviteLifetime = 7 * 24 * time.Hour

var errAlreadyMember = errors.New("staff: already a member")

func validEmail(s string) bool {
	at := strings.Index(s, "@")
	return len(s) <= 254 && at > 0 && at == strings.LastIndex(s, "@") && at < len(s)-1 && !strings.ContainsAny(s, " \t\r\n")
}

func (h *handler) createInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email      string
		Role       string
		PropertyID string `json:"propertyId"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	ctx, a := r.Context(), access.ActorFrom(r.Context())
	email := account.NormalizeEmail(in.Email)
	if !validEmail(email) || !validRole(in.Role) {
		badRequest(w, "Enter an email address and choose a role.")
		return
	}
	var property *uuid.UUID
	if in.Role == string(access.Owner) {
		if !access.Can(a.Role, access.OwnersManage) {
			forbidden(w)
			return
		}
	} else {
		if a.PropertyID == uuid.Nil {
			badRequest(w, "Choose a property for this person.")
			return
		}
		if in.PropertyID != "" && in.PropertyID != a.PropertyID.String() {
			badRequest(w, "The property in the request does not match the property you are working in.")
			return
		}
		property = &a.PropertyID
	}

	token, hash, err := newToken()
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	now := h.Clock()
	inviteID := ids.New()
	err = db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx,
			`select exists (select 1 from membership m join app_user u on u.id = m.user_id
			 where u.email = $1 and m.organization_id = $2 and m.property_id is not distinct from $3::uuid)`, email, a.OrgID, property).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return errAlreadyMember
		}
		// A newer invite replaces an older one for the same person and place.
		if _, err := tx.Exec(ctx, `delete from invite where organization_id = $1 and email = $2 and property_id is not distinct from $3::uuid and accepted_at is null`, a.OrgID, email, property); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`insert into invite (id, organization_id, property_id, email, role, token_hash, invited_by, created_at, expires_at)
			 values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			inviteID, a.OrgID, property, email, in.Role, hash, a.UserID, now, now.Add(inviteLifetime)); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Entry{Action: "user.invited", TargetType: "invite", TargetID: inviteID, TargetLabel: email,
			After: map[string]any{"role": in.Role, "propertyId": property}}); err != nil {
			return err
		}
		var orgName, propName string
		if err := tx.QueryRow(ctx, `select name from organization where id = $1`, a.OrgID).Scan(&orgName); err != nil {
			return err
		}
		if property != nil {
			if err := tx.QueryRow(ctx, `select name from property where id = $1`, *property).Scan(&propName); err != nil {
				return err
			}
		}
		where := orgName
		if propName != "" {
			where += ", " + propName
		}
		text := fmt.Sprintf("You have been invited to %s as %s.\n\nOpen this link to set up your account. It works for 7 days and only once:\n%s/invite/%s\n\nIf you were not expecting this, ignore this email.\n",
			where, roleWords(in.Role), h.BaseURL, token)
		if err := h.Mail.Send(ctx, mail.Message{To: email, Subject: "You are invited to " + orgName, Text: text}); err != nil {
			return fmt.Errorf("%w: %v", errEmailFailed, err)
		}
		return nil
	})
	switch {
	case errors.Is(err, errAlreadyMember):
		httpx.Error(w, http.StatusConflict, "already_member", "That person already works here.")
	case errors.Is(err, errEmailFailed):
		httpx.Error(w, http.StatusServiceUnavailable, "email_failed", "We could not send the invite. Try again.")
	case err != nil:
		httpx.InternalError(w, err)
	default:
		httpx.JSON(w, http.StatusCreated, map[string]any{"id": inviteID})
	}
}

var errEmailFailed = errors.New("staff: email failed")

type inviteRow struct {
	ID       uuid.UUID
	Org      uuid.UUID
	Property *uuid.UUID
	Email    string
	Role     string
}

// openInvite finds a live invite by its token inside a db.WithInvite transaction and then switches the
// transaction to that invite's organization. With lock it holds the row, so two requests cannot both use it.
func openInvite(r *http.Request, tx pgx.Tx, token string, now time.Time, lock bool) (inviteRow, error) {
	ctx := r.Context()
	var inv inviteRow
	q := `select id, organization_id, property_id, email, role from invite where token_hash = $1 and accepted_at is null and expires_at > $2`
	if lock {
		q += ` for update`
	}
	err := tx.QueryRow(ctx, q, hashOf(token), now).Scan(&inv.ID, &inv.Org, &inv.Property, &inv.Email, &inv.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return inv, errInviteGone
	}
	if err != nil {
		return inv, err
	}
	return inv, setOrg(ctx, tx, inv.Org)
}

var errInviteGone = errors.New("staff: invite not found, used or expired")

func (h *handler) previewInvite(w http.ResponseWriter, r *http.Request) {
	token, ctx := r.PathValue("token"), r.Context()
	var out struct {
		Email            string `json:"email"`
		OrganizationName string `json:"organizationName"`
		PropertyName     string `json:"propertyName"`
		Role             string `json:"role"`
	}
	err := db.WithInvite(ctx, h.Pool, hashOf(token), func(tx pgx.Tx) error {
		inv, err := openInvite(r, tx, token, h.Clock(), false)
		if err != nil {
			return err
		}
		out.Email, out.Role = inv.Email, inv.Role
		if err := tx.QueryRow(ctx, `select name from organization where id = $1`, inv.Org).Scan(&out.OrganizationName); err != nil {
			return err
		}
		if inv.Property != nil {
			return tx.QueryRow(ctx, `select name from property where id = $1`, *inv.Property).Scan(&out.PropertyName)
		}
		return nil
	})
	switch {
	case errors.Is(err, errInviteGone):
		notFound(w)
	case err != nil:
		httpx.InternalError(w, err)
	default:
		httpx.JSON(w, http.StatusOK, out)
	}
}

func (h *handler) acceptInvite(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Password string }
	if !httpx.Decode(w, r, &in) {
		return
	}
	token, ctx, now := r.PathValue("token"), r.Context(), h.Clock()
	name := strings.TrimSpace(in.Name)

	var user account.User
	var created bool
	var problem func() // set when the person's input was refused; the transaction is rolled back so the invite stays usable
	err := db.WithInvite(ctx, h.Pool, hashOf(token), func(tx pgx.Tx) error {
		inv, err := openInvite(r, tx, token, now, true)
		if err != nil {
			return err
		}
		user, err = account.ByEmail(ctx, tx, inv.Email)
		switch {
		case err == nil:
			if user.Status != "active" {
				return errInviteGone
			}
		case errors.Is(err, account.ErrNotFound):
			if name == "" || len(name) > 100 {
				problem = func() { badRequest(w, "Enter your name.") }
				return errRefused
			}
			if p := h.passwordProblem(ctx, in.Password); p != nil {
				problem = func() { p(w) }
				return errRefused
			}
			hash, err := password.Hash(in.Password)
			if err != nil {
				return err
			}
			id, err := account.Create(ctx, tx, inv.Email, name)
			if err != nil {
				return err
			}
			if err := account.SetPassword(ctx, tx, id, hash); err != nil {
				return err
			}
			user, created = account.User{ID: id, Email: inv.Email, Name: name, Status: "active"}, true
		default:
			return err
		}
		var already bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from membership where user_id = $1 and organization_id = $2 and property_id is not distinct from $3::uuid)`,
			user.ID, inv.Org, inv.Property).Scan(&already); err != nil {
			return err
		}
		if !already {
			if err := tenant.AddMembership(ctx, tx, inv.Org, user.ID, inv.Property, inv.Role); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, `update invite set accepted_at = $2 where id = $1`, inv.ID, now); err != nil {
			return err
		}
		who := audit.Who{UserID: user.ID, OrgID: inv.Org, Role: inv.Role}
		if inv.Property != nil {
			who.PropertyID = *inv.Property
		}
		return audit.Record(audit.WithWho(ctx, who), tx, audit.Entry{Action: "user.joined", TargetType: "user", TargetID: user.ID, TargetLabel: user.Name,
			After: map[string]any{"role": inv.Role}})
	})
	switch {
	case errors.Is(err, errRefused):
		problem()
		return
	case errors.Is(err, errInviteGone):
		notFound(w)
		return
	case err != nil:
		httpx.InternalError(w, err)
		return
	}
	if !created {
		httpx.JSON(w, http.StatusOK, map[string]string{"next": "sign_in"})
		return
	}

	// A brand new account still confirms its email with a code, like any other sign-in.
	var pending session.Session
	if err := db.WithoutOrg(ctx, h.Pool, func(tx pgx.Tx) (e error) {
		pending, e = session.Create(ctx, tx, user.ID, session.StagePending, signin.DescribeBrowser(r.UserAgent()), now)
		return
	}); err != nil {
		httpx.InternalError(w, err)
		return
	}
	session.SetCookie(w, pending.ID, h.CookieSecure)
	if err := otp.Issue(ctx, h.Pool, h.Mail, user, otp.Where{Browser: signin.DescribeBrowser(r.UserAgent())}, now); err != nil {
		// The account exists; the person can ask for another code with the pending session.
		httpx.Error(w, http.StatusServiceUnavailable, "email_failed", "We could not send the code. Try again.")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"next": "code"})
}

var errRefused = errors.New("staff: input refused")

// NewOwnerInvite adds an invite for an organization's first owner and returns its token, which is not stored.
// It is for "server setup", where no person exists yet to send the invite, and it works inside the transaction
// that created the organization.
func NewOwnerInvite(ctx context.Context, tx pgx.Tx, org uuid.UUID, email string, now time.Time) (token string, err error) {
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx,
		`insert into invite (id, organization_id, property_id, email, role, token_hash, invited_by, created_at, expires_at)
		 values ($1, $2, null, $3, 'owner', $4, null, $5, $6)`,
		ids.New(), org, account.NormalizeEmail(email), hash, now, now.Add(inviteLifetime))
	return token, err
}
