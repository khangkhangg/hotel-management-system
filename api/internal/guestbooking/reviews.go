package guestbooking

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/ids"
)

type reviewOut struct {
	Rating    int       `json:"rating"`
	Comment   string    `json:"comment"`
	Name      string    `json:"name"`
	Reply     string    `json:"reply"`
	CreatedAt time.Time `json:"createdAt"`
}

// reviewsOut is what the public page shows: how many guests reviewed, their average, and the newest few.
type reviewsOut struct {
	Count   int         `json:"count"`
	Average float64     `json:"average"`
	Recent  []reviewOut `json:"recent"`
}

func loadReviews(ctx context.Context, tx pgx.Tx, prop uuid.UUID) (reviewsOut, error) {
	out := reviewsOut{Recent: []reviewOut{}}
	var avg *float64
	if err := tx.QueryRow(ctx, `select count(*), avg(rating)::float8 from review where property_id = $1 and status = 'published'`, prop).Scan(&out.Count, &avg); err != nil {
		return out, err
	}
	if avg != nil {
		out.Average = math.Round(*avg*10) / 10
	}
	rows, err := tx.Query(ctx, `select rating, comment, name, reply, created_at from review where property_id = $1 and status = 'published' order by created_at desc limit 5`, prop)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r reviewOut
		if err := rows.Scan(&r.Rating, &r.Comment, &r.Name, &r.Reply, &r.CreatedAt); err != nil {
			return out, err
		}
		out.Recent = append(out.Recent, r)
	}
	return out, rows.Err()
}

// displayName shows a guest as a first name and the first letter of the last name, so a review does not name anyone in full.
func displayName(full string) string {
	parts := strings.Fields(full)
	switch len(parts) {
	case 0:
		return "Guest"
	case 1:
		return parts[0]
	}
	last, _ := utf8.DecodeRuneInString(parts[len(parts)-1])
	return parts[0] + " " + string(last) + "."
}

var (
	errNotStayed = errors.New("guestbooking: the guest has not stayed yet")
	errReviewed  = errors.New("guestbooking: the booking was already reviewed")
	errBadReview = errors.New("guestbooking: bad review")
	errNoReview  = errors.New("guestbooking: no such review")
)

// reviewState says whether the guest of this booking may review it now.
func (h *handler) reviewState(w http.ResponseWriter, r *http.Request, tx pgx.Tx, a booked) error {
	var status string
	var reviewed bool
	if err := tx.QueryRow(r.Context(), `select r.status, exists (select 1 from review v where v.reservation_id = r.id) from reservation r where r.id = $1`, a.res).Scan(&status, &reviewed); err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"canReview": status == "checked_out" && !reviewed, "reviewed": reviewed})
	return nil
}

func (h *handler) postReview(w http.ResponseWriter, r *http.Request, tx pgx.Tx, a booked) error {
	var in struct {
		Rating  int    `json:"rating"`
		Comment string `json:"comment"`
	}
	if !readJSON(w, r, &in) {
		return errRollbackPreview // readJSON already answered
	}
	in.Comment = strings.TrimSpace(in.Comment)
	if in.Rating < 1 || in.Rating > 5 || utf8.RuneCountInString(in.Comment) > 2000 {
		return errBadReview
	}
	var status, name string
	if err := tx.QueryRow(r.Context(), `select r.status, g.name from reservation r join guest g on g.id = r.guest_id where r.id = $1`, a.res).Scan(&status, &name); err != nil {
		return err
	}
	if status != "checked_out" {
		return errNotStayed
	}
	tag, err := tx.Exec(r.Context(), `insert into review (id, organization_id, property_id, reservation_id, rating, comment, name, created_at) values ($1, $2, $3, $4, $5, $6, $7, $8)
		on conflict (reservation_id) do nothing`, ids.New(), a.org, a.prop, a.res, in.Rating, in.Comment, displayName(name), h.Clock())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errReviewed
	}
	w.WriteHeader(http.StatusCreated)
	return nil
}

// reviewError maps the review errors to answers. It returns false for any other error.
func reviewError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, errNotStayed):
		httpx.Error(w, http.StatusConflict, "not_stayed", "You can review your stay after you have checked out.")
	case errors.Is(err, errReviewed):
		httpx.Error(w, http.StatusConflict, "already_reviewed", "You have already reviewed this stay.")
	case errors.Is(err, errBadReview):
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Give a rating from 1 to 5 and a comment of up to 2000 characters.")
	default:
		return false
	}
	return true
}

type staffReview struct {
	ID uuid.UUID `json:"id"`
	reviewOut
	Status string `json:"status"`
}

func (h *handler) mountReviews(mux *http.ServeMux) {
	staff := func(f http.HandlerFunc) http.HandlerFunc {
		return httpx.Wrap(access.Require(h.Deps, access.RefundsApprove)(f))
	}
	mux.HandleFunc("/api/reviews", httpx.Methods(map[string]http.HandlerFunc{http.MethodGet: staff(h.listReviews)}))
	mux.HandleFunc("/api/reviews/{id}/hide", httpx.Methods(map[string]http.HandlerFunc{http.MethodPost: staff(h.setReview("hide"))}))
	mux.HandleFunc("/api/reviews/{id}/show", httpx.Methods(map[string]http.HandlerFunc{http.MethodPost: staff(h.setReview("show"))}))
	mux.HandleFunc("/api/reviews/{id}/reply", httpx.Methods(map[string]http.HandlerFunc{http.MethodPost: staff(h.setReview("reply"))}))
}

func (h *handler) listReviews(w http.ResponseWriter, r *http.Request) {
	a := access.ActorFrom(r.Context())
	out := []staffReview{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select id, rating, comment, name, reply, created_at, status from review where property_id = $1 order by created_at desc limit 200`, a.PropertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x staffReview
			if err := rows.Scan(&x.ID, &x.Rating, &x.Comment, &x.Name, &x.Reply, &x.CreatedAt, &x.Status); err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *handler) setReview(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := access.ActorFrom(r.Context())
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
			return
		}
		var in struct {
			Text string `json:"text"`
		}
		if action == "reply" {
			if !readJSON(w, r, &in) {
				return
			}
			in.Text = strings.TrimSpace(in.Text)
			if utf8.RuneCountInString(in.Text) > 1000 {
				httpx.Error(w, http.StatusBadRequest, "bad_request", "The reply is too long.")
				return
			}
		}
		err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
			var tag interface{ RowsAffected() int64 }
			var e error
			switch action {
			case "hide":
				tag, e = tx.Exec(r.Context(), `update review set status = 'hidden' where id = $1 and property_id = $2`, id, a.PropertyID)
			case "show":
				tag, e = tx.Exec(r.Context(), `update review set status = 'published' where id = $1 and property_id = $2`, id, a.PropertyID)
			default:
				tag, e = tx.Exec(r.Context(), `update review set reply = $3 where id = $1 and property_id = $2`, id, a.PropertyID, in.Text)
			}
			if e != nil {
				return e
			}
			if tag.RowsAffected() == 0 {
				return errNoReview
			}
			return audit.Record(r.Context(), tx, audit.Entry{Action: "review." + action, TargetType: "review", TargetID: id})
		})
		switch {
		case errors.Is(err, errNoReview):
			httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
		case err != nil:
			httpx.InternalError(w, err)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}
}
