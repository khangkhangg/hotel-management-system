package guestbooking_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAGuestCanReviewOnlyAfterTheStayAndOnlyOnce(t *testing.T) {
	h := newHotel(t, 2)
	m := h.book(t, "hotel", "2026-12-10", "2026-12-12")
	tok := h.link(t, m.Code, "mai@hotel.com")
	h.w.Anon("POST", "/api/manage/"+tok+"/review", `{"rating":5,"comment":"Lovely"}`).Want(t, 409, "not_stayed")
	h.w.Anon("GET", "/api/manage/"+tok+"/review", "").Want(t, 200, `"canReview":false`)
	h.sql(t, `update reservation set status = 'checked_out' where code = $1`, m.Code)
	tok = h.link(t, m.Code, "mai@hotel.com")
	h.w.Anon("GET", "/api/manage/"+tok+"/review", "").Want(t, 200, `"canReview":true`, `"reviewed":false`)
	for _, bad := range []string{`{"rating":0}`, `{"rating":6}`, `{"rating":5,"comment":"` + strings.Repeat("a", 2001) + `"}`, `{"rating":"five"}`, `{}`} {
		h.w.Anon("POST", "/api/manage/"+tok+"/review", bad).Want(t, 400)
	}
	h.w.Anon("POST", "/api/manage/"+tok+"/review", `{"rating":5,"comment":"Lovely quiet rooms"}`).Want(t, 201)
	h.w.Anon("POST", "/api/manage/"+tok+"/review", `{"rating":1,"comment":"again"}`).Want(t, 409, "already_reviewed")
	h.w.Anon("GET", "/api/manage/"+tok+"/review", "").Want(t, 200, `"canReview":false`, `"reviewed":true`)
}

func TestPublishedReviewsAppearOnThePublicPageWithAnAverageAndStaffCanHideOrAnswerThem(t *testing.T) {
	h := newHotel(t, 3)
	desk := h.w.Member(h.w.OrgA, h.prop, "front_desk")
	review := func(from, to string, rating int, comment string) {
		m := h.book(t, "hotel", from, to)
		h.sql(t, `update reservation set status = 'checked_out' where code = $1`, m.Code)
		tok := h.link(t, m.Code, "mai@hotel.com")
		h.w.Anon("POST", "/api/manage/"+tok+"/review", fmt.Sprintf(`{"rating":%d,"comment":%q}`, rating, comment)).Want(t, 201)
	}
	h.w.Anon("GET", "/api/public/a1", "").Want(t, 200, `"reviews":{"count":0`)
	review("2026-12-10", "2026-12-11", 5, "Lovely quiet rooms")
	review("2026-12-12", "2026-12-13", 4, "Good breakfast")
	page := h.w.Anon("GET", "/api/public/a1", "").Want(t, 200, `"count":2`, `"average":4.5`, "Lovely quiet rooms", `"name":"Mai P."`).Body
	if strings.Contains(page, "mai@hotel.com") {
		t.Fatal("the reviewer's email is on the public page")
	}
	desk.Do("GET", "/api/reviews", h.prop, "").Want(t, 403)
	var first string
	first = h.row(t, `select id::text from review order by created_at limit 1`)
	desk.Do("POST", "/api/reviews/"+first+"/hide", h.prop, "").Want(t, 403)
	h.mgr.Do("GET", "/api/reviews", h.prop, "").Want(t, 200, "Lovely quiet rooms", "Good breakfast")
	h.mgr.Do("POST", "/api/reviews/"+first+"/hide", h.prop, "").Want(t, 204)
	h.w.Anon("GET", "/api/public/a1", "").Want(t, 200, `"count":1`, `"average":4`, "Good breakfast")
	h.mgr.Do("POST", "/api/reviews/"+first+"/show", h.prop, "").Want(t, 204)
	h.mgr.Do("POST", "/api/reviews/"+first+"/reply", h.prop, `{"text":"Thank you, come again"}`).Want(t, 204)
	h.mgr.Do("POST", "/api/reviews/"+first+"/reply", h.prop, `{"text":"`+strings.Repeat("a", 1001)+`"}`).Want(t, 400)
	h.w.Anon("GET", "/api/public/a1", "").Want(t, 200, `"count":2`, "Thank you, come again")
	h.mgr.Do("POST", "/api/reviews/"+uuid.NewString()+"/hide", h.prop, "").Want(t, 404)
}

func (h *hotel) sql(t *testing.T, q string, args ...any) {
	t.Helper()
	h.row(t, q+" returning 'x'", args...)
}
