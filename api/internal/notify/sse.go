package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/session"
)

// Hub listens on the PostgreSQL channel "notifications" and wakes the open streams of the people named in it,
// so a notification reaches a screen without a reload, whichever server instance sent it.
type Hub struct {
	mu     sync.Mutex
	subs   map[uuid.UUID]map[chan struct{}]struct{}
	cancel context.CancelFunc
	done   chan struct{}
}

// NewHub starts listening. It returns once the listening has begun, so nothing sent after it is missed.
func NewHub(pool *pgxpool.Pool) (*Hub, error) {
	ctx, cancel := context.WithCancel(context.Background())
	conn, err := listen(ctx, pool)
	if err != nil {
		cancel()
		return nil, err
	}
	h := &Hub{subs: map[uuid.UUID]map[chan struct{}]struct{}{}, cancel: cancel, done: make(chan struct{})}
	go h.run(ctx, pool, conn)
	return h, nil
}

// listen takes a connection out of the pool for good, because a listening connection must not be shared.
func listen(ctx context.Context, pool *pgxpool.Pool) (*pgx.Conn, error) {
	pc, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	conn := pc.Hijack()
	if _, err := conn.Exec(ctx, `listen notifications`); err != nil {
		_ = conn.Close(ctx)
		return nil, err
	}
	return conn, nil
}

func (h *Hub) run(ctx context.Context, pool *pgxpool.Pool, conn *pgx.Conn) {
	defer close(h.done)
	for {
		n, err := conn.WaitForNotification(ctx)
		if err == nil {
			if id, perr := uuid.Parse(n.Payload); perr == nil {
				h.poke(id)
			}
			continue
		}
		_ = conn.Close(context.Background())
		for ctx.Err() == nil { // the connection dropped: reconnect, and tell every open stream to look again
			time.Sleep(time.Second)
			if conn, err = listen(ctx, pool); err == nil {
				h.pokeAll()
				break
			}
			log.Printf("notification listener: %v", err)
		}
		if ctx.Err() != nil {
			return
		}
	}
}

// Close stops listening and releases the connection.
func (h *Hub) Close() {
	h.cancel()
	<-h.done
}

func (h *Hub) subscribe(user uuid.UUID) (wake <-chan struct{}, unsubscribe func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	if h.subs[user] == nil {
		h.subs[user] = map[chan struct{}]struct{}{}
	}
	h.subs[user][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs[user], ch)
		if len(h.subs[user]) == 0 {
			delete(h.subs, user)
		}
		h.mu.Unlock()
	}
}

func wake(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default: // already woken; the stream will read the latest state anyway
	}
}

func (h *Hub) poke(user uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[user] {
		wake(ch)
	}
}

func (h *Hub) pokeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, set := range h.subs {
		for ch := range set {
			wake(ch)
		}
	}
}

// stream is a server-sent-events response. Each "notification" event carries the same body as GET /api/notifications.
func (h *handler) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	s := httpx.SessionFrom(r.Context())
	if !ok || s.OrgID == nil {
		httpx.Error(w, http.StatusUnauthorized, "organization_required", "Choose an organization to continue.")
		return
	}
	events, unsubscribe := h.hub.subscribe(s.UserID)
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	beat := time.NewTicker(25 * time.Second)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-beat.C:
			if !h.alive(r.Context(), s.ID) {
				return
			}
			fmt.Fprint(w, ": keep-alive\n\n")
		case <-events:
			if !h.alive(r.Context(), s.ID) { // signed out, expired or deactivated since the stream opened
				return
			}
			l, err := h.list(r.Context(), *s.OrgID, s.UserID)
			if err != nil {
				log.Printf("notification stream: %v", err)
				return
			}
			body, _ := json.Marshal(l)
			fmt.Fprintf(w, "event: notification\ndata: %s\n\n", body)
		}
		flusher.Flush()
	}
}

// alive reports whether the session is still valid, so a stream ends when its person is signed out or deactivated.
func (h *handler) alive(ctx context.Context, id string) bool {
	err := db.WithoutOrg(ctx, h.Pool, func(tx pgx.Tx) error {
		_, err := session.Lookup(ctx, tx, id, h.Clock())
		return err
	})
	return err == nil
}
