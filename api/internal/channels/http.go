package channels

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

const (
	batch     = 500
	maxReply  = 5 << 20
	otaStatus = "Commit"
)

// GuardedClient is an HTTP client that cannot reach the server's own network. The address of a channel is typed in
// by staff, so it is checked at connection time, after the name is resolved, which also stops a name that later points inward.
func GuardedClient() *http.Client {
	d := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
				return errors.New("channels: address is not on the public internet")
			}
			return nil
		},
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{DialContext: d.DialContext, Proxy: nil}}
}

// Real makes the adapters that talk over the internet. getenv turns the name of a server setting into its value.
func Real(getenv func(string) string) Factory {
	return func(c Connection) (Adapter, error) {
		switch c.Provider {
		case "cm_generic", "channex", "agoda", "booking_com", "expedia":
		default:
			return nil, ErrUnavailable
		}
		secret := getenv(c.SecretSetting)
		if c.SecretSetting == "" || secret == "" {
			return nil, errors.New("channels: the secret setting named for this connection is not set on the server")
		}
		if c.Provider == "channex" {
			if c.ExternalHotelID == "" || (c.BaseURL != "" && !validBaseURL(c.BaseURL)) {
				return nil, errors.New("channels: the Channex property id is needed, and any address must be an https name")
			}
			return NewChannex(c.BaseURL, c.ExternalHotelID, secret, GuardedClient()), nil
		}
		if c.BaseURL == "" || !validBaseURL(c.BaseURL) || c.ExternalHotelID == "" {
			return nil, errors.New("channels: the address and the hotel code are needed")
		}
		if c.Provider == "cm_generic" {
			return NewChannelManager(c.BaseURL, c.ExternalHotelID, secret, GuardedClient()), nil
		}
		return NewOpenTravel(c.BaseURL, c.ExternalHotelID, secret, GuardedClient()), nil
	}
}

func readReply(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReply))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("the other side answered %d", resp.StatusCode)
	}
	return body, nil
}

// channelManager speaks a plain JSON protocol, which a channel manager's own connector or a small bridge can serve:
//
//	POST {base}/availability  {"hotelId", "slots": [Slot...]}
//	GET  {base}/bookings?hotelId=&since=RFC3339  ->  {"bookings": [...]}
type channelManager struct {
	base, hotel, secret string
	c                   *http.Client
}

func NewChannelManager(base, hotel, secret string, c *http.Client) Adapter {
	return &channelManager{strings.TrimRight(base, "/"), hotel, secret, c}
}

func (a *channelManager) do(ctx context.Context, method, u string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.c.Do(req)
	if err != nil {
		return nil, err
	}
	return readReply(resp)
}

func (a *channelManager) PushAvailability(ctx context.Context, slots []Slot) error {
	for i := 0; i < len(slots); i += batch {
		end := min(i+batch, len(slots))
		body, err := json.Marshal(map[string]any{"hotelId": a.hotel, "slots": slots[i:end]})
		if err != nil {
			return err
		}
		if _, err := a.do(ctx, http.MethodPost, a.base+"/availability", body); err != nil {
			return err
		}
	}
	return nil
}

func (a *channelManager) PullBookings(ctx context.Context, since time.Time) ([]Booking, error) {
	q := url.Values{"hotelId": {a.hotel}, "since": {since.UTC().Format(time.RFC3339)}}
	body, err := a.do(ctx, http.MethodGet, a.base+"/bookings?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Bookings []struct {
			ID        string `json:"id"`
			Status    string `json:"status"`
			GuestName string `json:"guestName"`
			Email     string `json:"email"`
			Arrival   string `json:"arrival"`
			Departure string `json:"departure"`
			RoomID    string `json:"roomId"`
			RateID    string `json:"rateId"`
			Adults    int    `json:"adults"`
			Children  int    `json:"children"`
		} `json:"bookings"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	res := make([]Booking, 0, len(out.Bookings))
	for _, b := range out.Bookings {
		res = append(res, Booking{ExternalID: b.ID, Status: b.Status, GuestName: b.GuestName, Email: b.Email, Arrival: b.Arrival, Departure: b.Departure,
			ExternalRoomID: b.RoomID, ExternalRateID: b.RateID, Adults: b.Adults, Children: b.Children})
	}
	return res, nil
}

// openTravel speaks OpenTravel Alliance XML, the common language of the OTAs' connectivity programs. It sends
// OTA_HotelAvailNotifRQ and OTA_HotelRateAmountNotifRQ and reads OTA_ResRetrieveRS answers to an OTA_ReadRQ.
// shortcut: written from the OpenTravel message shapes, not yet run against Agoda, Booking.com or Expedia
// test environments, which need partner credentials; each OTA may need its own tags or endpoints.
type openTravel struct {
	base, hotel, secret string
	c                   *http.Client
}

func NewOpenTravel(base, hotel, secret string, c *http.Client) Adapter {
	return &openTravel{base, hotel, secret, c}
}

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func (a *openTravel) post(ctx context.Context, body string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base, strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>`+body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(a.hotel+":"+a.secret)))
	resp, err := a.c.Do(req)
	if err != nil {
		return nil, err
	}
	return readReply(resp)
}

