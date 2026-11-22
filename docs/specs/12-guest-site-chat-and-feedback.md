# Spec 12: Guest site, guest chat and feedback

Part of the [Hotel PMS design](../superpowers/specs/2026-11-23-hotel-pms-design.md).

This spec adds the page a guest sees before booking, the chat between a guest and the hotel, and guest feedback. The booking steps themselves are in spec 05.

## Guest site

Each property gets a public page at `book.<domain>/<hotel-slug>`. It is the hotel's landing page and the entry to booking.

### Sections, in order

| Section | Content | Why it is there |
| --- | --- | --- |
| Header | Left: logo and hotel name. Next to it: links to Rooms, Extras, Reviews and Location. Right: language menu with no border, "Manage a booking" as a quiet button, and an outlined "Book a room" button. The header stays at the top while the guest scrolls. | Identity, navigation, and booking always one click away |
| Top | The headline across the full width with the review score on the right. Under it a photo mosaic: one large photo and two smaller ones. The date search is one white bar that floats over the bottom edge of the photos, with Check-in, Check-out, Guests and "Check availability" in a single row. | A guest decides here whether to keep reading |
| Rooms | One card per room type: photo gallery, size, beds, amenities, price from, "See prices for your dates" | The main choice |

On a phone the header is one row: logo, hotel name and language menu. "Manage a booking" moves into the page under the search bar, and the section links are left out because the page is one scroll. The top section stacks: headline, review score, one large photo with three small ones under it, then the search bar. The chat button shows only the chat icon.
| Amenities | A plain list of what the hotel offers, with hours | Answers the most common questions before chat |
| Extras | Add-on services, tours and products with prices (spec 07) | Shows what can be added when booking |
| Guest reviews | Score, count, category scores, recent reviews with hotel replies | Trust, from verified stays only |
| Location and contact | Map, address, check-in and check-out times, phone, email | Practical facts |
| Chat button | Opens the chat (below) | Help at any point |

The owner turns sections on or off and edits their text in Settings, Guest site. Sections with no content do not appear.

### Photos

- The hotel uploads several photos for the property, for each room type and for each extra.
- Each photo has a short description in each language, used as alternative text.
- In the top mosaic, clicking a small photo makes it the large one. A counter shows "1 of 4".
- A room gallery has no arrow buttons. The owner asked for them to be removed. Tapping or clicking the photo shows the next one, a swipe does the same on touch screens, and a counter shows "2 of 4". The photo is one button, so it works with the keyboard and is announced as "Photo 2 of 4 of the deluxe room. Show the next photo."
- Photos never change by themselves.
- The server makes several sizes of each photo in WebP and AVIF. The page loads the size that fits and loads photos below the first screen only when they come into view.

### Copy rules

The guest site follows the same writing rules as the app, checked with the `no-ai-slop` skill:

- The first sentence states facts a guest can check: number of rooms, where, how far from what.
- Headings name their subject: "Rooms", "Amenities", "Guest reviews".
- No claim without a source. A review score shows its count and that only guests who stayed can write one.
- No buzzwords, no exclamation marks, no greeting as a title.

### Motion

| Moment | Motion | Time |
| --- | --- | --- |
| Section comes into view | Fades in and rises 16 px, once | 250 ms |
| Top mosaic | Photos cross-fade when the guest picks another | 300 ms |
| Room gallery | Photos slide sideways | 250 ms |
| Chat opens | Panel fades in and rises 8 px | 200 ms |
| Hover on buttons | Colour change only | 150 ms |

All motion follows spec 08: no bounce, no hover growth, nothing that moves on its own, and reduced motion is respected. Content is visible without JavaScript and without motion.

### Search engines and sharing

- The page is rendered on the server and has a title, description and social preview image per language.
- Structured data marks up the hotel, its rooms and its review score.
- Each language has its own URL and `hreflang` links.

## Guest chat

### What the guest sees

1. A "Chat with the hotel" button on every page of the guest site and the booking flow.
2. The panel says who answers ("A person at the front desk") and whether the hotel is online.
3. Up to 3 common questions the owner chose are shown as buttons. Tapping one sends it.
4. The guest types in their own language.
5. When the hotel is offline, the panel says so and asks for an email address, and the answer is sent there.

A guest with a booking is recognised from the "Manage a booking" link, and the chat is attached to that booking. A visitor without a booking gives a name, and an email only if they want the answer by email.

### What staff see

Guest chat arrives in **Messages** in the staff app, in the front desk and manager menus.

