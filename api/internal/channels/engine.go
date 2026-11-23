package channels

import (
	"net/http"
	"sort"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
)

// Profile describes one way to connect, so the setup screen is drawn from it and a new provider needs no new screen.
type Profile struct {
	ID          string   `json:"id"`
	Mode        string   `json:"mode"` // channel_manager or direct
	Name        string   `json:"name"`
	Recommended bool     `json:"recommended"`
	Fields      []string `json:"fields"`     // which inputs the owner fills in: hotelId, baseUrl, secret
	Steps       []string `json:"steps"`      // plain-language things to do on the other side first
	Reaches     []string `json:"reaches"`    // the booking sites this connection serves
	Verified    bool     `json:"verified"`   // false until it has been run against the provider's own test system
	SecretHint  string   `json:"secretHint"` // how the secret is written
	Bookings    bool     `json:"bookings"`   // false when the provider only shows prices and sends guests to your site
}

// Profiles lists what owners can connect to. The recommended one comes first.
func Profiles() []Profile {
	return []Profile{
		{ID: "channex", Mode: "channel_manager", Name: "Channex", Recommended: true, Fields: []string{"hotelId", "secret", "baseUrl"}, Bookings: true,
			Reaches: []string{"Booking.com", "Expedia", "Agoda", "Airbnb"}, Verified: true, SecretHint: "The API key from your Channex profile.",
			Steps: []string{"Make a free Channex account and add your hotel there.", "In Channex, connect your Booking.com, Expedia or Agoda listings to that hotel.", "Create an API key in your Channex profile, and ask your server administrator to save it as a server setting.", "Copy the hotel's Channex property id into the hotel code box below."}},
		{ID: "agoda", Mode: "direct", Name: "Agoda", Fields: []string{"hotelId", "secret", "baseUrl"}, Reaches: []string{"Agoda"}, Bookings: true,
			SecretHint: "Written as clientId:clientSecret, from the API access page of the Agoda YCS portal.",
			Steps:      []string{"Apply as a connectivity partner at Agoda (the partner questionnaire on developer.agoda.com). Agoda tests with 2 or 3 live hotels before it certifies you.", "Ask Agoda to switch your hotel to channel manager mode, or email agoda.chain.support@agoda.com with the hotel id.", "In the YCS portal, open API access, create a client id and secret, and allow this server's address.", "Ask your server administrator to save clientId:clientSecret as a server setting, and put your Agoda hotel id in the hotel code box."}},
		{ID: "booking_com", Mode: "direct", Name: "Booking.com", Fields: []string{"hotelId", "secret", "baseUrl"}, Reaches: []string{"Booking.com"}, Bookings: true,
			SecretHint: "Written as user:password for a machine account from the Connectivity Portal, or a current token.",
			Steps:      []string{"Ask Booking.com Connectivity Support to certify your system as a connectivity provider for Rates and Availability and for Reservations. They run test requests with you.", "Create a machine account for this system in the Connectivity Portal (connect.booking.com).", "Ask your server administrator to save the machine account as a server setting, and put your Booking.com hotel id in the hotel code box."}},
		{ID: "expedia", Mode: "direct", Name: "Expedia", Fields: []string{"hotelId", "secret", "baseUrl"}, Reaches: []string{"Expedia", "Hotels.com", "Vrbo"}, Bookings: true,
			SecretHint: "Written as username:password of your Expedia QuickConnect login.",
			Steps:      []string{"Expedia does not take direct connections from a single hotel. The company running this system applies at developers.expediagroup.com/supply/lodging as a connectivity provider (PCI review, licence agreement, test, go live).", "Once approved, Expedia gives the QuickConnect login for your hotel.", "Ask your server administrator to save username:password as a server setting, and put your Expedia hotel id in the hotel code box."}},
		{ID: "google", Mode: "direct", Name: "Google Hotels", Fields: []string{"hotelId", "secret", "baseUrl"}, Reaches: []string{"Google Search", "Google Maps"}, Bookings: false,
			SecretHint: "Optional: the partner key from your Hotel Center account settings.",
			Steps:      []string{"Open a Hotel Center account (hotelcenter.google.com) and add your hotel, with its landing page. Google tells you the hotel id you use here.", "Ask Google for Hotel Prices (ARI push) access through your account manager, and allow this server's address under price settings.", "Guests who click a price on Google arrive on your own booking site, so no bookings come back through this connection."}},
		{ID: "cm_generic", Mode: "channel_manager", Name: "Another channel manager (JSON connection)", Fields: []string{"hotelId", "baseUrl", "secret"}, Bookings: true,
			Reaches: []string{}, Verified: false, SecretHint: "A bearer token.",
			Steps: []string{"Ask your channel manager for a connector that speaks this system's JSON format, or ask us for the format sheet."}},
	}
}

