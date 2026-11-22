package mail_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"

	"hotelpms/api/internal/mail"
)

var ctx = context.Background()

func TestFakeRecordsWhatWasSent(t *testing.T) {
	f := mail.NewFake()
	if err := f.Send(ctx, mail.Message{To: "mai@hotel.com", Subject: "Hi", Text: "Body"}); err != nil {
		t.Fatal(err)
	}
	got := f.Sent()
	if len(got) != 1 || got[0].To != "mai@hotel.com" || got[0].Subject != "Hi" || got[0].Text != "Body" {
		t.Fatalf("%+v", got)
	}
}

func TestFakeFailNextFailsOnceAndRecordsNothing(t *testing.T) {
	f := mail.NewFake()
	f.FailNext()
	if err := f.Send(ctx, mail.Message{To: "a@b.c"}); err == nil {
		t.Fatal("the failure was not injected")
	}
	if len(f.Sent()) != 0 {
		t.Fatal("a failed send was recorded")
	}
	if err := f.Send(ctx, mail.Message{To: "a@b.c"}); err != nil || len(f.Sent()) != 1 {
		t.Fatalf("the fake did not recover: %v", err)
	}
}

// fakeSMTP is a minimal SMTP server: it accepts one message and returns what it received.
func fakeSMTP(t *testing.T) (addr string, got func() string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var mu sync.Mutex
	var data strings.Builder
	var rcpt []string
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r, w := bufio.NewReader(c), bufio.NewWriter(c)
		say := func(s string) { fmt.Fprintf(w, "%s\r\n", s); w.Flush() }
		say("220 fake ESMTP")
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			mu.Lock()
			switch {
			case inData && line == ".":
				inData = false
				mu.Unlock()
				say("250 queued")
				continue
			case inData:
				data.WriteString(line + "\n")
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
				mu.Unlock()
				say("250 hello")
				continue
			case strings.HasPrefix(line, "RCPT TO:"):
				rcpt = append(rcpt, line)
			case line == "DATA":
				inData = true
				mu.Unlock()
				say("354 go ahead")
				continue
			case line == "QUIT":
				mu.Unlock()
				say("221 bye")
				return
			}
			mu.Unlock()
			if !inData {
				say("250 ok")
			}
		}
	}()
	return ln.Addr().String(), func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.Join(rcpt, "|") + "\n" + data.String()
	}
}

func TestSMTPSendsTheMessage(t *testing.T) {
	addr, got := fakeSMTP(t)
	s, err := mail.NewSMTP("smtp://"+addr, "Sample Hotel <no-reply@hotel.example>")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send(ctx, mail.Message{To: "mai@hotel.com", Subject: "Your sign-in code", Text: "Code 123456"}); err != nil {
		t.Fatal(err)
	}
	seen := got()
	for _, want := range []string{"RCPT TO:<mai@hotel.com>", "Subject: Your sign-in code", "To: mai@hotel.com", "Code 123456", "From: Sample Hotel <no-reply@hotel.example>"} {
		if !strings.Contains(seen, want) {
			t.Errorf("the server did not receive %q in:\n%s", want, seen)
		}
	}
}

func TestSMTPRefusesHeaderInjection(t *testing.T) {
	addr, _ := fakeSMTP(t)
	s, _ := mail.NewSMTP("smtp://"+addr, "no-reply@hotel.example")
	for _, m := range []mail.Message{
		{To: "a@b.c\r\nBcc: evil@x.y", Subject: "x", Text: "x"},
		{To: "a@b.c", Subject: "x\r\nBcc: evil@x.y", Text: "x"},
	} {
		if err := s.Send(ctx, m); err == nil {
			t.Errorf("accepted %+v", m)
		}
	}
}

func TestSMTPReportsAnUnreachableServer(t *testing.T) {
	s, _ := mail.NewSMTP("smtp://127.0.0.1:1", "no-reply@hotel.example")
	if err := s.Send(ctx, mail.Message{To: "a@b.c", Subject: "x", Text: "x"}); err == nil {
		t.Fatal("no error from an unreachable server")
	}
}

func TestNewSMTPRejectsABadURL(t *testing.T) {
	for _, u := range []string{"", "http://x", "smtp://"} {
		if _, err := mail.NewSMTP(u, "a@b.c"); err == nil {
			t.Errorf("%q accepted", u)
		}
	}
}
