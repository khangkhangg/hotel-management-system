package mail

import (
	"context"
	"errors"
	"fmt"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

type smtpSender struct {
	addr string
	auth smtp.Auth
	from string
}

// NewSMTP builds a Sender from a URL like smtp://user:password@host:587. from is the From header,
// for example "Sample Hotel <no-reply@hotel.example>". The connection upgrades to TLS when the server offers it.
func NewSMTP(rawURL, from string) (Sender, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "smtp" || u.Host == "" {
		return nil, fmt.Errorf("mail: %q is not an smtp:// url with a host", rawURL)
	}
	s := &smtpSender{addr: u.Host, from: from}
	if u.User != nil {
		pw, _ := u.User.Password()
		s.auth = smtp.PlainAuth("", u.User.Username(), pw, u.Hostname())
	}
	return s, nil
}

func (s *smtpSender) Send(ctx context.Context, m Message) error {
	for _, field := range []string{m.To, m.Subject, s.from} {
		if strings.ContainsAny(field, "\r\n") {
			return errors.New("mail: a header contains a line break")
		}
	}
	envelopeFrom := s.from
	if i := strings.LastIndex(envelopeFrom, "<"); i >= 0 {
		envelopeFrom = strings.TrimSuffix(envelopeFrom[i+1:], ">")
	}
	body := "From: " + s.from + "\r\nTo: " + m.To + "\r\nSubject: " + m.Subject +
		"\r\nDate: " + time.Now().UTC().Format(time.RFC1123Z) +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" +
		strings.ReplaceAll(strings.ReplaceAll(m.Text, "\r\n", "\n"), "\n", "\r\n")

	done := make(chan error, 1)
	go func() { done <- smtp.SendMail(s.addr, s.auth, envelopeFrom, []string{m.To}, []byte(body)) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(20 * time.Second):
		return errors.New("mail: the server took too long")
	}
}
