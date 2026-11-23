package channels

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const otaNS = `xmlns="http://www.opentravel.org/OTA/2003/05"`

// sendXML posts an XML body and returns the answer read as a tree.
func sendXML(ctx context.Context, c *http.Client, method, u string, body string, headers map[string]string) (*node, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	out, err := readReply(resp)
	if err != nil {
		return nil, err
	}
	doc, err := parseXML(out)
	if err != nil {
		return nil, fmt.Errorf("the answer was not XML: %w", err)
	}
	return doc, nil
}

// otaError turns the errors of an OpenTravel answer into one error.
func otaError(who string, doc *node) error {
	var msgs []string
	for _, e := range doc.all("Error") {
		m := e.val("ShortText", "message", "description")
		if m == "" {
			m = strings.TrimSpace(e.text)
		}
		if c := e.attrs["Code"] + e.attrs["code"]; c != "" {
			m = c + " " + m
		}
		msgs = append(msgs, strings.TrimSpace(m))
	}
	if len(msgs) == 0 {
		return nil
	}
	return errors.New(who + " refused: " + strings.Join(msgs, "; "))
}

func basicAuth(secret string) map[string]string {
	if user, pass, ok := strings.Cut(secret, ":"); ok {
		return map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))}
	}
	if secret != "" {
		return map[string]string{"Authorization": "Bearer " + secret}
	}
	return nil
}

func partnerPOS(key string) string {
	if key == "" {
		return ""
	}
	return fmt.Sprintf(`<POS><Source><RequestorID ID="%s"/></Source></POS>`, esc(key))
}

func rateMessage(hotel, partner string, slots []Slot) string {
	var b strings.Builder
	for _, s := range slots {
		if s.ExternalRateID == "" || s.Price <= 0 {
			continue
		}
		fmt.Fprintf(&b, `<RateAmountMessage><StatusApplicationControl Start="%s" End="%s" InvTypeCode="%s" RatePlanCode="%s"/><Rates><Rate><BaseByGuestAmts><BaseByGuestAmt AmountAfterTax="%s" CurrencyCode="%s"/></BaseByGuestAmts></Rate></Rates></RateAmountMessage>`,
			esc(s.Date), esc(s.Date), esc(s.ExternalRoomID), esc(s.ExternalRateID), money(s.Price, s.Currency), esc(s.Currency))
	}
	if b.Len() == 0 {
		return ""
	}
	return fmt.Sprintf(`<OTA_HotelRateAmountNotifRQ %s Version="3.0" NotifType="Overlay" NotifScopeType="ProductRate">%s<RateAmountMessages HotelCode="%s">%s</RateAmountMessages></OTA_HotelRateAmountNotifRQ>`,
		otaNS, partnerPOS(partner), esc(hotel), b.String())
}

func chunks(slots []Slot) [][]Slot {
	var out [][]Slot
	for i := 0; i < len(slots); i += batch {
		out = append(out, slots[i:min(i+batch, len(slots))])
	}
	return out
}

// Google publishes prices and availability to Google Hotels. Google sends guests to the hotel's own site, so it returns
// no bookings. It is told what is left to sell (inventory), what it costs (rates) and whether the nights may be sold (availability),
// through the OpenTravel messages of Google's ARI delivery mode. The hotel code is the id in the Hotel List feed;
// the secret, if one is named, is the partner key from the Hotel Center account settings. Google allows the server by IP address,
// which the hotel sets in Hotel Center, so no password is sent.
// shortcut: needs the property data (rooms and rate plans) and landing pages set up in Hotel Center first, and Google's
// onboarding; not run against Google.
type Google struct {
	base, hotel, partner string
	c                    *http.Client
}

const DefaultGoogleURL = "https://www.google.com"

func NewGoogle(base, hotel, partner string, c *http.Client) Adapter {
	if base == "" {
		base = DefaultGoogleURL
	}
	return &Google{strings.TrimRight(base, "/"), hotel, partner, c}
}

