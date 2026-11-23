package notes_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/activity"
	"hotelpms/api/internal/alerts"
	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/folio"
	"hotelpms/api/internal/housekeeping"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/notes"
	"hotelpms/api/internal/notify"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/testkit"
)

var ctx = testkit.Ctx

func must(t *testing.T, err error) { t.Helper(); testkit.Must(t, err) }

type hotel struct {
	w                             *testkit.World
	owner, mgr, desk, rev, keeper *testkit.Person
	prop                          uuid.UUID
	rt, rp, res                   string
}

func idOf(t *testing.T, r testkit.Reply) string {
	t.Helper()
	var b struct{ ID string }
	must(t, json.Unmarshal([]byte(r.Body), &b))
	return b.ID
}

func newHotel(t *testing.T) *hotel {
	w := testkit.NewWith(t, func(w *testkit.World) []httpx.Mount {
		hub, err := notify.NewHub(w.Pool)
		must(t, err)
		t.Cleanup(hub.Close)
		return []httpx.Mount{rooms.Mount, booking.Mount, folio.Mount, housekeeping.Mount, alerts.Mount, activity.Mount, notify.Mount(hub), notes.Mount(hub)}
	})
	h := &hotel{w: w, prop: w.A1}
	h.owner = w.Member(w.OrgA, nil, "owner")
	h.mgr = w.Member(w.OrgA, &w.A1, "manager")
	h.desk = w.Member(w.OrgA, &w.A1, "front_desk")
	h.rev = w.Member(w.OrgA, &w.A1, "revenue")
	h.keeper = w.Member(w.OrgA, &w.A1, "housekeeping")
	h.rt = idOf(t, h.mgr.Do("POST", "/api/room-types", &h.prop, `{"name":"Deluxe","baseGuests":2,"maxGuests":3}`).Want(t, 201))
	h.mgr.Do("POST", "/api/rooms", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"number":"101","floor":1}`, h.rt)).Want(t, 201)
	h.rp = idOf(t, h.mgr.Do("POST", "/api/rate-plans", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"name":"Flexible"}`, h.rt)).Want(t, 201))
	h.mgr.Do("PUT", "/api/rates", &h.prop, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-11-23","to":"2027-01-01","price":10000}`, h.rp)).Want(t, 200)
	h.res = idOf(t, h.desk.Do("POST", "/api/reservations", &h.prop, fmt.Sprintf(
		`{"guest":{"name":"Mai Pham","email":"mai@hotel.com","phone":"+84 90 111 2222"},"rooms":[{"roomTypeId":%q,"ratePlanId":%q,"from":"2026-11-23","to":"2026-11-25","adults":2,"children":0}]}`, h.rt, h.rp)).Want(t, 201))
	return h
}

func (h *hotel) roomID() string {
	var rows []struct{ ID string }
	must(h.w.T, json.Unmarshal([]byte(h.mgr.Do("GET", "/api/rooms", &h.prop, "").Want(h.w.T, 200).Body), &rows))
	return rows[0].ID
}

func (h *hotel) note(p *testkit.Person, body string) testkit.Reply {
	return p.Do("POST", "/api/reservations/"+h.res+"/notes", &h.prop, body)
}

