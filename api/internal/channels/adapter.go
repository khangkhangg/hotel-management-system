// Package channels keeps a property's rooms in step with the outside world (spec 04): it pushes what is left to sell
// and what it costs, and pulls the bookings the OTAs made. A property connects either through a channel manager
// or straight to one OTA. Both are an Adapter, so the sync and the screens do not care which.
package channels

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Slot is one night of one rate plan as the outside world should see it. Date is YYYY-MM-DD in the hotel's time zone.
type Slot struct {
	ExternalRoomID string
	ExternalRateID string
	Date           string
	Available      int
	Price          int64 // minor units of Currency
	Currency       string
}

// Booking is a reservation made on the outside. Status is "new" or "cancelled". Dates are YYYY-MM-DD.
type Booking struct {
	ExternalID     string
	Status         string
	GuestName      string
	Email          string
	Arrival        string
	Departure      string
	ExternalRoomID string
	ExternalRateID string
	Adults         int
	Children       int
}

// Adapter talks to one channel manager or one OTA.
type Adapter interface {
	// PushAvailability sends the nights. The adapter splits them into as many calls as the other side needs.
	PushAvailability(ctx context.Context, slots []Slot) error
	// PullBookings returns the bookings that are new or changed since the time given.
	PullBookings(ctx context.Context, since time.Time) ([]Booking, error)
}

// Connection is a property's saved choice, with the name of the server setting that holds the secret.
type Connection struct {
	Mode            string
	Provider        string
	ExternalHotelID string
	BaseURL         string
	SecretSetting   string
}

// Factory makes the adapter for a connection. It is where a server setting is turned into a secret.
type Factory func(Connection) (Adapter, error)

// ErrUnavailable means no adapter exists for the provider.
var ErrUnavailable = errors.New("channels: no adapter for this provider")

// Fixed always answers with the same adapter. Tests use it.
func Fixed(a Adapter) Factory { return func(Connection) (Adapter, error) { return a, nil } }

// Fake is an in-memory channel for tests and the browser tests.
type Fake struct {
	mu     sync.Mutex
	Pushed []Slot
	Inbox  []Booking
	Fail   error
}

func NewFake() *Fake { return &Fake{} }

func (f *Fake) PushAvailability(_ context.Context, slots []Slot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Fail != nil {
		return f.Fail
	}
	f.Pushed = append(f.Pushed, slots...)
	return nil
}

func (f *Fake) PullBookings(context.Context, time.Time) ([]Booking, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Fail != nil {
		return nil, f.Fail
	}
	return append([]Booking(nil), f.Inbox...), nil
}
