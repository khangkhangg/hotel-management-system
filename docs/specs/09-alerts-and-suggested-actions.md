# Spec 09: Alerts and suggested actions

Part of the [Hotel PMS design](../superpowers/specs/2026-11-23-hotel-pms-design.md).

## Purpose

The system watches the hotel's data, finds problems before they cost money, and offers the fix as one button. Staff decide. The system never changes prices, bookings or channels on its own in the first release.

## How it works

1. A **check** is a rule that runs on a schedule or when data changes.
2. A check that finds something creates an **alert**: what is wrong, why the system thinks so, and who should act.
3. Most alerts carry a **suggested action**: one button that does the fix, after showing what will change.
4. An alert closes on its own when the problem is gone. Staff can also accept, dismiss or snooze it.

Checks are plain rules and simple statistics **(Claude's call)**. They are explainable, they work from day one, and they need no training data. Forecasts based on machine learning can be added once a hotel has a year of its own data.

## Rules for every alert

- It states the reason in plain words with the numbers behind it: "2 arrivals at 14:00, both rooms still dirty".
- It names one owner role. Other roles do not see it.
- One problem makes one alert. New facts update the alert and never create a second one.
- A dismissed alert does not return for the same cause for 24 hours.
- An accepted action is recorded in the activity trail with the alert that caused it (spec 10).
- Each check can be turned off, and its thresholds changed, per property by the owner or manager.

## Checks in the first release

### Bookings and rooms

| Check | Trigger | Suggested action | Goes to |
| --- | --- | --- | --- |
| Overbooked night | A channel booking arrived for a full night | "Move to another room type" with the free options listed, or "Contact guest" | Front desk, manager |
| Overbooking risk | A night has 1 room left and a channel has failed to confirm updates for 5 minutes | "Stop sell on that channel for that night" | Revenue manager |
| Room not ready | An arrival is due within 2 hours and the room is dirty or being cleaned | "Move to top of housekeeping list", or "Swap to a clean room of the same type" | Front desk, housekeeping |
| Unassigned arrival | An arrival today has no room number | "Assign room", with the best free room chosen first | Front desk |
| Assignment gap | Swapping two future bookings would free a longer run of nights in one room | "Swap rooms", showing both bookings | Front desk |
| Orphan night | 1 empty night sits between two bookings and the minimum nights rule blocks its sale | "Allow 1-night stays on that date" | Revenue manager |
| Out-of-order clash | A maintenance block overlaps a booking | "Move booking" or "Change the block dates" | Manager |
| Late checkout | A guest is still checked in 1 hour after checkout time and the room has an arrival today | "Call the room", "Charge late checkout" | Front desk |
| Likely no-show | It is 22:00, the guest has not arrived and has not paid | "Contact guest", then "Mark as no-show" after the hotel's cut-off time | Front desk |

### Money

| Check | Trigger | Suggested action | Goes to |
| --- | --- | --- | --- |
| Unpaid arrival | A guest arriving today has a balance due | "Take payment" at check-in | Front desk |
| Unpaid departure | A guest is checking out with a balance due | "Take payment" before checkout completes | Front desk |
| Bank transfer overdue | A bank transfer booking passed its payment deadline | "Extend by 24 hours" or "Cancel and release the room" | Front desk |
| Payment after cancellation | Money arrived for a cancelled booking | "Refund" or "Restore booking" | Manager |
| Refund waiting | A refund above the role limit needs approval | "Review refund" | Manager, owner |

### Rates and channels

| Check | Trigger | Suggested action | Goes to |
| --- | --- | --- | --- |
| Channel failing | 3 failed updates in a row | "See sync log" | Revenue manager |
| Channel down | No confirmed update for 30 minutes | "Stop sell on that channel", "See sync log" | Revenue manager, owner |
| Compare difference | The nightly compare found a value that differs | Fixed automatically. The alert reports what was repaired. | Revenue manager |
| Unmapped room or rate | A room type or rate plan has no mapping | "Open mapping" | Revenue manager |
| Fast pickup | Bookings for a date in the next 60 days are coming in at more than 1.5 times the pace of the same weekday in the last 8 weeks | "Raise the rate by 10%", editable before applying | Revenue manager |
| Slow pickup | A date within 14 days is under 40% sold and behind the usual pace | "Lower the rate by 10%" or "Remove the minimum nights rule" | Revenue manager |
| Rate gap | A date in the next 365 days has no rate, so it cannot be sold | "Copy the rate from the same weekday" | Revenue manager |
| Rate looks wrong | A rate was saved at less than half or more than 3 times the room type's average | "Confirm" or "Undo" | The person who saved it, then the revenue manager |

### Extras and operations

| Check | Trigger | Suggested action | Goes to |
| --- | --- | --- | --- |
| Tour under minimum | A tour departs in 24 hours with fewer guests than its minimum | "Cancel and refund" or "Run anyway" | Manager |
| Low stock | A tracked product is at or below its reorder level | "Mark as restocked" | Manager |
| Repeat issue | The same room has had 3 maintenance tickets in 30 days | "Open room history" | Manager |
| Returning guest | An arriving guest has stayed before | Shows the last stay and notes at check-in. No action. | Front desk |

### Security

Security checks are listed in spec 10, because they come from the activity trail.

## Thresholds

Every number in the tables is a default that the owner or manager can change per property. The settings screen shows each check with its switch, its numbers and how many alerts it raised in the last 30 days, so a noisy check is easy to find.

## Where staff see alerts

- A "Needs attention" panel at the top of each role's home screen, sorted by urgency, then by time.
- A badge on the place the problem lives: the booking bar on the calendar, the room in the housekeeping list, the date in the rate grid.
- Notifications, as set out in spec 10.

An empty panel says "Nothing needs attention" and shows the time of the last check.

## Measuring that it helps

For each check the system records how many alerts were accepted, dismissed, snoozed and left to close on their own. A check that staff dismiss more than half the time is flagged to the owner as "consider turning off or changing the threshold".

## Data

| Table | Key columns |
| --- | --- |
| `alert` | property_id, check_kind, subject_type, subject_id, severity, reason (structured), suggested_action (structured), status, target_role, opened_at, closed_at, closed_by, close_reason |
| `check_setting` | property_id, check_kind, enabled, thresholds (structured) |

`alert` is unique on check kind and subject while open, which is what stops duplicates.

## Tests this spec needs

- Each check raises one alert when its trigger is met, and none when it is not.
- A trigger that stays true does not create a second alert.
- An alert closes on its own when the cause is fixed elsewhere.
- A suggested action does exactly what its preview said, and writes an activity row linked to the alert.
- A role that is not the target cannot read the alert through the API.
- Turning a check off closes its open alerts.
- The pickup checks stay silent for a property with less than 8 weeks of bookings.
