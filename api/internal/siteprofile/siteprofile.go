// Package siteprofile holds what a hotel shows on its guest site besides rooms: text in each language, amenities,
// photos and the brand colour (specs 05 and 08). Only the owner changes it.
package siteprofile

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/ids"
)

// Locales are the ten languages of the guest site.
var Locales = []string{"en", "zh-Hans", "hi", "es", "fr", "ar", "bn", "pt", "ru", "ur"}

// fields are the texts a hotel writes, with the most characters each may have.
var fields = map[string]int{"tagline": 120, "description": 2000, "policies": 2000}

// Catalog is the list of amenities a hotel picks from. The guest site names each one in the guest's language.
var Catalog = []string{"wifi", "pool", "parking", "breakfast", "air_conditioning", "gym", "spa", "restaurant", "bar", "airport_shuttle", "pet_friendly", "front_desk_24h", "laundry", "family_rooms", "accessible"}

const (
	maxPhotoBytes = 2 << 20
	maxGallery    = 12
	maxCaption    = 120
)

var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// Photo is one picture of the hotel.
type Photo struct {
	ID      uuid.UUID `json:"id"`
	Caption string    `json:"caption"`
}

// Public is what the guest site shows.
type Public struct {
	Color     string                       `json:"color"`
	Logo      string                       `json:"logo"`
	Texts     map[string]map[string]string `json:"texts"`
	Amenities []string                     `json:"amenities"`
	Photos    []Photo                      `json:"photos"`
}

// LoadPublic reads the site profile of a property. Call it inside the organization's transaction.
func LoadPublic(ctx context.Context, tx pgx.Tx, prop uuid.UUID) (Public, error) {
	out := Public{Texts: map[string]map[string]string{}, Amenities: []string{}, Photos: []Photo{}}
	err := tx.QueryRow(ctx, `select color from property_brand where property_id = $1`, prop).Scan(&out.Color)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	var logo uuid.UUID
	if err := tx.QueryRow(ctx, `select id from property_photo where property_id = $1 and kind = 'logo'`, prop).Scan(&logo); err == nil {
		out.Logo = logo.String()
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	rows, err := tx.Query(ctx, `select locale, field, body from property_text where property_id = $1`, prop)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var l, f, b string
		if err := rows.Scan(&l, &f, &b); err != nil {
			rows.Close()
			return out, err
		}
		if out.Texts[l] == nil {
			out.Texts[l] = map[string]string{}
		}
		out.Texts[l][f] = b
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}
	am, err := tx.Query(ctx, `select code from property_amenity where property_id = $1 order by code`, prop)
	if err != nil {
		return out, err
	}
	for am.Next() {
		var c string
		if err := am.Scan(&c); err != nil {
			am.Close()
			return out, err
		}
		out.Amenities = append(out.Amenities, c)
	}
	am.Close()
	if err := am.Err(); err != nil {
		return out, err
	}
	ph, err := tx.Query(ctx, `select id, caption from property_photo where property_id = $1 and kind = 'gallery' order by created_at, id`, prop)
	if err != nil {
		return out, err
	}
	defer ph.Close()
	for ph.Next() {
		var p Photo
		if err := ph.Scan(&p.ID, &p.Caption); err != nil {
			return out, err
		}
		out.Photos = append(out.Photos, p)
	}
	return out, ph.Err()
}

// PhotoData returns one picture of the property, or pgx.ErrNoRows.
func PhotoData(ctx context.Context, tx pgx.Tx, prop, id uuid.UUID) (contentType string, data []byte, err error) {
	err = tx.QueryRow(ctx, `select content_type, data from property_photo where id = $1 and property_id = $2`, id, prop).Scan(&contentType, &data)
	return
}

