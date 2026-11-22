# Hotel PMS design

Date: 2026-11-23. Status: draft, waiting for the owner's approval.

Lines marked **(Claude's call)** are proposals that the owner has not confirmed. All other decisions come from the owner's own words.

## 1. Intent

Build hotel management software that looks good, is easy to use, and keeps rooms and prices correct on the big online travel agencies (OTAs) such as Booking.com, Agoda, Expedia and Airbnb.

The owner has not yet said why existing products fall short. That question is open (section 10).

## 2. Who uses it

- Small hotels with one property, and hotel chains with many. Both are first customers.
- Staff: owner, manager, front desk, revenue manager, housekeeping.
- Guests, who book a room on the hotel's own booking page.

## 3. Goals

1. Staff run the daily work in one app: bookings, rates, check-in, payments, housekeeping, reports.
2. Rooms and rates stay in sync with OTAs, fast and without double bookings.
3. Guests book directly, in 10 languages, and choose add-on services, tours and products.
4. The owner sets up payment types, users, roles, colours and logo without a developer.
5. Each staff member has their own login and reaches only what their role allows. The owner can switch between role views.
6. Staff logins use two steps: sign-in, then a one-time code sent by email. Google sign-in is offered.
7. The app works well on desktop, iPad mini (744 px wide) and phone.
8. The interface uses shared components, deliberate motion, and passes the `ai-design-check` checker.
9. Free and open-source libraries are used wherever a good one exists.
10. The system finds problems early, such as overbooking risk, rooms not ready and unpaid arrivals, and offers the fix as one button.
11. Notifications reach the right role once, at the right urgency, and escalate when ignored.
12. Every staff action is recorded, so any issue can be traced back to who did what, when and from where.
13. Staff write notes on a booking and tag each other there.
14. Every design area passes the owner's four questions: what is the value, is it user friendly, is it taking too much space, is it intuitive yet (spec 08).
15. Rich tooltips and other smart interactions give detail on demand, so screens stay quiet (spec 08).
16. Each hotel has a guest site with photo galleries, amenities, extras and reviews, and guests can chat with the hotel and leave feedback (spec 12).

Status: the owner approved goals 1 to 15 and specs 01 to 11 on 2026-11-23. Goal 16 and spec 12 were added after that approval. Later the same day the owner reviewed the guest site and guest booking mockups and asked for 7 changes by comment. Specs 05, 08 and 12 were updated to match on 2026-11-23.

## Plans

| Milestone | Plan |
| --- | --- |
| 1. Foundation | [2026-11-23-foundation.md](../plans/2026-11-23-foundation.md), written |
| 2. Core PMS | Not written. Write it when milestone 1 is merged. |
| 3. Guest booking | Not written |
| 4. Channel sync | Not written. Needs the channel manager's API confirmed first. |
| 5. Extras | Not written |
| 6. Reports and chains | Not written |

Each plan is written just before its milestone starts, because it names the files, functions and tables that the earlier milestones actually produced.

## 4. Non-goals for the first release

- Direct, certified connections to each OTA. The first release goes through a channel manager (section 6).
- Custom roles. The first release has 5 fixed roles.
- Native iOS or Android apps. The web app installs to the home screen.
- Restaurant or spa point of sale, payroll, and accounting ledgers. Sales of extras post to the guest folio only.
- Dynamic pricing by algorithm. The system suggests rate changes and staff apply them (spec 09).
- Forecasts based on machine learning. Checks are rules and simple statistics first.
- Notifications through chat apps such as Telegram or Zalo.
- Dark theme.
- Passkeys and authenticator-app codes. They are the planned upgrade to email codes.

## 5. Anti-goals

The product fails, even if it works, when any of these happen:

- A room is sold twice.
- A staff member sees data from another hotel organization.
- Card numbers pass through or rest on our servers.
- A screen looks machine-generated: default fonts, rainbow colours, decorative gradients, copied markup.
- A guest in a right-to-left language (Arabic, Urdu) gets a broken layout.

## 6. Approaches

### How to reach the OTAs

| Approach | What it means | Trade-off |
| --- | --- | --- |
| A. Direct OTA connections | Become a certified partner of each OTA and call their APIs | No middleman fee. Each OTA needs approval and a certification test, which can take months and may be refused. |
| B. Channel manager API first | Connect once to a certified channel manager, which talks to the OTAs | Live in weeks. A fee per property per month, and sync speed depends on their service. |
| C. Fork an open-source PMS | Start from an existing project such as QloApps | Fast start. Its data model, look and PHP stack would fight goals 2, 7 and 8. |

**Picked: B (Claude's call).** The sync engine is written against an internal interface, so a direct OTA adapter can replace the channel manager for one channel at a time.

Claude did not check the current partner rules of any OTA or channel manager. This must be verified before milestone 4 starts. Channex is the first candidate because it sells a white-label API.

### Technology

| Approach | Trade-off |
| --- | --- |
| A. Go API, PostgreSQL, SvelteKit web app | Small and fast, one binary, strong concurrency for sync. New stack for the owner. |
| B. Laravel (PHP), PostgreSQL, Livewire or Inertia | The owner already runs PHP apps, so faster to build. Heavier workers and slower live updates. |

**Picked: A (Claude's call).** Section 7 gives the parts.

## 7. Architecture

One PostgreSQL database is the single source of truth. Every booking changes the room count and queues the channel update in the same database transaction, so an update can be late but cannot be lost.

| Part | Choice | Licence |
| --- | --- | --- |
| Database | PostgreSQL 16 or later | PostgreSQL licence |
| API and workers | Go, standard `net/http`, `pgx` driver, `sqlc` for typed queries | BSD, MIT |
| Job queue | River, which stores jobs in PostgreSQL | MPL 2.0 |
| Web app | SvelteKit, TypeScript, plain CSS driven by design tokens (no CSS framework) | MIT |
| Accessible UI primitives | Bits UI | MIT |
| Icons | Lucide | ISC |
| Font | IBM Plex Sans, plus Noto Sans for scripts Plex lacks | OFL |
| Charts | LayerChart | MIT |
| Translations | Paraglide JS | Apache 2.0 |
| Live updates | Server-sent events | Built into browsers |
| File storage | Any S3-compatible store, for example MinIO | AGPL (MinIO) |
| Email | Any SMTP service | Paid service |
| Card payments | Stripe hosted payment page | Paid service |

Licences are listed from memory and must be checked before use.

There is no Redis, no Kafka and no separate search engine in the first release.

### Tenancy

Every row belongs to an organization, and most rows also belong to a property. A single hotel is an organization with one property. PostgreSQL row-level security enforces the organization boundary as a second guard behind the API checks.

### Surfaces

- Staff app at `app.<domain>`: one SvelteKit app, role-aware navigation.
- Guest booking page at `book.<domain>/<hotel-slug>`, and later the hotel's own domain.
- Public API for webhooks from the channel manager and the payment provider.

## 8. Spec documents

| Spec | Covers |
| --- | --- |
| [01 Roles and access](../../specs/01-roles-and-access.md) | Roles, permissions, owner role switching, user management |
| [02 Sign-in and security](../../specs/02-sign-in-and-security.md) | Passwords, Google sign-in, email codes, sessions, audit log |
| [03 Data model](../../specs/03-data-model.md) | Tables, constraints, inventory rules |
| [04 Channel sync](../../specs/04-channel-sync.md) | Outbox, retries, inbound bookings, nightly compare |
| [05 Guest booking](../../specs/05-guest-booking.md) | Booking flow, 10 languages, right-to-left layout |
| [06 Payments](../../specs/06-payments.md) | Payment types the owner configures, deposits, refunds |
| [07 Extras](../../specs/07-extras.md) | Add-on services, tours, products |
| [08 Branding and interface](../../specs/08-branding-and-interface.md) | Colour scheme, logo, design tokens, components, motion, breakpoints |
| [09 Alerts and suggested actions](../../specs/09-alerts-and-suggested-actions.md) | Checks that find problems early, and one-button fixes |
| [10 Notifications and activity trail](../../specs/10-notifications-and-activity-trail.md) | Who is told what and how, and tracing any issue back to its cause |
| [11 Team notes on bookings](../../specs/11-team-notes.md) | Staff chat on each booking, tagging colleagues |
| [12 Guest site, guest chat and feedback](../../specs/12-guest-site-chat-and-feedback.md) | The hotel's landing page, chat between guest and hotel, reviews |

## 9. Milestones

These are separate projects. Each one gets its own implementation plan and ships on its own.

1. **Foundation.** Organizations, properties, users, roles, sign-in with email codes and Google, audit log, app shell and component library.
2. **Core PMS.** Room types, rooms, rate plans, calendar, reservations, check-in and check-out, folios, housekeeping.
3. **Guest booking.** Guest site, booking page, 10 languages, payments, branding, guest chat and feedback.
4. **Channel sync.** Channel manager adapter, mapping screen, sync log, nightly compare.
5. **Extras.** Add-on services, tours, products, on the booking page and at the front desk.
6. **Reports and chains.** Owner overview, exports, group view across properties, central rate plans.

The activity trail and the notification system (spec 10) are built in milestone 1, because every later feature writes to them. The checks in spec 09 are added in the milestone that owns their data: booking and money checks in milestone 2, channel and rate checks in milestone 4, extras checks in milestone 5.

"Group view" is the owner overview with the numbers of all properties summed, and a table that compares the properties. "Central rate plans" are rate plan templates that a chain sets once and each property links to (spec 03). The tables for both exist from milestone 2, so milestone 6 adds screens and no data migration.

## 9a. Calls made after the builder review

A fresh reviewer read these documents as a builder. These choices answer its questions, and each is marked "Claude's call" in the spec named.

- One channel connection per property, with OTA mapping done in the channel manager (spec 04).
- The nightly compare checks the channel manager's copy, with that limit stated (spec 04).
- Room price per night, plus an extra-guest price above a base number of guests (spec 03).
- Guests can view, resend and cancel a booking themselves. Date changes go through the hotel (spec 05).
- Guests can book several rooms in one booking (spec 05).
- A user is one person across organizations (spec 03).
- A channel booking that arrives for a full night is stored and flagged, using a separate counter (specs 03 and 04).

## 10. Open questions for the owner

1. What do the PMS products you have seen get wrong? The answer sets what milestone 2 must do best.
2. Which country and currency come first? This decides tax rules, the default language and which local payment gateways to add after Stripe.
3. The 10 languages are chosen by total speakers (spec 05). A list chosen by where hotel guests come from would differ, for example Japanese, Korean and German in place of Hindi, Bengali and Urdu. Which list do you want?
4. Is this a product you sell to other hotels, or software for hotels you own? Selling it adds billing, sign-up and support tools that are not in these specs. It also decides how a hotel connects its card payment account (spec 06).
5. Should a guest who chooses "Pay at the hotel" give a card as a guarantee, so that a no-show can be charged? The specs say no for the first release, because it needs saved cards and a second payment flow.

## 11. Left to the builder

- File and package layout inside the Go and SvelteKit projects.
- Exact API route names and payload shapes, as long as they follow the specs' rules.
- Test tooling. Each spec lists the behaviours that need tests.
- Hosting layout. One server with PostgreSQL, the Go binary and the built web app is enough to start.
