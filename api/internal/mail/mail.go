// Package mail sends plain-text email. Everything that sends mail depends on the Sender interface,
// so tests use the in-memory Fake and production uses SMTP.
package mail

import "context"

type Message struct {
	To      string
	Subject string
	Text    string
}

type Sender interface {
	Send(ctx context.Context, m Message) error
}