// contrastWithWhite is the WCAG contrast of a #RRGGBB colour against white. Text on the brand colour is white.
func contrastWithWhite(hex string) float64 {
	lin := func(s string) float64 {
		var v int
		for _, c := range strings.ToLower(s) {
			v = v*16 + strings.IndexRune("0123456789abcdef", c)
		}
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	l := 0.2126*lin(hex[1:3]) + 0.7152*lin(hex[3:5]) + 0.0722*lin(hex[5:7])
	return 1.05 / (l + 0.05)
}

type handler struct{ httpx.Deps }

// Mount adds the staff routes. They need the branding permission, which only owners have.
func Mount(mux *http.ServeMux, d httpx.Deps) {
	h := &handler{d}
	one := func(method string, f http.HandlerFunc) http.HandlerFunc {
		return httpx.Methods(map[string]http.HandlerFunc{method: httpx.Wrap(access.Require(d, access.BrandingConfigure)(f))})
	}
	mux.HandleFunc("/api/site", one(http.MethodGet, h.get))
	mux.HandleFunc("/api/site/brand", one(http.MethodPut, h.putBrand))
	mux.HandleFunc("/api/site/texts/{locale}/{field}", one(http.MethodPut, h.putText))
	mux.HandleFunc("/api/site/amenities", one(http.MethodPut, h.putAmenities))
	mux.HandleFunc("/api/site/photos", one(http.MethodPost, h.postPhoto))
	mux.HandleFunc("/api/site/photos/{id}", one(http.MethodDelete, h.deletePhoto))
}

func scope(w http.ResponseWriter, r *http.Request) (access.Actor, bool) {
	a := access.ActorFrom(r.Context())
	if a.PropertyID == uuid.Nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Choose a property first.")
		return a, false
	}
	return a, true
}

func fail(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
	default:
		httpx.Error(w, http.StatusInternalServerError, "internal", "Something went wrong.")
	}
	return true
}

func bad(w http.ResponseWriter, code, msg string) { httpx.Error(w, http.StatusBadRequest, code, msg) }

func readBody(w http.ResponseWriter, r *http.Request, limit int64, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			httpx.Error(w, http.StatusRequestEntityTooLarge, "too_large", "That file is too large. Photos can be up to 2 MB.")
		} else {
			bad(w, "bad_request", "Check the details and try again.")
		}
		return false
	}
	return true
}

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	out := struct {
		Slug    string   `json:"slug"`
		Catalog []string `json:"catalog"`
		Locales []string `json:"locales"`
		Public
	}{Catalog: Catalog, Locales: Locales}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `select slug from property where id = $1`, a.PropertyID).Scan(&out.Slug); err != nil {
			return err
		}
		var err error
		out.Public, err = LoadPublic(r.Context(), tx, a.PropertyID)
		return err
	})
	if fail(w, err) {
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *handler) changed(w http.ResponseWriter, r *http.Request, a access.Actor, what string, after any, f func(tx pgx.Tx) error) {
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		if err := f(tx); err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "site." + what, TargetType: "property", TargetID: a.PropertyID, After: after})
	})
	if fail(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) putBrand(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var in struct {
		Color string `json:"color"`
	}
	if !readBody(w, r, 4<<10, &in) {
		return
	}
	if !hexColor.MatchString(in.Color) {
		bad(w, "bad_request", "Give the colour as six hex digits, like #1F5F4F.")
		return
	}
	if contrastWithWhite(in.Color) < 4.5 {
		bad(w, "color_too_light", "This colour is too light for white text. Pick a darker one.")
		return
	}
	h.changed(w, r, a, "brand_changed", in, func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `insert into property_brand (organization_id, property_id, color) values ($1, $2, $3)
			on conflict (property_id) do update set color = excluded.color`, a.OrgID, a.PropertyID, strings.ToUpper(in.Color))
		return err
	})
}

