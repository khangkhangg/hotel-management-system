package mail

import (
	"context"
	"errors"
	"sync"
)

// Fake is an in-memory Sender for tests.
type Fake struct {
	mu   sync.Mutex
	sent []Message
	fail bool
}

func NewFake() *Fake { return &Fake{} }

func (f *Fake) Send(_ context.Context, m Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		f.fail = false
		return errors.New("mail: injected failure")
	}
	f.sent = append(f.sent, m)
	return nil
}

// FailNext makes the next Send return an error. Later sends work again.
func (f *Fake) FailNext() {
	f.mu.Lock()
	f.fail = true
	f.mu.Unlock()
}

func (f *Fake) Sent() []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Message(nil), f.sent...)
}
