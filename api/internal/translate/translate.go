// Package translate turns a hotel's own text into another language with an open source service (LibreTranslate), so a
// hotel that writes in one language can fill in the others. The result is a draft that a person can edit.
package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Translator translates one text between two of the guest site's language codes.
type Translator interface {
	Translate(ctx context.Context, text, from, to string) (string, error)
}

// Libre talks to a LibreTranslate server, either one the hotel group runs or a hosted one.
type Libre struct {
	base, key string
	c         *http.Client
}

// NewLibre makes the client. key may be empty for a server that needs none.
func NewLibre(base, key string, c *http.Client) *Libre {
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	return &Libre{strings.TrimRight(base, "/"), key, c}
}

// code maps the guest site's codes to LibreTranslate's.
func code(l string) string {
	if l == "zh-Hans" {
		return "zh"
	}
	return l
}

func (l *Libre) Translate(ctx context.Context, text, from, to string) (string, error) {
	body, err := json.Marshal(map[string]string{"q": text, "source": code(from), "target": code(to), "format": "text", "api_key": l.key})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.base+"/translate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := l.c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("translate: the service answered %d", resp.StatusCode)
	}
	var out struct {
		Text string `json:"translatedText"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || strings.TrimSpace(out.Text) == "" {
		return "", errors.New("translate: the service gave no translation")
	}
	return out.Text, nil
}

// Fake marks each text with the language it was sent to, for tests and for a machine with no service.
type Fake struct {
	mu    sync.Mutex
	Fail  bool
	Calls int
}

func NewFake() *Fake { return &Fake{} }

func (f *Fake) Translate(_ context.Context, text, _, to string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	if f.Fail {
		return "", errors.New("translate: the fake was told to fail")
	}
	return "[" + to + "] " + text, nil
}