func (g *Google) PushAvailability(ctx context.Context, slots []Slot) error {
	type key struct{ room, date string }
	seen := map[key]bool{}
	for _, part := range chunks(slots) {
		var inv, avail strings.Builder
		for _, s := range part {
			if k := (key{s.ExternalRoomID, s.Date}); !seen[k] {
				seen[k] = true
				fmt.Fprintf(&inv, `<Inventory><StatusApplicationControl Start="%s" End="%s" InvTypeCode="%s"/><InvCounts><InvCount Count="%d" CountType="2"/></InvCounts></Inventory>`, esc(s.Date), esc(s.Date), esc(s.ExternalRoomID), s.Available)
			}
			status := "Open"
			if s.Available <= 0 {
				status = "Close"
			}
			// Google treats every product as closed until it is told otherwise.
			fmt.Fprintf(&avail, `<AvailStatusMessage><StatusApplicationControl Start="%s" End="%s" InvTypeCode="%s" RatePlanCode="%s"/><RestrictionStatus Status="%s" Restriction="Master"/></AvailStatusMessage>`,
				esc(s.Date), esc(s.Date), esc(s.ExternalRoomID), esc(s.ExternalRateID), status)
		}
		msgs := []struct{ path, xml string }{
			{"/travel/hotels/uploads/ota/hotel_inv_count_notif", fmt.Sprintf(`<OTA_HotelInvCountNotifRQ %s Version="3.0">%s<Inventories HotelCode="%s">%s</Inventories></OTA_HotelInvCountNotifRQ>`, otaNS, partnerPOS(g.partner), esc(g.hotel), inv.String())},
			{"/travel/hotels/uploads/ota/hotel_rate_amount_notif", rateMessage(g.hotel, g.partner, part)},
			{"/travel/hotels/uploads/ota/hotel_avail_notif", fmt.Sprintf(`<OTA_HotelAvailNotifRQ %s Version="3.0">%s<AvailStatusMessages HotelCode="%s">%s</AvailStatusMessages></OTA_HotelAvailNotifRQ>`, otaNS, partnerPOS(g.partner), esc(g.hotel), avail.String())},
		}
		for _, m := range msgs {
			if m.xml == "" {
				continue
			}
			doc, err := sendXML(ctx, g.c, http.MethodPost, g.base+m.path, m.xml, nil)
			if err != nil {
				return err
			}
			if err := otaError("Google", doc); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *Google) PullBookings(context.Context, time.Time) ([]Booking, error) { return nil, nil }

// BookingCom is the adapter for Booking.com's OpenTravel connectivity: availability and rates go to
// OTA_HotelAvailNotif and OTA_HotelRateAmountNotif, and reservations are read from OTA_HotelResNotif (new)
// and OTA_HotelResModifyNotif (changed and cancelled) and acknowledged after they are saved. The secret is the machine
// account as "user:password", or a token.
// shortcut: Booking.com is moving machine accounts to short-lived tokens (the old scheme ends 31 Dec 2025), and their
// token request is not public; with a token, paste a current one until a token call is added. The acknowledgement
// body and the exact tags were not verifiable from public pages. Certification with Connectivity Support comes first.
type BookingCom struct {
	ari, res, hotel, secret string
	c                       *http.Client
}

func NewBookingCom(base, hotel, secret string, c *http.Client) Adapter {
	if base == "" {
		return &BookingCom{"https://supply-xml.booking.com", "https://secure-supply-xml.booking.com", hotel, secret, c}
	}
	base = strings.TrimRight(base, "/")
	return &BookingCom{base, base, hotel, secret, c}
}

func (b *BookingCom) PushAvailability(ctx context.Context, slots []Slot) error {
	for _, part := range chunks(slots) {
		var avail strings.Builder
		for _, s := range part {
			fmt.Fprintf(&avail, `<AvailStatusMessage BookingLimit="%d"><StatusApplicationControl Start="%s" End="%s" InvTypeCode="%s" RatePlanCode="%s"/></AvailStatusMessage>`,
				s.Available, esc(s.Date), esc(s.Date), esc(s.ExternalRoomID), esc(s.ExternalRateID))
		}
		msgs := []struct{ path, xml string }{
			{"/hotels/ota/OTA_HotelAvailNotif", fmt.Sprintf(`<OTA_HotelAvailNotifRQ %s Version="1.0"><AvailStatusMessages HotelCode="%s">%s</AvailStatusMessages></OTA_HotelAvailNotifRQ>`, otaNS, esc(b.hotel), avail.String())},
			{"/hotels/ota/OTA_HotelRateAmountNotif", rateMessage(b.hotel, "", part)},
		}
		for _, m := range msgs {
			if m.xml == "" {
				continue
			}
			doc, err := sendXML(ctx, b.c, http.MethodPost, b.ari+m.path, m.xml, basicAuth(b.secret))
			if err != nil {
				return err
			}
			if err := otaError("Booking.com", doc); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *BookingCom) PullBookings(ctx context.Context, since time.Time) ([]Booking, error) {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		loc = time.UTC
	}
	limit := time.Now().Add(-13 * 24 * time.Hour) // Booking.com looks back two weeks at most
	if since.Before(limit) {
		since = limit
	}
	var out []Booking
	for _, kind := range []struct{ path, prefix string }{{"/hotels/ota/OTA_HotelResNotif", "new"}, {"/hotels/ota/OTA_HotelResModifyNotif", "mod"}} {
		q := url.Values{"hotel_ids": {b.hotel}, "last_change": {since.In(loc).Format("2006-01-02 15:04:05")}, "limit": {"100"}}
		doc, err := sendXML(ctx, b.c, http.MethodGet, b.res+kind.path+"?"+q.Encode(), "", basicAuth(b.secret))
		if err != nil {
			return nil, err
		}
		if err := otaError("Booking.com", doc); err != nil {
			return nil, err
		}
		for _, r := range doc.all("HotelReservation") {
			id := ""
			if h := r.first("HotelReservationID"); h != nil {
				id = h.attrs["ResID_Value"]
			}
			if id == "" {
				continue
			}
			bk := Booking{ExternalID: id, AckID: kind.prefix + ":" + id, Status: "new", Arrival: r.val("Start"), Departure: r.val("End"),
				ExternalRoomID: r.val("RoomTypeCode"), ExternalRateID: r.val("RatePlanCode"), Email: r.val("Email"),
				GuestName: strings.TrimSpace(r.val("GivenName") + " " + r.val("Surname"))}
			if kind.prefix == "mod" {
				bk.Status = "modified"
			}
			if s := r.attrs["ResStatus"]; s == "Cancel" || s == "Cancelled" {
				bk.Status = "cancelled"
			}
			for _, gc := range r.all("GuestCount") {
				n := 0
				fmt.Sscanf(gc.attrs["Count"], "%d", &n)
				switch gc.attrs["AgeQualifyingCode"] {
				case "10":
					bk.Adults += n
				case "8":
					bk.Children += n
				}
			}
			if n := len(r.all("RoomStay")); n > 1 {
				bk.Problem = fmt.Sprintf("the booking has %d rooms, and this PMS keeps one room per imported booking", n)
			}
			out = append(out, bk)
		}
	}
	return out, nil
}

// Ack tells Booking.com the reservations are saved; an unacknowledged one is sent again, and after 30 minutes the hotel is emailed.
func (b *BookingCom) Ack(ctx context.Context, ids []string) error {
	byKind := map[string][]string{"new": nil, "mod": nil}
	for _, id := range ids {
		kind, res, _ := strings.Cut(id, ":")
		byKind[kind] = append(byKind[kind], res)
	}
	for kind, path := range map[string]string{"new": "/hotels/ota/OTA_HotelResNotif", "mod": "/hotels/ota/OTA_HotelResModifyNotif"} {
		if len(byKind[kind]) == 0 {
			continue
		}
		var res strings.Builder
		for _, id := range byKind[kind] {
			fmt.Fprintf(&res, `<HotelReservation><ResGlobalInfo><HotelReservationIDs><HotelReservationID ResID_Value="%s"/></HotelReservationIDs></ResGlobalInfo></HotelReservation>`, esc(id))
		}
		body := fmt.Sprintf(`<OTA_NotifReportRQ %s Version="1.0"><Success/><NotifDetails><HotelNotifReport><HotelReservations>%s</HotelReservations></HotelNotifReport></NotifDetails></OTA_NotifReportRQ>`, otaNS, res.String())
		doc, err := sendXML(ctx, b.c, http.MethodPost, b.res+path, body, basicAuth(b.secret))
		if err != nil {
			return err
		}
		if err := otaError("Booking.com", doc); err != nil {
			return err
		}
	}
	return nil
}

// Expedia is the adapter for Expedia QuickConnect (EQC): availability and rates go to /eqc/ar, bookings are read from
// /eqc/br and confirmed at /eqc/bc. The secret is the EQC login as "username:password", sent inside each message.
// shortcut: Expedia does not take direct connections from single hotels, so a PMS company applies as a connectivity
// provider first, and its documentation is behind the partner portal; the message layout here is from the long-published EQC
// format and must be checked against Expedia's schema before go-live.
type Expedia struct {
	base, hotel, user, pass string
	c                       *http.Client
}

const DefaultExpediaURL = "https://services.expediapartnercentral.com"

func NewExpedia(base, hotel, secret string, c *http.Client) Adapter {
	if base == "" {
		base = DefaultExpediaURL
	}
	u, p, _ := strings.Cut(secret, ":")
	return &Expedia{strings.TrimRight(base, "/"), hotel, u, p, c}
}

func (e *Expedia) auth() string {
	return fmt.Sprintf(`<Authentication username="%s" password="%s"/><Hotel id="%s"/>`, esc(e.user), esc(e.pass), esc(e.hotel))
}

func (e *Expedia) PushAvailability(ctx context.Context, slots []Slot) error {
	for _, part := range chunks(slots) {
		var upd strings.Builder
		for _, s := range part {
			plan := ""
			if s.ExternalRateID != "" && s.Price > 0 {
				plan = fmt.Sprintf(`<RatePlan id="%s" closed="false"><Rate currency="%s"><PerDay rate="%s"/></Rate></RatePlan>`, esc(s.ExternalRateID), esc(s.Currency), money(s.Price, s.Currency))
			}
			fmt.Fprintf(&upd, `<AvailRateUpdate><DateRange from="%s" to="%s"/><RoomType id="%s" closed="%t"><Inventory totalInventoryAvailable="%d"/>%s</RoomType></AvailRateUpdate>`,
				esc(s.Date), esc(s.Date), esc(s.ExternalRoomID), s.Available <= 0, s.Available, plan)
		}
		body := fmt.Sprintf(`<AvailRateUpdateRQ xmlns="urn:expedia:eqc:ar:2007:09:01">%s%s</AvailRateUpdateRQ>`, e.auth(), upd.String())
		doc, err := sendXML(ctx, e.c, http.MethodPost, e.base+"/eqc/ar", body, nil)
		if err != nil {
			return err
		}
		if err := otaError("Expedia", doc); err != nil {
			return err
		}
	}
	return nil
}

func (e *Expedia) PullBookings(ctx context.Context, _ time.Time) ([]Booking, error) {
	body := fmt.Sprintf(`<BookingRetrievalRQ xmlns="urn:expedia:eqc:br:2007:09:01">%s<ParamSet><Bookings/></ParamSet></BookingRetrievalRQ>`, e.auth())
	doc, err := sendXML(ctx, e.c, http.MethodPost, e.base+"/eqc/br", body, nil)
	if err != nil {
		return nil, err
	}
	if err := otaError("Expedia", doc); err != nil {
		return nil, err
	}
	var out []Booking
	for _, b := range doc.all("Booking") {
		id, typ := b.attrs["id"], b.attrs["type"]
		if id == "" {
			continue
		}
		bk := Booking{ExternalID: id, AckID: id + "|" + typ, Status: "new", Arrival: b.val("arrival"), Departure: b.val("departure"),
			ExternalRoomID: b.val("roomTypeID"), ExternalRateID: b.val("ratePlanID"), Email: b.val("Email"),
			GuestName: strings.TrimSpace(b.val("givenName") + " " + b.val("surname"))}
		fmt.Sscanf(b.val("adult"), "%d", &bk.Adults)
		fmt.Sscanf(b.val("child"), "%d", &bk.Children)
		switch typ {
		case "Cancel":
			bk.Status = "cancelled"
		case "Modify":
			bk.Status = "modified"
		}
		if n := len(b.all("RoomStay")); n > 1 {
			bk.Problem = fmt.Sprintf("the booking has %d rooms, and this PMS keeps one room per imported booking", n)
		}
		out = append(out, bk)
	}
	return out, nil
}

func (e *Expedia) Ack(ctx context.Context, ids []string) error {
	var conf strings.Builder
	for _, id := range ids {
		bid, typ, _ := strings.Cut(id, "|")
		fmt.Fprintf(&conf, `<BookingConfirmNumber bookingID="%s" type="%s" confirmNumber="%s" confirmTime="%s"/>`, esc(bid), esc(typ), esc(bid), time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	}
	body := fmt.Sprintf(`<BookingConfirmRQ xmlns="urn:expedia:eqc:bc:2007:09:01">%s<BookingConfirmNumbers>%s</BookingConfirmNumbers></BookingConfirmRQ>`, e.auth(), conf.String())
	doc, err := sendXML(ctx, e.c, http.MethodPost, e.base+"/eqc/bc", body, nil)
	if err != nil {
		return err
	}
	return otaError("Expedia", doc)
}