func (a *openTravel) PushAvailability(ctx context.Context, slots []Slot) error {
	for i := 0; i < len(slots); i += batch {
		part := slots[i:min(i+batch, len(slots))]
		var avail, rates strings.Builder
		for _, s := range part {
			avail.WriteString(fmt.Sprintf(`<AvailStatusMessage BookingLimit="%d"><StatusApplicationControl Start="%s" End="%s" InvTypeCode="%s" RatePlanCode="%s"/></AvailStatusMessage>`,
				s.Available, esc(s.Date), esc(s.Date), esc(s.ExternalRoomID), esc(s.ExternalRateID)))
			rates.WriteString(fmt.Sprintf(`<RateAmountMessage><StatusApplicationControl Start="%s" End="%s" InvTypeCode="%s" RatePlanCode="%s"/><Rates><Rate><BaseByGuestAmts><BaseByGuestAmt AmountAfterTax="%s" CurrencyCode="%s"/></BaseByGuestAmts></Rate></Rates></RateAmountMessage>`,
				esc(s.Date), esc(s.Date), esc(s.ExternalRoomID), esc(s.ExternalRateID), money(s.Price, s.Currency), esc(s.Currency)))
		}
		if _, err := a.post(ctx, fmt.Sprintf(`<OTA_HotelAvailNotifRQ xmlns="http://www.opentravel.org/OTA/2003/05" Version="1.0"><AvailStatusMessages HotelCode="%s">%s</AvailStatusMessages></OTA_HotelAvailNotifRQ>`, esc(a.hotel), avail.String())); err != nil {
			return err
		}
		if _, err := a.post(ctx, fmt.Sprintf(`<OTA_HotelRateAmountNotifRQ xmlns="http://www.opentravel.org/OTA/2003/05" Version="1.0"><RateAmountMessages HotelCode="%s">%s</RateAmountMessages></OTA_HotelRateAmountNotifRQ>`, esc(a.hotel), rates.String())); err != nil {
			return err
		}
	}
	return nil
}

var zeroDecimal = map[string]bool{"VND": true, "JPY": true, "KRW": true, "CLP": true, "IDR": true}

func money(minor int64, currency string) string {
	if zeroDecimal[currency] {
		return fmt.Sprintf("%d", minor)
	}
	return fmt.Sprintf("%d.%02d", minor/100, minor%100)
}

type otaRes struct {
	Status string `xml:"ResStatus,attr"`
	ID     struct {
		ID string `xml:"ID,attr"`
	} `xml:"UniqueID"`
	RoomCode []struct {
		Code string `xml:"RoomTypeCode,attr"`
	} `xml:"RoomStays>RoomStay>RoomTypes>RoomType"`
	RateCode []struct {
		Code string `xml:"RatePlanCode,attr"`
	} `xml:"RoomStays>RoomStay>RatePlans>RatePlan"`
	Counts []struct {
		Age   string `xml:"AgeQualifyingCode,attr"`
		Count int    `xml:"Count,attr"`
	} `xml:"RoomStays>RoomStay>GuestCounts>GuestCount"`
	Span struct {
		Start string `xml:"Start,attr"`
		End   string `xml:"End,attr"`
	} `xml:"RoomStays>RoomStay>TimeSpan"`
	Given   string `xml:"ResGuests>ResGuest>Profiles>ProfileInfo>Profile>Customer>PersonName>GivenName"`
	Surname string `xml:"ResGuests>ResGuest>Profiles>ProfileInfo>Profile>Customer>PersonName>Surname"`
	Email   string `xml:"ResGuests>ResGuest>Profiles>ProfileInfo>Profile>Customer>Email"`
}

func (a *openTravel) PullBookings(ctx context.Context, since time.Time) ([]Booking, error) {
	body, err := a.post(ctx, fmt.Sprintf(`<OTA_ReadRQ xmlns="http://www.opentravel.org/OTA/2003/05" Version="1.0"><ReadRequests><HotelReadRequest HotelCode="%s"><SelectionCriteria Start="%s" SelectionType="Undelivered"/></HotelReadRequest></ReadRequests></OTA_ReadRQ>`,
		esc(a.hotel), since.UTC().Format("2006-01-02T15:04:05")))
	if err != nil {
		return nil, err
	}
	var doc struct {
		Res []otaRes `xml:"ReservationsList>HotelReservation"`
	}
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	out := make([]Booking, 0, len(doc.Res))
	for _, r := range doc.Res {
		b := Booking{ExternalID: r.ID.ID, Status: "new", GuestName: strings.TrimSpace(r.Given + " " + r.Surname), Email: r.Email, Arrival: r.Span.Start, Departure: r.Span.End}
		if r.Status == "Cancel" || r.Status == "Cancelled" {
			b.Status = "cancelled"
		}
		if len(r.RoomCode) > 0 {
			b.ExternalRoomID = r.RoomCode[0].Code
		}
		if len(r.RateCode) > 0 {
			b.ExternalRateID = r.RateCode[0].Code
		}
		for _, c := range r.Counts {
			switch c.Age {
			case "10":
				b.Adults += c.Count
			case "8":
				b.Children += c.Count
			}
		}
		out = append(out, b)
	}
	return out, nil
}
