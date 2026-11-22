# Spec 03: Data model

Part of the [Hotel PMS design](../superpowers/specs/2026-11-23-hotel-pms-design.md).

## Rules for every table

1. Every table has `organization_id`. Property-level tables also have `property_id`. The exceptions are `app_user`, `session`, `otp_challenge` and `trusted_device`, which describe a person and belong to no organization. One person can hold memberships in several organizations and picks one after sign-in.
2. Row-level security limits each request to its own organization.
3. Money is stored as an integer in the currency's smallest unit, with a 3-letter currency code beside it. Floating-point numbers are never used for money.
4. Times are stored in UTC. Each property has an IANA time zone. A "hotel day" is a date in the property's time zone.
5. Primary keys are UUID version 7.
6. Foreign keys are indexed and declared.
7. Rows that carry legal or money history are never deleted. They get a status instead.

## Tables

### Tenancy and people

| Table | Key columns | Notes |
| --- | --- | --- |
| `organization` | name | The paying customer |
| `property` | name, slug, time_zone, currency, default_locale, address | `slug` is unique and is used in the booking page URL |
| `app_user` | email (unique, lowercased), name, password_hash, google_subject, status | One row per person |
| `membership` | user_id, property_id (null for owners), role | Unique on user and property |
| `session` | user_id, expires_at, last_seen_at, device_label | |
| `otp_challenge` | user_id, code_hash, expires_at, attempts, used_at | |
| `trusted_device` | user_id, token_hash, expires_at | |
| `invite` | email, property_id, role, token_hash, expires_at, accepted_at | |
| `audit_log` | See spec 02 | Append-only |

### Rooms and prices

| Table | Key columns | Notes |
| --- | --- | --- |
| `room_type` | name, base_guests, max_guests, extra_adult_price, extra_child_price | For example Standard, Deluxe, Suite |
| `room` | room_type_id, number, floor, status | Status: clean, dirty, inspect, out_of_order |
| `rate_plan_template` | organization_id, name, cancellation fields, meal_plan | Optional. A chain's central rate plan (see below) |
| `rate_plan` | room_type_id, template_id (nullable), name, free_cancel_until_days, free_cancel_until_time, late_cancel_penalty, meal_plan | For example "Flexible", "Non-refundable" |
| `rate` | rate_plan_id, date, price, min_nights, closed, closed_to_arrival, closed_to_departure | Unique on rate plan and date |
| `inventory` | room_type_id, date, total, sold, blocked, oversold | Unique on room type and date. `CHECK (sold + blocked <= total)` |

