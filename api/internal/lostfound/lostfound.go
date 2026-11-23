// Package lostfound keeps what guests leave behind until it is returned or thrown away. Housekeeping logs and updates items,
// and the front desk looks them up when a guest calls.
package lostfound

import (
	"encoding/json"
	"errors"
	"io"
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

type item struct {
	ID           uuid.UUID `json:"id"`
	Description  string    `json:"description"`
	Location     string    `json:"location"`
	FoundOn      string    `json:"foundOn"`
	Status       string    `json:"status"` // stored, returned or discarded
	OwnerName    string    `json:"ownerName"`
	OwnerContact string    `json:"ownerContact"`
	Note         string    `json:"note"`
}

func (i item) valid() bool {
	_, err := time.Parse("2006-01-02", i.FoundOn)
	return strings.TrimSpace(i.Description) != "" && utf8.RuneCountInString(i.Description) <= 300 && err == nil && utf8.RuneCountInString(i.Location) <= 200 &&
		utf8.RuneCountInString(i.OwnerName) <= 200 && utf8.RuneCountInString(i.OwnerContact) <= 200 && utf8.RuneCountInString(i.Note) <= 1000 &&
		(i.Status == "" || i.Status == "stored" || i.Status == "returned" || i.Status == "discarded")
}

type handler struct{ httpx.Deps }

// Mount adds the routes. Housekeeping, managers and owners change items, and the front desk may look.
func Mount(mux *http.ServeMux, d httpx.Deps) {
	h := &handler{d}
	view := func(f http.HandlerFunc) http.HandlerFunc {
		return httpx.Wrap(access.Require(d, access.HousekeepingView)(f))
	}
	edit := func(f http.HandlerFunc) http.HandlerFunc {
		return httpx.Wrap(access.Require(d, access.HousekeepingUpdate)(f))
	}
	mux.HandleFunc("/api/lost-items", httpx.Methods(map[string]http.HandlerFunc{http.MethodGet: view(h.list), http.MethodPost: edit(h.create)}))
	mux.HandleFunc("/api/lost-items/{id}", httpx.Methods(map[string]http.HandlerFunc{http.MethodPut: edit(h.put)}))
}

func scope(w http.ResponseWriter, r *http.Request) (access.Actor, bool) {
	a := access.ActorFrom(r.Context())
	if a.PropertyID == uuid.Nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Choose a property first.")
		return a, false
	}
	return a, true
}

func read(w http.ResponseWriter, r *http.Request) (item, bool) {
	var in item
	d := json.NewDecoder(io.LimitReader(r.Body, 16<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil || !in.valid() {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Check the item and try again.")
		return in, false
	}
	in.Description = strings.TrimSpace(in.Description)
	if in.Status == "" {
		in.Status = "stored"
	}
	return in, true
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	out := []item{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select id, description, location, to_char(found_on, 'YYYY-MM-DD'), status, owner_name, owner_contact, note from lost_item
			where property_id = $1 and ($2 = '' or status = $2) order by (status = 'stored') desc, found_on desc, created_at desc limit 500`, a.PropertyID, status)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x item
			if err := rows.Scan(&x.ID, &x.Description, &x.Location, &x.FoundOn, &x.Status, &x.OwnerName, &x.OwnerContact, &x.Note); err != nil {
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

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	in, ok := read(w, r)
	if !ok {
		return
	}
	in.ID, in.Status = ids.New(), "stored"
	now := h.Clock()
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `insert into lost_item (id, organization_id, property_id, description, location, found_on, note, created_at, updated_at) values ($1, $2, $3, $4, $5, $6, $7, $8, $8)`,
			in.ID, a.OrgID, a.PropertyID, in.Description, in.Location, in.FoundOn, in.Note, now); err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "lost_item.logged", TargetType: "lost_item", TargetID: in.ID, TargetLabel: in.Description})
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, in)
}

var errGone = errors.New("lostfound: the item left the shelf")

func (h *handler) put(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
		return
	}
	in, ok := read(w, r)
	if !ok {
		return
	}
	in.ID = id
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(r.Context(), `select status from lost_item where id = $1 and property_id = $2 for update`, id, a.PropertyID).Scan(&status); err != nil {
			return err
		}
		if status != "stored" && in.Status == "stored" {
			return errGone
		}
		if _, err := tx.Exec(r.Context(), `update lost_item set description = $3, location = $4, found_on = $5, status = $6, owner_name = $7, owner_contact = $8, note = $9, updated_at = $10 where id = $1 and property_id = $2`,
			id, a.PropertyID, in.Description, in.Location, in.FoundOn, in.Status, in.OwnerName, in.OwnerContact, in.Note, h.Clock()); err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "lost_item." + in.Status, TargetType: "lost_item", TargetID: id, TargetLabel: in.Description})
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
	case errors.Is(err, errGone):
		httpx.Error(w, http.StatusConflict, "already_closed", "This item was already returned or thrown away.")
	case err != nil:
		httpx.InternalError(w, err)
	default:
		httpx.JSON(w, http.StatusOK, in)
	}
}