func (h *hotel) notifications(p *testkit.Person) (n int, text string) {
	h.w.T.Helper()
	must(h.w.T, db.WithOrg(ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select count(*), coalesce(string_agg(title || ' ' || body || ' ' || action_url, ' | '), '') from notification where user_id = $1 and key like 'note:%'`, p.ID).Scan(&n, &text)
	}))
	return
}

type threadItem struct {
	Type    string
	ID      string
	Body    *string
	Author  string
	Removed bool
	Pinned  bool
	Action  string
	At      time.Time
}

type thread struct {
	Pinned []threadItem
	Items  []threadItem
}

func (h *hotel) thread(p *testkit.Person) thread {
	h.w.T.Helper()
	var t thread
	must(h.w.T, json.Unmarshal([]byte(p.Do("GET", "/api/reservations/"+h.res+"/thread", &h.prop, "").Want(h.w.T, 200).Body), &t))
	return t
}

func TestTaggingAColleagueMakesExactlyOneNotification(t *testing.T) {
	h := newHotel(t)
	h.note(h.desk, fmt.Sprintf(`{"body":"Please call the guest","mentions":[{"userId":%q}]}`, h.mgr.ID)).Want(t, 201)
	n, text := h.notifications(h.mgr)
	if n != 1 || !strings.Contains(text, "Please call the guest") || !strings.Contains(text, "/calendar") {
		t.Fatalf("%d notifications: %s", n, text)
	}
	if n, _ := h.notifications(h.desk); n != 0 {
		t.Fatal("the author notified themself")
	}
	// Tagging the same person twice, once by name and once by role, is still one.
	h.note(h.desk, fmt.Sprintf(`{"body":"Both ways","mentions":[{"userId":%q},{"role":"manager"}]}`, h.mgr.ID)).Want(t, 201)
	if n, _ := h.notifications(h.mgr); n != 2 {
		t.Fatalf("%d notifications after the second note, want 2 (one per note)", n)
	}
}

func TestARoleTagReachesEveryoneWithTheRoleAtTheProperty(t *testing.T) {
	h := newHotel(t)
	other := h.w.Member(h.w.OrgA, &h.w.A1, "front_desk")
	elsewhere := h.w.Member(h.w.OrgA, &h.w.A2, "front_desk")
	h.note(h.mgr, `{"body":"Front desk, please look","mentions":[{"role":"front_desk"}]}`).Want(t, 201)
	for _, p := range []*testkit.Person{h.desk, other} {
		if n, _ := h.notifications(p); n != 1 {
			t.Fatalf("%d notifications", n)
		}
	}
	if n, _ := h.notifications(elsewhere); n != 0 {
		t.Fatal("a front desk at another property was notified")
	}
}

func TestOnlyColleaguesAtThePropertyCanBeTagged(t *testing.T) {
	h := newHotel(t)
	elsewhere := h.w.Member(h.w.OrgA, &h.w.A2, "manager")
	stranger := h.w.Member(h.w.OrgB, &h.w.B1, "manager")
	for _, id := range []string{elsewhere.ID.String(), stranger.ID.String(), uuid.NewString()} {
		h.note(h.desk, fmt.Sprintf(`{"body":"hi","mentions":[{"userId":%q}]}`, id)).Want(t, 400, "cannot_tag")
	}
	h.note(h.desk, `{"body":"hi","mentions":[{"role":"owner"}]}`).Want(t, 400)
	// An owner works at every property and can be tagged.
	h.note(h.desk, fmt.Sprintf(`{"body":"hi","mentions":[{"userId":%q}]}`, h.owner.ID)).Want(t, 201)
	// A deactivated colleague cannot.
	must(t, db.WithOrg(ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update membership set status = 'deactivated' where user_id = $1`, h.rev.ID)
		return err
	}))
	h.note(h.desk, fmt.Sprintf(`{"body":"hi","mentions":[{"userId":%q}]}`, h.rev.ID)).Want(t, 400, "cannot_tag")
}

func TestTheColleagueListFollowsTheTypedLetters(t *testing.T) {
	h := newHotel(t)
	var all []struct{ ID, Name, Role string }
	must(t, json.Unmarshal([]byte(h.desk.Do("GET", "/api/colleagues", &h.prop, "").Want(t, 200).Body), &all))
	if len(all) != 5 { // owner, manager, front desk, revenue, housekeeping
		t.Fatalf("%+v", all)
	}
	got := h.desk.Do("GET", "/api/colleagues?q=person", &h.prop, "").Want(t, 200)
	if !strings.Contains(got.Body, `"role":"manager"`) {
		t.Fatalf("%s", got.Body)
	}
	h.desk.Do("GET", "/api/colleagues?q=zzzz", &h.prop, "").Want(t, 200, "[]")
	h.keeper.Do("GET", "/api/colleagues", &h.prop, "").Want(t, 403)
}

