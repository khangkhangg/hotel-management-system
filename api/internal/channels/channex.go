package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultChannexURL is Channex's production API. Their sandbox is https://staging.channex.io/api/v1.
const DefaultChannexURL = "https://app.channex.io/api/v1"

// Channex is the adapter for Channex.io, a channel manager built for property management systems. Channex holds the
// links to Booking.com, Expedia, Agoda and others; this PMS keeps one property connected to it. The hotel's code on
// their side is the Channex property id; a mapped room is a Channex room type id and a mapped rate a Channex rate plan id.
type Channex struct {
	base, property, key string
	c                   *http.Client
}

func NewChannex(base, property, key string, c *http.Client) *Channex {
	if base == "" {
		base = DefaultChannexURL
	}
	return &Channex{strings.TrimRight(base, "/"), property, key, c}
}

func (a *Channex) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var rd *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("user-api-key", a.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.c.Do(req)
	if err != nil {
		return nil, err
	}
	out, err := readReply(resp)
	if err != nil {
		return nil, err
	}
	// Channex reports a rejected value with a 200 and a warning, so the warnings are read too.
	var meta struct {
		Meta struct {
			Warnings json.RawMessage `json:"warnings"`
		} `json:"meta"`
	}
	if json.Unmarshal(out, &meta) == nil {
		if w := strings.TrimSpace(string(meta.Meta.Warnings)); w != "" && w != "[]" && w != "null" {
			return out, fmt.Errorf("Channex refused some values: %s", w)
		}
	}
	return out, nil
}

func (a *Channex) PushAvailability(ctx context.Context, slots []Slot) error {
	type key struct{ id, date string }
	seen := map[key]bool{}
	var avail, rates []map[string]any
	for _, s := range slots {
		if k := (key{s.ExternalRoomID, s.Date}); !seen[k] {
			seen[k] = true
			avail = append(avail, map[string]any{"property_id": a.property, "room_type_id": s.ExternalRoomID, "date": s.Date, "availability": s.Available})
		}
		if s.ExternalRateID != "" && s.Price > 0 {
			rates = append(rates, map[string]any{"property_id": a.property, "rate_plan_id": s.ExternalRateID, "date": s.Date, "rate": s.Price})
		}
	}
	for _, p := range []struct {
		path   string
		values []map[string]any
	}{{"/availability", avail}, {"/restrictions", rates}} {
		for i := 0; i < len(p.values); i += batch {
			if _, err := a.do(ctx, http.MethodPost, p.path, map[string]any{"values": p.values[i:min(i+batch, len(p.values))]}); err != nil {
				return err
			}
		}
	}
	return nil
}

type channexRevision struct {
	ID         string `json:"id"`
	Attributes struct {
		BookingID string `json:"booking_id"`
		Status    string `json:"status"`
		OTAName   string `json:"ota_name"`
		Arrival   string `json:"arrival_date"`
		Departure string `json:"departure_date"`
		Occupancy struct {
			Adults   int `json:"adults"`
			Children int `json:"children"`
		} `json:"occupancy"`
		Customer struct {
			Name    string `json:"name"`
			Surname string `json:"surname"`
			Mail    string `json:"mail"`
		} `json:"customer"`
		Rooms []struct {
			RoomTypeID string `json:"room_type_id"`
			RatePlanID string `json:"rate_plan_id"`
		} `json:"rooms"`
	} `json:"attributes"`
}

func (a *Channex) PullBookings(ctx context.Context, _ time.Time) ([]Booking, error) {
	q := url.Values{"filter[property_id]": {a.property}, "order[inserted_at]": {"asc"}}
	body, err := a.do(ctx, http.MethodGet, "/booking_revisions/feed?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Data []channexRevision `json:"data"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	out := make([]Booking, 0, len(doc.Data))
	for _, r := range doc.Data {
		at := r.Attributes
		b := Booking{ExternalID: at.BookingID, AckID: r.ID, Status: at.Status, Source: at.OTAName, Arrival: at.Arrival, Departure: at.Departure,
			GuestName: strings.TrimSpace(at.Customer.Name + " " + at.Customer.Surname), Adults: at.Occupancy.Adults, Children: at.Occupancy.Children}
		if m := strings.TrimSpace(at.Customer.Mail); m != "" && !strings.Contains(m, "*") { // once acknowledged, Channex masks contact details
			b.Email = m
		}
		switch len(at.Rooms) {
		case 0:
			b.Problem = "the booking has no room"
		case 1:
			b.ExternalRoomID, b.ExternalRateID = at.Rooms[0].RoomTypeID, at.Rooms[0].RatePlanID
		default:
			b.Problem = fmt.Sprintf("the booking has %d rooms, and this PMS keeps one room per imported booking", len(at.Rooms))
		}
		out = append(out, b)
	}
	return out, nil
}

// Ack tells Channex the bookings are saved. Channex hands an unacknowledged booking out again after 30 minutes.
func (a *Channex) Ack(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if _, err := a.do(ctx, http.MethodPost, "/booking_revisions/"+url.PathEscape(id)+"/ack", map[string]any{}); err != nil {
			return err
		}
	}
	return nil
}

// Test checks the key and the property code.
func (a *Channex) Test(ctx context.Context) error {
	_, err := a.do(ctx, http.MethodGet, "/properties/"+url.PathEscape(a.property), nil)
	return err
}

// Remote lists the room types and rate plans on Channex, for the mapping step.
func (a *Channex) Remote(ctx context.Context) ([]RemoteRate, error) {
	q := "?filter[property_id]=" + url.QueryEscape(a.property)
	rb, err := a.do(ctx, http.MethodGet, "/room_types/options"+q, nil)
	if err != nil {
		return nil, err
	}
	pb, err := a.do(ctx, http.MethodGet, "/rate_plans/options"+q, nil)
	if err != nil {
		return nil, err
	}
	var rooms struct {
		Data []struct {
			ID    string `json:"id"`
			Attrs struct {
				Title string `json:"title"`
			} `json:"attributes"`
		} `json:"data"`
	}
	var plans struct {
		Data []struct {
			ID    string `json:"id"`
			Attrs struct {
				Title      string `json:"title"`
				Occupancy  int    `json:"occupancy"`
				RoomTypeID string `json:"room_type_id"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rb, &rooms); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(pb, &plans); err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, r := range rooms.Data {
		names[r.ID] = r.Attrs.Title
	}
	var out []RemoteRate
	for _, p := range plans.Data {
		out = append(out, RemoteRate{RoomID: p.Attrs.RoomTypeID, RoomName: names[p.Attrs.RoomTypeID], RateID: p.ID, RateName: p.Attrs.Title, Occupancy: p.Attrs.Occupancy})
	}
	return out, nil
}
