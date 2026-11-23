# Guest Booking (Milestone 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans. Steps use checkbox syntax.

**Goal:** A guest finds a hotel by its address, picks rooms, pays by the method the owner turned on (Stripe card, bank transfer or pay at the hotel), and gets a confirmed booking.

**Architecture:** Public routes under `/api/public/{slug}/...` resolve the property through a security-definer function (no staff session), then run in that organization's RLS scope. A `payments.Provider` interface has a fake (tests) and a Stripe adapter (hosted Checkout, signed webhook). A pending booking holds inventory until paid or until a job releases it.

**Tech Stack:** Go net/http, pgx, PostgreSQL RLS, Stripe Checkout over plain HTTPS (no card data touches our servers), SvelteKit guest routes, Paraglide-style message files.

**Spec:** `docs/specs/05-guest-booking.md`, `docs/specs/06-payments.md`, `docs/specs/12-guest-site-chat-and-feedback.md`, `docs/specs/08-branding-and-interface.md`.

## Global Constraints

- Money is integer minor units. Card numbers are never stored, logged or audited.
- Every provider call carries an idempotency key. A webhook event is recorded once by its event id.
- The total shown at quote time equals the booking total and the folio total.
- A card hold lasts 15 minutes; a bank transfer hold lasts the configured hours. A job cancels unpaid pending bookings and frees inventory.
- Interface languages: en, zh-Hans, hi, es, fr, ar, bn, pt, ru, ur. A language is offered only when marked reviewed (only en at first). ar and ur set `dir="rtl"`; CSS uses logical properties.
- Design tokens only, no Tailwind; no emoji; Font Awesome icons.

## Review Focus

- Webhook arrives twice, or before the guest returns, or after the hold expired (booking cancelled): one payment recorded; late payment flagged for staff.
- Two guests book the last room: one 409 `sold_out`, inventory never negative.
- Owner turns a payment type off while a guest has the page open: creating the booking with it is refused.
- Refund through the provider fails: the refund stays `pending` and staff see it.
- Forged webhook signature or replayed old timestamp is refused.

## Tasks

### Task 1: Payment settings and hold columns (migration 0020)
- Create `api/migrations/0020_guest_booking.sql`: `payment_type(property_id, kind in (hotel,bank_transfer,card), enabled, where_offered, deposit_kind, deposit_value, bank_details, hold_hours, provider_account)`; `reservation.payment_status`, `reservation.hold_until`, `reservation.payment_type`; `payment_event(provider, event_id unique)`; unique `(organization_id, code)` on reservation; `property_by_slug(text)` security-definer returning `(id, organization_id)`.
- Test (`api/internal/guestbooking/settings_test.go`): owner sets types; non-owner refused; at least one type must stay on.

### Task 2: Public info, availability and quote
- `GET /api/public/{slug}`: name, currency, default language, enabled payment kinds, room types with photos placeholder fields.
- `POST /api/public/{slug}/quote`: dates, rooms → per-night lines, taxes, total, sold-out flags. Uses `rooms.Price` and `folio` tax rules.
- Tests: unknown slug 404; sold-out type flagged; quote total equals created booking total.

### Task 3: Provider interface, fake and Stripe adapter
- `api/internal/payments/provider.go`: `Provider{CreateCheckout, ParseWebhook, Refund}`; `Fake`; `Stripe` (form-encoded POST `/v1/checkout/sessions`, `Idempotency-Key` header, `Stripe-Signature` HMAC-SHA256 check with 5 minute tolerance, refunds via `/v1/refunds`).
- Tests: signature accepted, forged and stale refused; idempotency key sent; no card field in any request.

### Task 4: Create booking, webhook, hold expiry
- `POST /api/public/{slug}/bookings`: creates a `pending` reservation with `hold_until`, returns checkout URL (card) or bank details (transfer) or confirms at once (hotel).
- `POST /api/public/webhooks/{provider}`: verifies, records `payment_event`, marks booking `confirmed` + payment row; late payment on a cancelled booking raises a staff alert.
- Job `ExpireHolds(now)`: cancels pending bookings past `hold_until`.
- Tests: webhook twice → one payment; webhook before return; expired hold released; last room race; late payment flagged; disabled type refused.

### Task 5: Manage a booking
- One-time link by email; view, resend confirmation, cancel with provider refund per rate plan policy; refund failure leaves `pending`.

### Task 6: Guest site
- SvelteKit routes `/book/[slug]` with 4 steps, language menu, RTL, summary bar; interface message files for the 10 languages with a `reviewed` flag.
- e2e: full booking with the fake provider; keyboard-only run; phone width.

### Task 7: Owner screens
- Settings → Payments (types, rules) and Branding (logo, colour) in the staff app.

### Task 8: Review and fix pass
- `ai-design-check`, full suites, independent review, one fix pass.