func TestAHousekeeperGetsTheNoteAndTheRoomButNoGuestData(t *testing.T) {
	h := newHotel(t)
	room := h.roomID()
	h.desk.Do("POST", "/api/reservations/"+h.res+"/assign", &h.prop, fmt.Sprintf(`{"roomId":%q}`, room)).Want(t, 204)
	body := fmt.Sprintf(`{"body":"Extra towels and a cot please","mentions":[{"userId":%q}]}`, h.keeper.ID)
	h.note(h.desk, body).Want(t, 201)
	n, text := h.notifications(h.keeper)
	if n != 1 || !strings.Contains(text, "Extra towels") || !strings.Contains(text, "room 101") {
		t.Fatalf("%d: %s", n, text)
	}
	for _, secret := range []string{"Mai", "mai@", "+84", "10000", "Pham"} {
		if strings.Contains(text, secret) {
			t.Fatalf("the housekeeper's notification shows %q: %s", secret, text)
		}
	}
	got := h.keeper.Do("GET", "/api/rooms/"+room+"/notes", &h.prop, "").Want(t, 200, "Extra towels", `"roomNumber":"101"`)
	for _, secret := range []string{"Mai", "mai@", "+84", "10000", "Pham", "reservation"} {
		if strings.Contains(got.Body, secret) {
			t.Fatalf("the room's notes show %q: %s", secret, got.Body)
		}
	}
	// The housekeeper replies on the room, and the reply shows in the booking thread.
	var notes []struct{ ID string }
	must(t, json.Unmarshal([]byte(got.Body), &notes))
	h.keeper.Do("POST", "/api/rooms/"+room+"/notes", &h.prop, fmt.Sprintf(`{"body":"Done, cot is in","replyTo":%q}`, notes[0].ID)).Want(t, 201)
	found := false
	for _, it := range h.thread(h.desk).Items {
		if it.Body != nil && strings.Contains(*it.Body, "cot is in") {
			found = true
		}
	}
	if !found {
		t.Fatal("the reply is missing from the booking thread")
	}
	// A housekeeper cannot read notes on a room that never tagged housekeeping, nor reply to someone else's.
	h.keeper.Do("POST", "/api/rooms/"+room+"/notes", &h.prop, fmt.Sprintf(`{"body":"x","replyTo":%q}`, uuid.NewString())).Want(t, 404)
}

func TestTheAuthorIsWarnedWhenAHousekeepingNoteNamesTheGuest(t *testing.T) {
	h := newHotel(t)
	h.desk.Do("POST", "/api/reservations/"+h.res+"/assign", &h.prop, fmt.Sprintf(`{"roomId":%q}`, h.roomID())).Want(t, 204)
	named := fmt.Sprintf(`{"body":"Mai Pham wants a quiet room","mentions":[{"userId":%q}]}`, h.keeper.ID)
	h.note(h.desk, named).Want(t, 409, "contains_guest_name")
	acknowledged := fmt.Sprintf(`{"body":"Mai Pham wants a quiet room","acknowledged":true,"mentions":[{"userId":%q}]}`, h.keeper.ID)
	h.note(h.desk, acknowledged).Want(t, 201)
	// Not tagging housekeeping: no warning.
	h.note(h.desk, `{"body":"Mai Pham called"}`).Want(t, 201)
}

func TestANoteCanBeEditedByItsAuthorForFiveMinutes(t *testing.T) {
	h := newHotel(t)
	id := idOf(t, h.note(h.desk, `{"body":"first draft"}`).Want(t, 201))
	h.w.Advance(4 * time.Minute)
	h.desk.Do("PATCH", "/api/notes/"+id, &h.prop, `{"body":"better"}`).Want(t, 204)
	h.mgr.Do("PATCH", "/api/notes/"+id, &h.prop, `{"body":"hijacked"}`).Want(t, 403)
	h.w.Advance(2 * time.Minute)
	h.desk.Do("PATCH", "/api/notes/"+id, &h.prop, `{"body":"too late"}`).Want(t, 409, "edit_window_closed")
	if got := *h.thread(h.desk).Items[len(h.thread(h.desk).Items)-1].Body; got != "better" {
		t.Fatalf("%q", got)
	}
	// Pinning is not an edit, and the author or a manager may do it at any time.
	h.desk.Do("PATCH", "/api/notes/"+id, &h.prop, `{"pinned":true}`).Want(t, 204)
	if p := h.thread(h.desk).Pinned; len(p) != 1 || p[0].ID != id {
		t.Fatalf("%+v", p)
	}
	h.rev.Do("PATCH", "/api/notes/"+id, &h.prop, `{"pinned":false}`).Want(t, 403)
	h.mgr.Do("PATCH", "/api/notes/"+id, &h.prop, `{"pinned":false}`).Want(t, 204)
}

func TestNoteInputIsChecked(t *testing.T) {
	h := newHotel(t)
	h.note(h.desk, `{"body":""}`).Want(t, 400)
	h.note(h.desk, `{"body":"   "}`).Want(t, 400)
	h.note(h.desk, fmt.Sprintf(`{"body":%q}`, strings.Repeat("x", 2001))).Want(t, 400)
	h.note(h.desk, fmt.Sprintf(`{"body":%q}`, strings.Repeat("x", 2000))).Want(t, 201)
	h.note(h.desk, `{bad`).Want(t, 400)
	h.keeper.Do("POST", "/api/reservations/"+h.res+"/notes", &h.prop, `{"body":"x"}`).Want(t, 403)
	h.w.Anon("POST", "/api/reservations/"+h.res+"/notes", `{"body":"x"}`).Want(t, 401)
	other := h.w.Member(h.w.OrgB, &h.w.B1, "front_desk")
	other.Do("GET", "/api/reservations/"+h.res+"/thread", &h.w.B1, "").Want(t, 404)
	other.Do("POST", "/api/reservations/"+h.res+"/notes", &h.w.B1, `{"body":"x"}`).Want(t, 404)
}