**Room price (Claude's call).** `rate.price` is the price of the room per night for up to `base_guests`. Each extra adult or child adds the room type's extra price per night, up to `max_guests`. Prices do not otherwise change with the number of guests.

**Cancellation policy.** A rate plan allows free cancellation until a number of days before arrival at a set time. After that, `late_cancel_penalty` applies: `none`, `first_night`, a percent, or `full`. A non-refundable plan has `free_cancel_until_days` empty and penalty `full`.

**Central rate plans.** A chain can create a rate plan template once. Each property links its own rate plan to the template, and the name, cancellation rules and meal plan follow the template. Prices stay per property. A single hotel never sees templates.

### Bookings

| Table | Key columns | Notes |
| --- | --- | --- |
| `guest` | name, email, phone, country, locale, notes | Belongs to the organization, so chains share one guest profile |
| `reservation` | code (unique), guest_id, status, source_channel, channel_booking_ref, total, currency | Status: pending, confirmed, checked_in, checked_out, cancelled, no_show |
| `reservation_room` | reservation_id, room_type_id, room_id (nullable), rate_plan_id, stay (daterange), adults, children | One row per room booked |
| `reservation_night` | reservation_room_id, date, price | The price of each night at booking time |

`channel_booking_ref` is unique per channel connection. This stops the same OTA booking from being stored twice.

`reservation_room` has an exclusion constraint so that one room cannot hold two stays that overlap:

```sql
EXCLUDE USING gist (room_id WITH =, stay WITH &&)
  WHERE (room_id IS NOT NULL AND status_active)
```

`stay` is a half-open range: it includes the arrival date and excludes the departure date. A guest leaving on 25 Nov and a guest arriving on 25 Nov do not overlap.

### Money

| Table | Key columns | Notes |
| --- | --- | --- |
| `folio` | reservation_id (nullable), status | One per reservation in the first release. A walk-in sale of extras is a folio with no reservation. |
| `folio_line` | folio_id, kind, description, quantity, unit_price, tax_amount, catalog_item_id, posted_by | Kind: room, extra, tax, adjustment |
| `payment` | folio_id, method, provider, provider_ref, amount, status, taken_by | See spec 06 |
| `refund` | payment_id, amount, reason, status, requested_by, approved_by | |
| `payment_method_config` | property_id, method, enabled, settings (encrypted where secret) | See spec 06 |
| `tax_rule` | property_id, name, percent, inclusive, applies_to | |

### Channels

| Table | Key columns | Notes |
| --- | --- | --- |
| `channel_connection` | property_id, provider, external_property_id, credentials (encrypted), status | |
| `channel_mapping` | connection_id, room_type_id, rate_plan_id, external_room_id, external_rate_id | |
| `outbox` | property_id, kind, payload, dedupe_key, created_at | Written in the same transaction as the change |
| `sync_attempt` | outbox_id, connection_id, status, tries, last_error, sent_at, confirmed_at | |
| `inbound_event` | connection_id, external_event_id (unique), payload, processed_at | |

### Extras, branding, operations

| Table | Key columns | Notes |
| --- | --- | --- |
| `catalog_item` | property_id, kind, price, pricing_unit, tax_rule_id, active, stock | See spec 07 |
| `catalog_item_translation` | catalog_item_id, locale, name, description | |
| `tour_departure` | catalog_item_id, starts_at, capacity, booked | `CHECK (booked <= capacity)` |
| `content_translation` | property_id, entity, entity_id, field, locale, text | Room type names, policies and other hotel text |
| `branding` | property_id, logo_file_id, brand_color, derived_tokens | See spec 08 |
| `file` | storage_key, mime_type, bytes, uploaded_by | |
| `housekeeping_task` | room_id, date, kind, status, assigned_to | |
| `maintenance_ticket` | room_id, description, status, blocks_room_from, blocks_room_to | |

### Alerts, notifications and activity

| Table | Key columns | Notes |
| --- | --- | --- |
| `alert`, `check_setting` | See spec 09 | |
| `notification`, `notification_delivery`, `notification_preference`, `push_subscription` | See spec 10 | |
| `audit_log` | Adds `trace_id`, `alert_id`, `reason`, `prev_hash`, `row_hash` | The activity trail in spec 10 |
| `note`, `note_mention`, `note_read`, `note_attachment` | See spec 11 | Team notes on bookings |
| `conversation`, `message`, `quick_question`, `review`, `stay_feedback`, `photo`, `photo_translation`, `site_section` | See spec 12 | Guest site, chat and feedback |

Every request gets a `trace_id`. It is written on the audit row, on any outbox row and on any sync attempt the request causes, which is what links an effect back to its cause.

## Inventory rules

Rooms are sold by room type, and a room number is assigned later. Two guards work together.

1. **By room type.** A booking adds to `inventory.sold` for each night, in one transaction, with the rows locked in date order. The `CHECK` constraint rejects the transaction when a night is full.
2. **By room.** Assigning a room number is protected by the exclusion constraint above.

Availability sent to guests and channels is `total - sold - blocked` for each night.

A cancelled booking lowers `sold` and writes an outbox row in the same transaction.

An out-of-order room raises `blocked` for its dates.

A booking from a channel that arrives when the night is already full is the one exception to guard 1. It is stored, flagged as overbooked, and counted in `inventory.oversold`, which sits outside the `CHECK`. Bookings from staff and from the booking page can never use `oversold`. Availability never goes below 0, and staff clear the flag by moving or cancelling a booking (spec 04).

## Tests this spec needs

- Two bookings for the last room at the same moment: exactly one succeeds.
- A checkout and a check-in on the same date in the same room are both accepted.
- The same OTA booking delivered twice creates one reservation.
- A booking and its outbox row are both saved or both rolled back.
- A query with another organization's ID returns no rows.
