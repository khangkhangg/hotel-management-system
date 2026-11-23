package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// DefaultAgodaURL is Agoda's supply (YCS) host.
const DefaultAgodaURL = "https://supply.agoda.com"

// Agoda is the adapter for Agoda's YCS connectivity API, built from the public Direct Supply documentation. The secret
// is "clientId:clientSecret" from the API access page of the YCS portal. Agoda lists the property's rooms and rates
// (GetProduct), takes rates and inventory (SetAriV2) and lists bookings by the hour (GetBookingListV2 and
// GetBookingDetail). The public documents show no way to acknowledge a booking, so none is sent.
// shortcut: not run against Agoda; certification (GetProduct, SetAriV2, GetBookingList, GetBookingDetails)
// and a pilot with 2 or 3 live properties come first, and the date format of the booking list window is a guess.
type Agoda struct {
	base, property, id, secret string
	c                          *http.Client
	mu                         sync.Mutex
	token                      string
	expires                    time.Time
}

func NewAgoda(base, property, secret string, c *http.Client) *Agoda {
	if base == "" {
		base = DefaultAgodaURL
	}
	id, sec, _ := strings.Cut(secret, ":")
	return &Agoda{base: strings.TrimRight(base, "/"), property: property, id: id, secret: sec, c: c}
}

