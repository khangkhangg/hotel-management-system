# Spec 10: Notifications and activity trail

Part of the [Hotel PMS design](../superpowers/specs/2026-11-23-hotel-pms-design.md).

## Part A: Notifications

### Purpose

The right person hears about a problem once, through a channel that matches how urgent it is, with the fix one tap away.

### Three levels

| Level | Meaning | Examples | How it reaches staff |
| --- | --- | --- | --- |
| Urgent | Money or a guest is at risk now | Overbooked night, channel down, payment after cancellation | In-app at once, push to the phone, and email. Ignores quiet hours. |
| Action needed | Someone should act today | Room not ready, unpaid arrival, refund waiting | In-app at once, push to the phone |
| For your information | Good to know | New booking, compare repaired a value, returning guest | In-app only, and the daily summary |

### Channels

- **In-app.** A bell with a count, a list, and a live update through server-sent events. No page reload is needed.
- **Push to the phone or desktop.** Web push from the installed web app. On an iPhone this works only after the user adds the app to the home screen.
- **Email.** For urgent alerts and for summaries.
- **Chat apps.** Telegram, Zalo, WhatsApp or Slack are not in the first release. The sender is built behind one interface so they can be added.

### What makes it smart

1. **Sent by role and property.** A notification goes to the role named on the alert (spec 09), and only to people who work at that property and are on shift if shifts are set.
2. **One problem, one notification.** New facts update the same notification. A channel that fails 20 times sends 1 notification, with a count.
3. **Grouped.** Several of the same kind arrive as one: "4 arrivals have rooms that are not ready".
4. **Escalation.** An urgent alert that nobody opens goes to the manager after 10 minutes and to the owner after 30 minutes. Opening or acting on it stops the escalation.
5. **Closes itself.** When the problem is fixed, the notification is marked resolved for everyone, with who fixed it.
6. **Acts in place.** Each notification carries the alert's suggested action. One tap opens the preview of the fix.
7. **Quiet hours.** Each user sets hours when only urgent notifications get through.
8. **Summaries.** The owner and manager get a morning email: yesterday's numbers, alerts opened and closed, and what is still open.
9. **Limits.** No user gets more than 10 pushes an hour. Above that, notifications are held and sent as one group.

### What each user controls

- Channels per level, except that urgent in-app notifications cannot be turned off.
- Quiet hours.
- The language of notifications.

### What the owner controls

- Escalation times.
- Which roles get which checks, starting from the defaults in spec 09.
- The time of the daily summary.

### Guest messages

Emails to guests (confirmation, payment received, cancellation, pre-arrival) are a separate list with their own templates and the guest's language (spec 05). Staff can see every message sent to a guest on the booking.

### Data

| Table | Key columns |
| --- | --- |
| `notification` | alert_id (nullable), user_id, level, title, body, action, status, sent_at, opened_at, resolved_at |
| `notification_delivery` | notification_id, channel, status, tries, error, delivered_at |
| `notification_preference` | user_id, level, channel, enabled, quiet_from, quiet_to |
| `push_subscription` | user_id, endpoint, keys (encrypted), device_label |

## Part B: Activity trail

### Purpose

When something goes wrong, the owner can find out what happened, who did it, when, from where, and what the value was before.

This builds on the audit log in spec 02. That spec says what is written. This part says how people read and use it.

### What is recorded

Every action that changes data writes one row. So do these read actions, because they matter when tracing a leak:

- Opening a guest's ID document.
- Exporting a report or a guest list.
- Viewing a payment provider setting.
- Signing in, signing out and failed sign-ins.

Each row holds:

| Field | Example |
| --- | --- |
| When | 2026-11-23 14:02:11, property time and UTC |
| Who | Mai Pham, front desk |
| Role view | "Owner viewing as front desk", when that applies |
| What | Changed rate |
| On what | Deluxe, Fri 27 Nov, Flexible plan |
| Before and after | 104 to 112 |
| Why | Free text when the action asks for a reason, or the alert that suggested it |
| Where from | Device label, browser, IP address, rough location |
| Trace ID | Links this row to everything it caused |

Actions by the system are recorded too, with the actor "System" and the job or check that did it.

The system does not record keystrokes, screens or mouse movement. Staff are told at first sign-in that their actions in the app are logged.

### Tracing an issue back

The trace ID connects one action to its effects. Starting from any of these, the trail shows the full chain:

- A wrong price on an OTA leads to the sync attempt, the outbox row, the rate change, the person and the device.
- A missing booking leads to the cancellation, who cancelled it, the reason given and the refund.
- A refund leads to who asked, who approved, and the payment it came from.
- An overbooked night leads to both bookings, their channels, and the sync state at that moment.

### Where staff read it

- **History tab on every record.** Booking, guest, room, rate date, extra, user and setting each show their own timeline in plain sentences: "Mai Pham changed the departure from 25 Nov to 26 Nov. 14:02."
- **Activity screen.** For owners and managers: filter by person, action, record, property and date, then export.
- **Per-person view.** Everything one user did in a period, for reviews and handovers.
- **Shift handover.** A summary of a shift: check-ins, check-outs, payments taken by method, refunds, changes and open alerts. The next shift reads it at sign-in.

Managers see activity for their property. Owners see all properties. Other roles see the History tab only on records they can open, without IP addresses.

### Undo

Where an action can be reversed safely, its row in the History tab has an "Undo" button for the person who did it and for managers, for 24 hours. Rate changes, restriction changes, room assignments and catalog edits qualify. Payments, refunds, cancellations sent to a channel and check-outs do not, and the trail says why.

### Proof against tampering

- The application's database role cannot update or delete trail rows (spec 02).
- Each row stores a hash of the row before it, per organization. A nightly job checks the chain and raises an urgent alert if it is broken.
- Rows are kept for 2 years **(Claude's call)**. The legal minimum for the hotel's country must be checked once the country is known.

### Security checks

These checks read the trail and raise alerts through spec 09. They go to the owner.

| Check | Trigger |
| --- | --- |
| New sign-in place | A user signs in from a country they have not used before |
| Sign-in attack | 10 failed sign-ins on one account in 15 minutes |
| Unusual refunds | A user's refunds in a day exceed 3 times their 30-day daily average |
| Rate change at an odd hour | A rate is changed between 00:00 and 05:00 property time |
| Large export | A guest list of more than 500 rows is exported |
| Many cancellations | One user cancels more than 5 bookings in an hour |
| Cash mismatch | Cash payments recorded in a shift differ from the amount counted at handover |
| Role change | A user is made owner or manager |
| Trail broken | The nightly hash check fails |

Each is a signal for the owner to look, not proof of wrongdoing. The alert text says so.

## Tests this spec needs

- A notification reaches only users with the target role at that property.
- 20 failures of one channel produce 1 notification with a count of 20.
- An unopened urgent alert escalates at 10 and 30 minutes, and stops when opened.
- Quiet hours hold back "action needed" pushes and let urgent ones through.
- Fixing a problem marks its notification resolved for every recipient.
- Every write endpoint creates exactly one trail row with before and after values.
- From an OTA sync attempt, the trace ID leads back to the user action that caused it.
- Undo restores the earlier value, writes its own trail row, and is refused after 24 hours.
- Changing a trail row by hand in the database makes the nightly check fail.
- A front desk user cannot read IP addresses from any History tab.
