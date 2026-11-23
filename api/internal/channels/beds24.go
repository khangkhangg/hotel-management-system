package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBeds24URL is Beds24's API. Its documentation is public at beds24.com/api/v2.
const DefaultBeds24URL = "https://api.beds24.com/v2"

// beds24 is the adapter for Beds24, a channel manager with an open API. The hotel's code is the Beds24 property id,
// a mapped room is a Beds24 room type id, and the secret is a long life token made from an invite code in Beds24.
// Beds24 holds one price per room and night and never says a booking was modified, so every kept booking is
// reported as a change and the engine compares the stay with ours.
type beds24 struct {
	base, property, token string
	c                     *http.Client
}

// NewBeds24 makes the Beds24 adapter. An empty base means the real API.
func NewBeds24(base, property, token string, c *http.Client) Adapter {
	if base == "" {
		base = DefaultBeds24URL
	}
	return &beds24{strings.TrimRight(base, "/"), property, token, c}
}

func (a *beds24) do(ctx context.Context, method, path string, q url.Values, body any) ([]byte, error) {
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
	u := a.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("token", a.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.c.Do(req)
	if err != nil {
		return nil, err
	}
	out, err := readReply(resp)
	if err != nil {
		return nil, err
	}
	// Beds24 may answer 200 and say in the body that it refused. The body is one result or a list of them.
	var one struct {
		Success *bool  `json:"success"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(out, &one) == nil && one.Success != nil && !*one.Success {
		return out, fmt.Errorf("Beds24 refused the request: %s", one.Error)
	}
	var many []struct {
		Success *bool  `json:"success"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(out, &many) == nil {
		for _, m := range many {
			if m.Success != nil && !*m.Success {
				return out, fmt.Errorf("Beds24 refused the request: %s", m.Error)
			}
		}
	}
	return out, nil
}

func beds24ID(s string) any {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return s
}

func (a *beds24) PushAvailability(ctx context.Context, slots []Slot) error {
	for _, part := range chunks(slots) {
		type row struct {
			From     string      `json:"from"`
			To       string      `json:"to"`
			NumAvail int         `json:"numAvail"`
			Price1   json.Number `json:"price1,omitempty"`
		}
		type room struct {
			RoomID   any   `json:"roomId"`
			Calendar []row `json:"calendar"`
		}
		var rooms []*room
		at := map[string]*room{}
		for _, s := range part {
			r := at[s.ExternalRoomID]
			if r == nil {
				r = &room{RoomID: beds24ID(s.ExternalRoomID)}
				at[s.ExternalRoomID] = r
				rooms = append(rooms, r)
			}
			x := row{From: s.Date, To: s.Date, NumAvail: s.Available}
			if s.Price > 0 {
				x.Price1 = json.Number(money(s.Price, s.Currency))
			}
			r.Calendar = append(r.Calendar, x)
		}
		if _, err := a.do(ctx, http.MethodPost, "/inventory/rooms/calendar", nil, rooms); err != nil {
			return err
		}
	}
	return nil
}

func (a *beds24) PullBookings(ctx context.Context, since time.Time) ([]Booking, error) {
	var out []Booking
	for page := 1; page <= 20; page++ {
		q := url.Values{"propertyId": {a.property}, "modifiedFrom": {since.UTC().Format(time.RFC3339)}, "page": {strconv.Itoa(page)}}
		body, err := a.do(ctx, http.MethodGet, "/bookings", q, nil)
		if err != nil {
			return nil, err
		}
		var doc struct {
			Data []struct {
				ID        int64  `json:"id"`
				RoomID    int64  `json:"roomId"`
				Status    string `json:"status"`
				Arrival   string `json:"arrival"`
				Departure string `json:"departure"`
				NumAdult  int    `json:"numAdult"`
				NumChild  int    `json:"numChild"`
				FirstName string `json:"firstName"`
				LastName  string `json:"lastName"`
				Email     string `json:"email"`
				Channel   string `json:"channel"`
			} `json:"data"`
			Pages struct {
				NextPageExists bool `json:"nextPageExists"`
			} `json:"pages"`
		}
		if err := json.Unmarshal(body, &doc); err != nil {
			return nil, err
		}
		for _, b := range doc.Data {
			var status string
			switch b.Status {
			case "confirmed", "new":
				status = "modified"
			case "cancelled":
				status = "cancelled"
			default:
				continue // a request, an inquiry or a blocked night is not a booking
			}
			out = append(out, Booking{ExternalID: strconv.FormatInt(b.ID, 10), Status: status, GuestName: strings.TrimSpace(b.FirstName + " " + b.LastName),
				Email: b.Email, Arrival: b.Arrival, Departure: b.Departure, ExternalRoomID: strconv.FormatInt(b.RoomID, 10),
				Adults: b.NumAdult, Children: b.NumChild, Source: b.Channel})
		}
		if !doc.Pages.NextPageExists {
			break
		}
	}
	return out, nil
}

// Test checks the token.
func (a *beds24) Test(ctx context.Context) error {
	body, err := a.do(ctx, http.MethodGet, "/authentication/details", nil, nil)
	if err != nil {
		return err
	}
	var d struct {
		ValidToken bool `json:"validToken"`
	}
	if err := json.Unmarshal(body, &d); err != nil || !d.ValidToken {
		return errors.New("Beds24 does not accept this token")
	}
	return nil
}

// Remote lists the property's room types, for the mapping step. Beds24 has no rate plans: one price per room.
func (a *beds24) Remote(ctx context.Context) ([]RemoteRate, error) {
	body, err := a.do(ctx, http.MethodGet, "/properties", url.Values{"id": {a.property}, "includeAllRooms": {"true"}}, nil)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Data []struct {
			RoomTypes []struct {
				ID        int64  `json:"id"`
				Name      string `json:"name"`
				MaxPeople int    `json:"maxPeople"`
			} `json:"roomTypes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	var out []RemoteRate
	for _, p := range doc.Data {
		for _, r := range p.RoomTypes {
			out = append(out, RemoteRate{RoomID: strconv.FormatInt(r.ID, 10), RoomName: r.Name, Occupancy: r.MaxPeople})
		}
	}
	return out, nil
}