func (a *Agoda) bearer(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.token != "" && time.Now().Before(a.expires) {
		return a.token, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {a.id}, "client_secret": {a.secret}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/token-based-authentication/v2/exchange", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.c.Do(req)
	if err != nil {
		return "", err
	}
	body, err := readReply(resp)
	if err != nil {
		return "", fmt.Errorf("Agoda did not give a token (check the client id and secret, and that this server's address is allowed): %w", err)
	}
	var tok struct {
		Access string `json:"access_token"`
		Expiry int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.Access == "" {
		return "", errors.New("Agoda's token answer had no token")
	}
	a.token, a.expires = tok.Access, time.Now().Add(time.Duration(max(tok.Expiry-60, 60))*time.Second)
	return a.token, nil
}

// call sends one YCS request and returns the parsed answer. A 4xx answer carries an <errors> list, read into the error.
func (a *Agoda) call(ctx context.Context, kind int, criteria string) (*node, error) {
	tok, err := a.bearer(ctx)
	if err != nil {
		return nil, err
	}
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><request timestamp="%d" type="%d">%s</request>`, time.Now().UnixMilli(), kind, criteria)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/api", strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "text/xml")
	resp, err := a.c.Do(req)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(http.MaxBytesReader(nil, resp.Body, maxReply))
	resp.Body.Close()
	doc, perr := parseXML(buf.Bytes())
	if resp.StatusCode == http.StatusMultiStatus || resp.StatusCode >= 300 {
		var msgs []string
		if perr == nil {
			for _, e := range doc.all("error") {
				msgs = append(msgs, e.attrs["description"])
			}
		}
		if len(msgs) == 0 {
			msgs = append(msgs, fmt.Sprintf("Agoda answered %d", resp.StatusCode))
		}
		return nil, errors.New("Agoda refused: " + strings.Join(msgs, "; "))
	}
	if perr != nil {
		return nil, perr
	}
	return doc, nil
}

func (a *Agoda) PushAvailability(ctx context.Context, slots []Slot) error {
	type key struct{ room, date string }
	seen := map[key]bool{}
	for i := 0; i < len(slots); i += batch {
		var inv, rates strings.Builder
		for _, s := range slots[i:min(i+batch, len(slots))] {
			if k := (key{s.ExternalRoomID, s.Date}); !seen[k] {
				seen[k] = true
				fmt.Fprintf(&inv, `<update room_id="%s"><date_values><value>%s</value></date_values><allotment>%d</allotment></update>`, esc(s.ExternalRoomID), esc(s.Date), s.Available)
			}
			if s.ExternalRateID != "" && s.Price > 0 {
				fmt.Fprintf(&rates, `<update room_id="%s" rateplan_id="%s"><date_values><value>%s</value></date_values><prices currency="%s"><normal default="%s"/></prices></update>`,
					esc(s.ExternalRoomID), esc(s.ExternalRateID), esc(s.Date), esc(s.Currency), money(s.Price, s.Currency))
			}
		}
		crit := fmt.Sprintf(`<criteria property_id="%s">`, esc(a.property))
		if inv.Len() > 0 {
			crit += "<inventory>" + inv.String() + "</inventory>"
		}
		if rates.Len() > 0 {
			crit += "<rate>" + rates.String() + "</rate>"
		}
		if _, err := a.call(ctx, 10, crit+"</criteria>"); err != nil {
			return err
		}
	}
	return nil
}

const agodaWindow = time.Hour

func (a *Agoda) PullBookings(ctx context.Context, since time.Time) ([]Booking, error) {
	now := time.Now().UTC()
	if since.Before(now.Add(-72 * time.Hour)) { // a long gap is caught up over a few runs, not in one burst of calls
		since = now.Add(-72 * time.Hour)
	}
	var ids []string
	status := map[string]string{}
	for from := since.UTC(); from.Before(now); from = from.Add(agodaWindow) {
		to := from.Add(agodaWindow)
		doc, err := a.call(ctx, 9, fmt.Sprintf(`<criteria from="%s" to="%s"><property id="%s"/></criteria>`, from.Format("2006-01-02T15:04:05"), to.Format("2006-01-02T15:04:05"), esc(a.property)))
		if err != nil {
			return nil, err
		}
		for _, b := range doc.all("booking") {
			id := b.val("booking_id")
			if id == "" {
				continue
			}
			if _, dup := status[id]; !dup {
				ids = append(ids, id)
			}
			status[id] = b.val("status")
		}
	}
	var out []Booking
	for i := 0; i < len(ids); i += 50 {
		var list strings.Builder
		for _, id := range ids[i:min(i+50, len(ids))] {
			fmt.Fprintf(&list, `<booking id="%s"/>`, esc(id))
		}
		doc, err := a.call(ctx, 4, fmt.Sprintf(`<criteria language="EN"><property id="%s">%s</property></criteria>`, esc(a.property), list.String()))
		if err != nil {
			return nil, err
		}
		for _, b := range doc.all("booking") {
			id := b.val("booking_id")
			if id == "" {
				continue
			}
			st := b.val("status")
			if st == "" {
				st = status[id]
			}
			bk := Booking{ExternalID: id, Arrival: b.val("arrival"), Departure: b.val("departure"), ExternalRoomID: b.val("room_id"), ExternalRateID: b.val("rateplan_id"),
				GuestName: strings.TrimSpace(b.val("first_name") + " " + b.val("last_name")), Email: b.val("email"), Source: b.val("channel_name")}
			fmt.Sscanf(b.val("adults"), "%d", &bk.Adults)
			fmt.Sscanf(b.val("children"), "%d", &bk.Children)
			switch st {
			case "CancelBooking":
				bk.Status = "cancelled"
			case "AmendBooking":
				bk.Status = "modified"
			default:
				bk.Status = "new"
			}
			rooms := 0
			fmt.Sscanf(b.val("room_count"), "%d", &rooms)
			if n := rooms; n > 1 {
				bk.Problem = fmt.Sprintf("the booking has %d rooms, and this PMS keeps one room per imported booking", n)
			}
			out = append(out, bk)
		}
	}
	return out, nil
}

// Test asks for the property's products, which fails unless the credentials, the address allow-list and the hotel code are right.
func (a *Agoda) Test(ctx context.Context) error {
	_, err := a.call(ctx, 5, fmt.Sprintf(`<criteria language="EN"><property id="%s"><rooms></rooms><rateplans></rateplans></property></criteria>`, esc(a.property)))
	return err
}

func (a *Agoda) Remote(ctx context.Context) ([]RemoteRate, error) {
	doc, err := a.call(ctx, 5, fmt.Sprintf(`<criteria language="EN"><property id="%s"><rooms></rooms><rateplans></rateplans></property></criteria>`, esc(a.property)))
	if err != nil {
		return nil, err
	}
	rooms, rates := map[string]string{}, map[string]string{}
	for _, r := range doc.all("room") {
		rooms[r.attrs["room_id"]] = r.attrs["room_name"]
	}
	for _, r := range doc.all("rateplan") {
		rates[r.attrs["rateplan_id"]] = r.attrs["rateplan_name"]
	}
	var out []RemoteRate
	for _, p := range doc.all("product") {
		rm, rt := p.attrs["room_id"], p.attrs["rateplan_id"]
		out = append(out, RemoteRate{RoomID: rm, RoomName: rooms[rm], RateID: rt, RateName: rates[rt]})
	}
	return out, nil
}