func TestARemovedNoteIsHiddenFromStaffButKeptForOwners(t *testing.T) {
	h := newHotel(t)
	id := idOf(t, h.note(h.desk, `{"body":"Secret allergy detail"}`).Want(t, 201))
	h.rev.Do("DELETE", "/api/notes/"+id, &h.prop, "").Want(t, 403)
	h.desk.Do("DELETE", "/api/notes/"+id, &h.prop, "").Want(t, 204)
	h.mgr.Do("DELETE", "/api/notes/"+id, &h.prop, "").Want(t, 409, "already_removed")
	got := h.mgr.Do("GET", "/api/reservations/"+h.res+"/thread", &h.prop, "").Want(t, 200, `"removed":true`)
	if strings.Contains(got.Body, "Secret allergy") {
		t.Fatalf("staff can still read a removed note: %s", got.Body)
	}
	if !strings.Contains(got.Body, "Person") {
		t.Fatal("the thread does not say who removed it")
	}
	h.owner.Do("GET", "/api/activity?action=note.removed", nil, "").Want(t, 200, "Secret allergy detail")
	// A manager may remove someone else's note.
	other := idOf(t, h.note(h.desk, `{"body":"mine"}`).Want(t, 201))
	h.mgr.Do("DELETE", "/api/notes/"+other, &h.prop, "").Want(t, 204)
}

func TestTheThreadMixesEventsAndNotesInTimeOrder(t *testing.T) {
	h := newHotel(t)
	h.w.Advance(time.Minute)
	h.note(h.desk, `{"body":"first note"}`).Want(t, 201)
	h.w.Advance(time.Minute)
	h.note(h.mgr, `{"body":"second note"}`).Want(t, 201)
	got := h.thread(h.desk)
	if len(got.Items) < 3 || got.Items[0].Type != "event" || got.Items[0].Action != "reservation.created" {
		t.Fatalf("%+v", got.Items)
	}
	last := got.Items[len(got.Items)-1]
	if last.Type != "note" || *last.Body != "second note" {
		t.Fatalf("%+v", last)
	}
	for i := 1; i < len(got.Items); i++ {
		if got.Items[i].At.Before(got.Items[i-1].At) {
			t.Fatal("the thread is not in time order")
		}
	}
}

func TestUnreadNotesShowAsADotUntilTheThreadIsOpened(t *testing.T) {
	h := newHotel(t)
	h.note(h.desk, `{"body":"for the team"}`).Want(t, 201)
	unread := func(p *testkit.Person) int {
		var out struct{ ReservationIDs []string }
		must(t, json.Unmarshal([]byte(p.Do("GET", "/api/notes/unread", &h.prop, "").Want(t, 200).Body), &out))
		return len(out.ReservationIDs)
	}
	if unread(h.mgr) != 1 || unread(h.desk) != 0 {
		t.Fatalf("manager %d, author %d", unread(h.mgr), unread(h.desk))
	}
	h.thread(h.mgr)
	if unread(h.mgr) != 0 {
		t.Fatal("the dot stayed after reading")
	}
}

func TestATaggedPersonCanMarkItDone(t *testing.T) {
	h := newHotel(t)
	id := idOf(t, h.note(h.desk, fmt.Sprintf(`{"body":"Please check","mentions":[{"userId":%q}]}`, h.mgr.ID)).Want(t, 201))
	h.rev.Do("POST", "/api/notes/"+id+"/done", &h.prop, "").Want(t, 404)
	h.mgr.Do("POST", "/api/notes/"+id+"/done", &h.prop, "").Want(t, 204)
	h.mgr.Do("POST", "/api/notes/"+id+"/done", &h.prop, "").Want(t, 204) // marking twice changes nothing
}

