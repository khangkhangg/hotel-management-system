# Spec 11: Team notes on bookings

Part of the [Hotel PMS design](../superpowers/specs/2026-11-23-hotel-pms-design.md).

## Purpose

Staff talk about a booking on the booking itself, and tag the colleague who must act. The conversation stays with the booking, so the next shift reads it there and nobody needs a separate chat app.

## How it works

1. Every booking has one thread, shown as "Activity and team notes" on the booking.
2. The thread mixes two kinds of rows in time order: system events from the activity trail (spec 10) and notes written by staff.
3. A staff member writes a note and can tag colleagues with `@`.
4. A tagged colleague gets a notification at the "action needed" level, with the note text and a link to the booking (spec 10).
5. New notes appear live for everyone who has the booking open.

Notes and the activity trail share one thread **(Claude's call)**. One list answers "what happened and what did we say about it", and it takes less space than two.

## Tagging

- Typing `@` opens a list of colleagues who work at the property. Typing letters narrows it. Arrow keys and Enter pick a name.
- Each name shows the person's role, so "Lan Vo, Housekeeping" is clear.
- `@frontdesk`, `@housekeeping`, `@revenue` and `@managers` tag everyone with that role who is on shift.
- Only people who are allowed to see the booking can be tagged directly.

### Tagging housekeeping

Housekeeping cannot open bookings or see guest names (spec 01). When a note tags a housekeeper **(Claude's call)**:

- The housekeeper gets the note text, the room number and the author.
- The note also appears on that room's task in the housekeeping app, where the housekeeper replies.
- The reply shows in the booking thread.
- The guest's name, contact details and prices are never included. If the note text itself contains the guest's name, the author sees a reminder before sending.

## What a note can hold

- Plain text up to 2,000 characters, with line breaks.
- Tags.
- A photo, for example of damage or a lost item. Photos follow the upload rules in spec 02.
- A "pin" mark. A pinned note, such as "Guest is allergic to nuts", stays at the top of the thread and is shown at check-in.

## Rules

- Notes are internal. They are never shown to the guest, never sent to a channel and never put in a guest email.
- The author can edit a note for 5 minutes. After that it is fixed.
- The author or a manager can remove a note. A removed note shows "Removed by Mai Pham" and its text is kept in the activity trail for owners.
- A note can be marked "Done" by the person tagged. An open tag older than 4 hours raises a "Tag not answered" alert to the author (spec 09).
- A booking with an unread note for the current user shows a dot on its calendar bar and in lists.
- Notes are searchable by text, author and date from the Activity screen.
- Notes are kept as long as the booking is kept.

## Where notes appear

| Place | What shows |
| --- | --- |
| Booking detail | The full thread and the box to write a note |
| Calendar bar tooltip | "2 notes, 1 for you" when there are unread notes |
| Check-in screen | Pinned notes |
| Housekeeping room task | Notes that tagged housekeeping, without guest details |
| Notifications | Each tag, with the note text |
| Shift handover | Open tags and notes written during the shift |

Room, guest and maintenance ticket records can get the same thread later. The first release has it on bookings and housekeeping room tasks only.

## Permissions

Anyone who can open a booking can read and write its notes. Housekeeping reads and writes only on room tasks.

## Data

| Table | Key columns |
| --- | --- |
| `note` | property_id, reservation_id (nullable), room_id (nullable), author_id, body, pinned, edited_at, removed_at, removed_by, created_at |
| `note_mention` | note_id, user_id (nullable), role (nullable), done_at |
| `note_read` | note_id, user_id, read_at |
| `note_attachment` | note_id, file_id |

## Tests this spec needs

- Tagging a user creates exactly one notification for that user.
- A user outside the property cannot be tagged.
- A housekeeper's copy of a note contains no guest name, contact detail or price.
- A note cannot be edited after 5 minutes.
- A removed note is hidden from staff and still readable by the owner in the activity trail.
- A new note appears in a second open browser without a reload.
- Notes never appear in any guest email or channel payload.
- An unanswered tag raises its alert after 4 hours and closes when marked done.
