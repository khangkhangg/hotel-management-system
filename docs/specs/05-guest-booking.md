# Spec 05: Guest booking

Part of the [Hotel PMS design](../superpowers/specs/2026-11-23-hotel-pms-design.md).

## Flow

1. **Choose a room.** The guest sets dates and guests, then sees each room type with its price, what is left, and a "Select room" button.
2. **Add extras.** Add-on services, tours and products the hotel offers for those dates (spec 07). The step is skipped when the hotel has none.
3. **Your details.** Name, email, phone, arrival time, and the payment choice (spec 06).
4. **Confirmation.** Booking code, summary, and an email in the guest's language.

### What the guest sees on each step

- **Progress.** A bar with one segment per step sits under the header. On desktop each segment has its name: "1. Room", "2. Extras", "3. Your details", "4. Confirmation". On a phone it is one line: "Step 1 of 4 Room" on the left and "Next: Extras" on the right.
- **Room cards.** Each card has the room's photo gallery, size, beds, an amenities line, how many rooms are left, the average price per night, the price for the stay before tax, and "Select room".
- **Extras.** Each row shows a photo, the kind (service, tour or product), the name, one line of detail, the price for this stay, and an "Add" button that becomes "Added".
- **Stay summary on desktop.** A "Your stay" card beside the steps with dates, room, a line per night, extras, taxes, the total, the cancellation deadline and the main button.
- **Stay summary on a phone.** A bar fixed to the bottom. Its left side says what the price covers, for example "Deluxe room · 2 nights · 1 extra", and the total "with taxes". Tapping it opens the full price breakdown above the bar. Its right side is the main button, which names the next step: "Next: extras", "Next: your details", "Confirm booking".
- **Header.** Logo and hotel name link back to the guest site. "Manage a booking" is a quiet button.

Rules:

- A guest can book several rooms in one booking **(Claude's call)**. Each room type has a quantity, all rooms share the same dates, and the guest states the adults and children per room. The hold, the price lines and the total cover every room.
- The price follows spec 03: the room price per night, plus the extra-guest price above the room type's base number of guests.
- The stay summary is always visible: beside the steps on wide screens, and as the bottom bar on a phone. The phone page has no second summary card, because the bar opens the same breakdown.
- Each screen has one filled main button.
- A sold-out room type stays in the list, marked "Sold out for these dates", with its button disabled.
- "Last room" and "2 rooms left" come from real inventory. Fake urgency is never shown.
- The price shown includes a line for each night, then taxes and fees, then the total. The total on the first step equals the total charged.
- The room is held for 15 minutes when the guest reaches step 3. The hold adds to `inventory.blocked` and is released by a job if the booking is not completed.
- The booking is created in one transaction with the inventory change (spec 03). If the last room was taken meanwhile, the guest is told and returned to step 1 with the dates kept.

## Languages

The booking page ships in 10 languages. The staff app ships in English first and uses the same translation system.

| Language | Locale code | Direction |
| --- | --- | --- |
| English | `en` | Left to right |
| Chinese, Simplified | `zh-Hans` | Left to right |
| Hindi | `hi` | Left to right |
| Spanish | `es` | Left to right |
| French | `fr` | Left to right |
| Arabic | `ar` | Right to left |
| Bengali | `bn` | Left to right |
| Portuguese | `pt` | Left to right |
| Russian | `ru` | Left to right |
| Urdu | `ur` | Right to left |

This list is the 10 languages with the most total speakers, taken from memory of Ethnologue's ranking **(Claude's call)**. It should be checked, and the owner may prefer a list based on where hotel guests come from. The set of languages is data, so adding one needs translations and no code change.

### Choosing the language

1. A language in the URL wins: `book.<domain>/<hotel>/es`.
2. Otherwise the guest's earlier choice, stored in a cookie.
3. Otherwise the browser's language, if it is one of the 10.
4. Otherwise the property's default language.

A language menu is in the header on every step. It lists each language by its own name, for example "Español", with no flags.

### Two kinds of text

| Kind | Example | Who translates it | Where it lives |
| --- | --- | --- | --- |
| Interface text | "Select room", "Total" | We do, once, for all hotels | Translation files in the repo, managed by Paraglide JS |
| Hotel text | Room names, descriptions, policies, extras | The hotel | `content_translation` and `catalog_item_translation` tables |

- The owner enters hotel text in the default language. A screen shows what is missing per language.
- "Suggest translation" fills a draft by machine translation that the owner can edit. Machine drafts are marked until a person approves them.
- Missing hotel text falls back to the property's default language.

### Translation rules

- A sentence is one translation key with placeholders. Sentences are never glued from pieces.
- Plurals use ICU plural forms. Arabic and Russian have more than two forms.
- Numbers, dates and money are formatted with `Intl` for the active locale. The currency stays the property's currency.
- Interface translations start as machine drafts and are reviewed by a native speaker before release. An unreviewed language is not offered to guests.

### Right-to-left layout

- The page sets `dir="rtl"` for Arabic and Urdu.
- CSS uses logical properties only: `margin-inline-start`, `padding-inline-end`, `inset-inline-start`. Physical `left` and `right` are not used for layout.
- Arrows and other direction icons mirror. Numbers, prices and the calendar grid keep their order.
- Fonts: IBM Plex Sans covers Latin and Cyrillic. IBM Plex Sans Arabic and IBM Plex Sans Devanagari cover Arabic, Urdu and Hindi. Noto Sans SC and Noto Sans Bengali cover Chinese and Bengali. Only the fonts for the active language are loaded.

## Manage a booking

The guest opens their booking with the emailed one-time link (spec 02). In the first release the guest can do 3 things **(Claude's call)**:

1. View the booking and its payment status.
2. Resend the confirmation email.
3. Cancel the booking.

Cancelling follows the rate plan's cancellation policy (spec 03). The page states the refund or the penalty amount before the guest confirms. A refund inside the policy is created automatically and needs no staff approval. A card refund goes back through the payment provider.

Changing dates or rooms is not self-service in the first release. The page shows the hotel's phone and email for that.

Bookings made on an OTA are managed on that OTA and cannot be opened here.

## Confirmation email

- Sent in the language the guest booked in.
- Contains the booking code, dates, room, price lines, total, payment status, cancellation policy, hotel address and a "Manage a booking" link.
- The same content is kept so that front desk can resend it.

## Performance and access

- The first step loads in under 2 seconds on a mid-range phone over 4G.
- The page works with the keyboard alone and with a screen reader. Text contrast is at least 4.5 to 1.
- Inputs use a 16 px font so that iPhones do not zoom the page.
- Tap targets are at least 44 px.

## Tests this spec needs

- A full booking in each of the 10 languages, with the right text direction.
- The total on step 1 equals the amount on the confirmation and on the folio.
- Two guests booking the last room: one succeeds and the other is returned to step 1.
- An abandoned hold is released after 15 minutes.
- A language with unreviewed interface text is not offered.
- Missing hotel text shows the default language, never an empty space or a key name.