func (h *handler) providers(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, Profiles())
}

// test builds the adapter from the saved connection and asks it to check its own credentials. A failed test is a
// normal answer, not an error: the screen shows the reason.
func (h *handler) test(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var l loaded
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error { return loadConnection(r.Context(), tx, a.PropertyID, &l, false) })
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "Save the connection first.")
		return
	}
	ad, err := h.factory(l.conn)
	if err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	t, can := ad.(Tester)
	if !can {
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "message": "This provider has no test call, so nothing was checked."})
		return
	}
	if err := t.Test(r.Context()); err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// LocalRate is one of the property's rate plans, with the room type it belongs to.
type LocalRate struct{ PlanID, RoomName, PlanName string }

// Suggestion pairs a local rate plan with a room and rate on the other side.
type Suggestion struct {
	PlanID string  `json:"planId"`
	RoomID string  `json:"roomId"`
	RateID string  `json:"rateId"`
	Score  float64 `json:"score"`
}

func (h *handler) remote(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var l loaded
	var local []LocalRate
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		if err := loadConnection(r.Context(), tx, a.PropertyID, &l, false); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), `select p.id, t.name, p.name from rate_plan p join room_type t on t.id = p.room_type_id where p.property_id = $1 order by t.name, p.name`, a.PropertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x LocalRate
			if err := rows.Scan(&x.PlanID, &x.RoomName, &x.PlanName); err != nil {
				return err
			}
			local = append(local, x)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "Save the connection first.")
		return
	}
	ad, err := h.factory(l.conn)
	if err != nil {
		httpx.Error(w, http.StatusConflict, "provider_unavailable", err.Error())
		return
	}
	lister, can := ad.(Lister)
	if !can {
		httpx.JSON(w, http.StatusOK, map[string]any{"remote": []RemoteRate{}, "suggestions": []Suggestion{}, "listed": false})
		return
	}
	remote, err := lister.Remote(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "provider_error", err.Error())
		return
	}
	if remote == nil {
		remote = []RemoteRate{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"remote": remote, "suggestions": Suggest(local, remote), "listed": true})
}

var filler = map[string]bool{"room": true, "rate": true, "the": true, "and": true, "with": true, "for": true, "a": true, "of": true, "plan": true}

func tokens(s string) map[string]bool {
	out := map[string]bool{}
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if !filler[f] {
			out[f] = true
		}
	}
	return out
}

// overlap is how much of the shorter name is found in the other, from 0 to 1. A short name such as "Deluxe"
// matches "Deluxe Double Room" fully, because everything it says is there.
func overlap(a, b string) float64 {
	x, y := tokens(a), tokens(b)
	if len(x) == 0 || len(y) == 0 {
		return 0
	}
	hit := 0
	for t := range x {
		if y[t] {
			hit++
		}
	}
	return float64(hit) / float64(min(len(x), len(y)))
}

// norm joins "non-refundable" and "non refundable" the same way.
func norm(s string) string { return strings.NewReplacer("-", "", " ", "").Replace(strings.ToLower(s)) }

func rateScore(a, b string) float64 {
	if norm(a) == norm(b) {
		return 1
	}
	return overlap(a, b)
}

// minScore is how close a match must be to be offered. Below it, the owner picks by hand.
const minScore = 0.5

// Suggest pairs rate plans with the other side's rates by how alike their room and rate names are. Each remote
// rate and each plan is used once, best match first. It only suggests: the owner accepts or changes each one.
func Suggest(local []LocalRate, remote []RemoteRate) []Suggestion {
	type cand struct {
		plan, rem int
		score     float64
	}
	var cands []cand
	for i, l := range local {
		for j, r := range remote {
			room, rate := overlap(l.RoomName, r.RoomName), rateScore(l.PlanName, r.RateName)
			if room == 0 || rate == 0 {
				continue
			}
			if s := (room + rate) / 2; s >= minScore {
				cands = append(cands, cand{i, j, s})
			}
		}
	}
	sort.SliceStable(cands, func(a, b int) bool { return cands[a].score > cands[b].score })
	usedPlan, usedRem := map[int]bool{}, map[int]bool{}
	out := []Suggestion{}
	for _, c := range cands {
		if usedPlan[c.plan] || usedRem[c.rem] {
			continue
		}
		usedPlan[c.plan], usedRem[c.rem] = true, true
		out = append(out, Suggestion{PlanID: local[c.plan].PlanID, RoomID: remote[c.rem].RoomID, RateID: remote[c.rem].RateID, Score: c.score})
	}
	return out
}