- The list shows every conversation with the guest's name, where it came from, the linked booking and the last message. Unanswered ones are bold and first.
- Sources in one list: website chat, stay feedback, and channel messages where the channel manager provides them.
- Opening a conversation shows the thread, the booking status and "Open booking".
- The reply box has two modes: **Reply to guest** and **Internal note**. An internal note is drawn with a dashed outline and the words "internal note", so nobody sends one to a guest by mistake.
- Internal notes use the same tagging as booking notes (spec 11). Hovering or focusing a tag shows a rich tooltip with the person's role, shift and last activity.
- A conversation linked to a booking and that booking's notes are one thread, seen from two places.

### Smart help for staff

- **Translation.** A guest message in another language is shown translated, marked "Translated from Spanish", with "Show original". The staff reply is translated into the guest's language before sending, and the guest sees it in their language. Staff see what will be sent before they send it.
- **Suggested replies.** For a common question the system drafts a reply from the hotel's own data: hours, policies, room status and earlier answers. Staff click one to edit it, then send. Nothing is sent to a guest without a person pressing send **(Claude's call)**.
- **Context.** A question about early check-in shows the room's cleaning status beside the thread.
- **Routing.** A new chat raises a notification to front desk on shift (spec 10). No answer in 5 minutes during staffed hours escalates to the manager.

Translation and reply drafts use a language model through an API. The hotel's data is sent only for the conversation at hand, and the provider must not train on it. This is a paid service and the one place in the product where a free library does not do the job.

### Rules

- Guests never see internal notes, staff names beyond a first name, or other guests' data.
- Chat text from guests is treated as untrusted input: it is escaped on display, and instructions inside it are never followed by the drafting model.
- Links and files from guests open only after a warning.
- Chat is rate-limited per visitor and protected against automated abuse.
- A guest can ask for their conversation to be deleted. It is removed from the guest's view and kept in the activity trail for the period the law requires.

## Feedback

### During the stay

- On the second day the guest gets a message: "How is your stay so far?" with a 1 to 5 rating and a text box.
- A rating of 3 or lower raises an "action needed" alert to the manager with the text (spec 09), so the hotel can fix the problem while the guest is still there.
- In-stay feedback is private and never published.

### After the stay

- The day after check-out the guest gets an email with a link to rate the stay: an overall score, category scores (cleanliness, staff, breakfast, location), and a comment.
- The guest chooses whether the comment may be published as a review.
- Only a completed stay can leave a review, one per booking. The link is tied to the booking code.
- The hotel can reply once to each review. The reply is public.
- The hotel cannot edit or delete a review. It can report one that breaks the rules (abuse, private data, not about the stay), and a reported review is hidden until the platform operator decides.
- The guest site shows the average, the count, and the most recent reviews first. It never shows only the best ones.

### Guest site form

The guest site also has "Tell us about your stay" with booking code, rating and comment, for guests who lost the email.

### What staff see

- Feedback arrives in Messages with the source "Stay feedback".
- Owner reports show the score by month, by category and by room, and the share of guests who answered.
- A room with 3 low scores in 30 days raises an alert (spec 09).

## Data

| Table | Key columns |
| --- | --- |
| `conversation` | property_id, reservation_id (nullable), guest_id (nullable), source, visitor_name, visitor_email, locale, status, last_message_at |
| `message` | conversation_id, sender_kind (guest, staff, system), sender_user_id, body, body_translated, original_locale, internal, created_at |
| `quick_question` | property_id, locale, text, position |
| `review` | reservation_id (unique), overall, category_scores, comment, publish_allowed, status, hotel_reply, replied_at |
| `stay_feedback` | reservation_id, rating, comment, created_at |
| `photo` | property_id, owner_type, owner_id, file_id, position |
| `photo_translation` | photo_id, locale, description |
| `site_section` | property_id, kind, enabled, position, content |

Internal notes on a conversation are rows in the `note` table of spec 11, so one booking has one set of notes.

## Tests this spec needs

- An internal note never appears in the guest's chat panel or in any guest email.
- A guest message in Spanish reaches staff translated, and the original can be shown.
- A staff reply is delivered in the guest's language, and the staff member saw that text before sending.
- No reply is sent to a guest without a staff action.
- Text in a guest message that looks like an instruction does not change the drafted reply's behaviour.
- A review cannot be written without a completed stay, and only once per booking.
- The hotel cannot edit or delete a review through any endpoint.
- An in-stay rating of 3 raises the manager alert.
- The guest site loads and shows all content with JavaScript off and with reduced motion on.
- A conversation and its booking show the same internal notes.