func (h *handler) putText(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	locale, field := r.PathValue("locale"), r.PathValue("field")
	limit, known := fields[field]
	if !known || !slices.Contains(Locales, locale) {
		bad(w, "bad_request", "That language or field is not offered.")
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if !readBody(w, r, 16<<10, &in) {
		return
	}
	in.Body = strings.TrimSpace(in.Body)
	if utf8.RuneCountInString(in.Body) > limit {
		bad(w, "too_long", "That text is too long.")
		return
	}
	h.changed(w, r, a, "text_changed", map[string]any{"locale": locale, "field": field, "removed": in.Body == ""}, func(tx pgx.Tx) error {
		if in.Body == "" {
			_, err := tx.Exec(r.Context(), `delete from property_text where property_id = $1 and locale = $2 and field = $3`, a.PropertyID, locale, field)
			return err
		}
		_, err := tx.Exec(r.Context(), `insert into property_text (organization_id, property_id, locale, field, body) values ($1, $2, $3, $4, $5)
			on conflict (property_id, locale, field) do update set body = excluded.body`, a.OrgID, a.PropertyID, locale, field, in.Body)
		return err
	})
}

func (h *handler) putAmenities(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var in struct {
		Codes []string `json:"codes"`
	}
	if !readBody(w, r, 8<<10, &in) {
		return
	}
	var codes []string
	for _, c := range in.Codes {
		if !slices.Contains(Catalog, c) {
			bad(w, "bad_request", "That amenity is not in the list.")
			return
		}
		if !slices.Contains(codes, c) {
			codes = append(codes, c)
		}
	}
	h.changed(w, r, a, "amenities_changed", map[string]any{"codes": codes}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `delete from property_amenity where property_id = $1`, a.PropertyID); err != nil {
			return err
		}
		for _, c := range codes {
			if _, err := tx.Exec(r.Context(), `insert into property_amenity (organization_id, property_id, code) values ($1, $2, $3)`, a.OrgID, a.PropertyID, c); err != nil {
				return err
			}
		}
		return nil
	})
}

var errTooMany = errors.New("siteprofile: the gallery is full")

func (h *handler) postPhoto(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var in struct {
		Kind    string `json:"kind"`
		Caption string `json:"caption"`
		Data    string `json:"data"`
	}
	if !readBody(w, r, int64(base64.StdEncoding.EncodedLen(maxPhotoBytes))+8<<10, &in) {
		return
	}
	if (in.Kind != "gallery" && in.Kind != "logo") || utf8.RuneCountInString(in.Caption) > maxCaption {
		bad(w, "bad_request", "Check the details and try again.")
		return
	}
	if len(in.Data) > base64.StdEncoding.EncodedLen(maxPhotoBytes) {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "too_large", "That file is too large. Photos can be up to 2 MB.")
		return
	}
	raw, err := base64.StdEncoding.DecodeString(in.Data)
	if err != nil {
		bad(w, "bad_request", "Check the details and try again.")
		return
	}
	// The type comes from the bytes, never from what the sender says. SVG and other text files are refused.
	ctype := http.DetectContentType(raw)
	if ctype != "image/png" && ctype != "image/jpeg" && ctype != "image/webp" {
		bad(w, "not_an_image", "Use a JPEG, PNG or WebP picture.")
		return
	}
	id := ids.New()
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		if in.Kind == "logo" {
			if _, err := tx.Exec(r.Context(), `delete from property_photo where property_id = $1 and kind = 'logo'`, a.PropertyID); err != nil {
				return err
			}
		} else {
			var n int
			if err := tx.QueryRow(r.Context(), `select count(*) from property_photo where property_id = $1 and kind = 'gallery'`, a.PropertyID).Scan(&n); err != nil {
				return err
			}
			if n >= maxGallery {
				return errTooMany
			}
		}
		if _, err := tx.Exec(r.Context(), `insert into property_photo (id, organization_id, property_id, kind, content_type, caption, data) values ($1, $2, $3, $4, $5, $6, $7)`,
			id, a.OrgID, a.PropertyID, in.Kind, ctype, strings.TrimSpace(in.Caption), raw); err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "site.photo_added", TargetType: "property", TargetID: a.PropertyID, After: map[string]any{"photoId": id, "kind": in.Kind}})
	})
	if errors.Is(err, errTooMany) {
		httpx.Error(w, http.StatusConflict, "too_many_photos", "The gallery holds up to 12 photos. Remove one first.")
		return
	}
	if fail(w, err) {
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (h *handler) deletePhoto(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
		return
	}
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `delete from property_photo where id = $1 and property_id = $2`, id, a.PropertyID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "site.photo_removed", TargetType: "property", TargetID: a.PropertyID, After: map[string]any{"photoId": id}})
	})
	if fail(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