func TestAnUnansweredTagRaisesAnAlertAfterFourHours(t *testing.T) {
	h := newHotel(t)
	id := idOf(t, h.note(h.desk, fmt.Sprintf(`{"body":"Please check the invoice","mentions":[{"userId":%q}]}`, h.mgr.ID)).Want(t, 201))
	h.w.Advance(3*time.Hour + 59*time.Minute)
	must(t, alerts.Run(ctx, h.w.Pool, h.w.Now))
	open := func() int {
		var out []struct{ CheckKind string }
		must(t, json.Unmarshal([]byte(h.desk.Do("GET", "/api/alerts", &h.prop, "").Want(t, 200).Body), &out))
		n := 0
		for _, a := range out {
			if a.CheckKind == "tag_not_answered" {
				n++
			}
		}
		return n
	}
	if open() != 0 {
		t.Fatal("raised too early")
	}
	h.w.Advance(2 * time.Minute)
	must(t, alerts.Run(ctx, h.w.Pool, h.w.Now))
	if open() != 1 {
		t.Fatal("no alert for the author after four hours")
	}
	h.mgr.Do("POST", "/api/notes/"+id+"/done", &h.prop, "").Want(t, 204)
	must(t, alerts.Run(ctx, h.w.Pool, h.w.Now))
	if open() != 0 {
		t.Fatal("the alert stayed after the tag was answered")
	}
}

func openStream(t *testing.T, h *hotel, p *testkit.Person) (<-chan string, func()) {
	t.Helper()
	c, cancel := context.WithCancel(ctx)
	req, _ := http.NewRequestWithContext(c, "GET", h.w.Srv.URL+"/api/reservations/"+h.res+"/thread/stream", nil)
	req.AddCookie(&http.Cookie{Name: "sid", Value: p.Session})
	req.Header.Set("X-Property-Id", h.prop.String())
	res, err := http.DefaultClient.Do(req)
	must(t, err)
	if res.StatusCode != 200 {
		t.Fatalf("stream status %d", res.StatusCode)
	}
	out := make(chan string, 8)
	go func() {
		defer res.Body.Close()
		defer close(out)
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if name, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
				out <- name
			}
		}
	}()
	return out, cancel
}

func TestANewNoteReachesASecondOpenBrowserWithoutAReload(t *testing.T) {
	h := newHotel(t)
	events, stop := openStream(t, h, h.mgr)
	defer stop()
	h.note(h.desk, `{"body":"live"}`).Want(t, 201)
	select {
	case name := <-events:
		if name != "note" {
			t.Fatalf("event %q", name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event within 2 seconds")
	}
}

func TestANoteOnAnotherBookingDoesNotWakeTheStream(t *testing.T) {
	h := newHotel(t)
	events, stop := openStream(t, h, h.mgr)
	defer stop()
	second := idOf(t, h.desk.Do("POST", "/api/reservations", &h.prop, fmt.Sprintf(
		`{"guest":{"name":"Zed"},"rooms":[{"roomTypeId":%q,"ratePlanId":%q,"from":"2026-12-10","to":"2026-12-11","adults":1,"children":0}]}`, h.rt, h.rp)).Want(t, 201))
	h.desk.Do("POST", "/api/reservations/"+second+"/notes", &h.prop, `{"body":"elsewhere"}`).Want(t, 201)
	select {
	case name := <-events:
		t.Fatalf("woken by another booking: %q", name)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestNotesAreInternalAndAppearNowhereGuestFacing(t *testing.T) {
	h := newHotel(t)
	h.note(h.desk, `{"body":"SECRET-NOTE-TEXT"}`).Want(t, 201)
	for _, path := range []string{"/api/reservations/" + h.res, "/api/reservations", "/api/guests", "/api/reservations/" + h.res + "/folio"} {
		if got := h.desk.Do("GET", path, &h.prop, "").Body; strings.Contains(got, "SECRET-NOTE-TEXT") {
			t.Fatalf("%s carries a note", path)
		}
	}
	for _, m := range h.w.Mail.Sent() {
		if strings.Contains(m.Text, "SECRET-NOTE-TEXT") {
			t.Fatal("a note reached an email")
		}
	}
}

func TestEditingAHousekeepingNoteRunsTheGuestNameCheckAgain(t *testing.T) {
	h := newHotel(t)
	h.desk.Do("POST", "/api/reservations/"+h.res+"/assign", &h.prop, fmt.Sprintf(`{"roomId":%q}`, h.roomID())).Want(t, 204)
	id := idOf(t, h.note(h.desk, fmt.Sprintf(`{"body":"Extra towels","mentions":[{"userId":%q}]}`, h.keeper.ID)).Want(t, 201))
	h.desk.Do("PATCH", "/api/notes/"+id, &h.prop, `{"body":"Extra towels for Mai Pham"}`).Want(t, 409, "contains_guest_name")
	h.desk.Do("PATCH", "/api/notes/"+id, &h.prop, `{"body":"Extra towels for Mai Pham","acknowledged":true}`).Want(t, 204)
}
