# UX audit by role

Audited 2026-11-24. Each screen is checked once per role that can open it. The answers drive the change, and the change is made before the next screen.

## Questions asked for every screen and role

1. Who is looking, and in what moment (start of shift, busy desk, month end)?
2. What are they responsible for?
3. Which 1 to 3 actions did they come to take? Are those the loudest things on the screen?
4. What would help them do it better and get better results (a comparison, a reason, a warning, a default)?
5. Then the general checks in `docs/design-rules.md` (grouping, filter and search, grid forms, empty states, shared components, plain words, desktop, phone, right-to-left).

## Roles

| Role | Lands on | Responsible for | Needs to know now |
| --- | --- | --- | --- |
| Owner | Overview | Whole hotel, money, people | What needs a decision, how the hotel is doing against its own numbers, who did what |
| Manager | Overview | Running the property day to day | Problems to resolve, staff load, rooms and bookings that are stuck |
| Front desk | Calendar | Guests arriving, staying, leaving | Who arrives and what blocks check-in (no room, unpaid), messages waiting |
| Revenue | Rates | Prices and sales rules, channels | Where prices lag demand, channel errors, what a change will do |
| Housekeeping | Rooms | Clean rooms on time, report issues | Which rooms to do first (arrivals today), what is blocked |
| Guest | Booking site | Finding a room and booking it | Price total, what is included, cancellation rule, trust |

## Personas and journeys

Each journey is walked in the running app as that person, screen to screen. A step that needs more than one obvious click, a guess, or a trip to another screen to find information is a finding.

| Persona | Who | Goal | Fear |
| --- | --- | --- | --- |
| Linh, owner | Owns 1 hotel, checks on a phone in the morning and at a desk in the evening | Know if the month is on track, no surprises with money or staff | Finding a problem from a guest, not from the system |
| Daniel, manager | Runs the property, 10 to 12 hour days | Clear what is stuck, keep staff and rooms moving | A refund or complaint sitting unseen |
| Lan, front desk | Busy desk, one hand on the phone | Check guests in and out in under a minute | A guest arriving with no room or an unpaid stay |
| Rita, revenue | Part time, prices and channels | Raise prices when demand is high, catch errors on channels | A channel showing a wrong price or no stock |
| Tuan, housekeeping | On a phone, on his feet | Know which rooms to clean first and report problems | Cleaning a room a guest is waiting for last |
| Ana, guest | Booking a room on her phone, in her language | Find a room, see the full price, pay safely | Hidden fees, not knowing the cancellation rule |

| Journey | Steps | Screens |
| --- | --- | --- |
| Owner: morning check | Sign in, see what needs a decision, see month against last month, find who did something odd | Sign in, Overview, Payments, Activity |
| Front desk: arrival | Sign in, open today, find Ana, give her a room, take payment, check in | Sign in, Calendar, booking panel, Payments |
| Revenue: weekend price | Sign in, see rates for next weekend, raise them, see what channels will receive | Sign in, Rates, Price suggestions, Channels |
| Housekeeping: morning round | Sign in, see rooms to clean with arrivals first, mark clean, report a broken lamp | Sign in, Rooms, Issues |
| Guest: booking | Land from search, pick dates, see total, enter details, pay, get confirmation | Booking site |
| Manager: complaint | See issue, find the booking and what was done, reply, offer a refund | Overview, Issues, Messages, Payments, Activity |

Every journey starts at the sign-in page, the header and the sidebar, so those three are audited as part of each one.

## Findings and changes

Filled in screen by screen below.

### Overview (owner, manager)

Purpose: tell the person who answers for the hotel what needs a decision now and how the hotel is doing.

| Change | Why |
| --- | --- |
| Money row: taken this month against the same days last month, refunds waiting | An owner judges results by money and trend. The screen showed only rooms, so it could not say if the month was good. |
| Needs attention stays first, each alert on one line with its actions | It is the thing to act on. Stacked cards pushed the numbers down the page. |
| Five room tiles in one row | The fifth tile sat alone on a second row and looked like a mistake. |
| "Go to" links grouped by purpose and ordered as in the menu, All properties inside Hotel | Single-item groups and random order slowed scanning. |

### Calendar (front desk, manager, owner)

Purpose: see who is in which room and place unplaced bookings.

| Change | Why |
| --- | --- |
| Alerts on one line each | Two tall cards pushed the grid below the fold, and the grid is the job. |
| Hint under "Bookings without a room" | The chips looked like labels. The hint says they open the booking to give it a room. |

### Payments (owner, manager, front desk)

Purpose: see what was taken and decide refunds.

| Change | Why |
| --- | --- |
| Refunds section always shown, with "none waiting" | The subtitle promised refunds, the screen hid them when empty, so the manager could not tell if the check had run. |
| Empty text says what fills the list | "No payments today yet" gave no next step. |
| Tile no longer stretches full width | A single tile across the page looked broken. |

### Activity (owner, manager)

Purpose: find what went wrong, fast.

| Change | Why |
| --- | --- |
| Search and staff filter, backed by a new `q` search on the API | The owner asked to filter by staff and search. Tested for action, target, reason and person. |
| Codes such as `alert.opened` read as "opened alert" | Codes are for developers, not for the person looking for a problem. |

### Rates (revenue, manager, owner)

Purpose: set prices and sales rules and see what is in force.

| Change | Why |
| --- | --- |
| Nights with the same price and rules fold into one row | 30 identical rows hid the one night that differed, which is what a revenue manager looks for. |

### Rooms (housekeeping, manager, owner)

Purpose: housekeeping cleans in the right order, managers see room state.

| Change | Why |
| --- | --- |
| Summary line: clean, dirty, waiting for inspection | The manager had to count badges. |
| Buttons say "Mark clean", "Mark dirty", "Needs inspection" | "Dirty" on a clean room read as a state, not an action. |
| Status words start with a capital | Lowercase badges looked unfinished. |

### Sign in (everyone)

Purpose: get each person into their own view quickly and safely.

| Change | Why |
| --- | --- |
| Form in a centred card | The form hung at the top of an empty screen and looked unfinished. A card gives one clear place to look. |
| Line "You see the screens for your role." | Tells a new user that the app adapts to them, so they do not look for missing screens. |
| "or" between the password button and Google | Two full-width buttons competed. The divider shows Google is an alternative. |
| Not done | Show password, autofocus on email. The shared Field has neither, so adding them is a separate change. |

### Header and sidebar (everyone)

Purpose: say who and where you are, and get to the 5 screens of your role.

| Change | Why |
| --- | --- |
| Signed-in name beside the bell | Front desk computers are shared. An icon-only sign-out gave no sign of who was signed in. |
| Role pill under the hotel name (earlier) | The owner asked that each person see their own role. |
| One compact "Viewing as" menu, owner only (earlier) | Two wide buttons crowded the header and invited every role to try it. |
| Not done | Unread count on Messages and today's count on Arrivals in the menu. Needs a count endpoint for each, so it is a separate change. |

### Arrivals (front desk, manager)

Purpose: check guests in and out in under a minute and see what blocks each check-in.

| Change | Why |
| --- | --- |
| "No room yet" as a warning badge | A missing room blocks check-in. Plain text in the table did not stand out from a normal row. |
| Status words capitalised | Lowercase badges looked unfinished. |
| Not done | A "Due" column with the balance. The calendar API has no balance per stay, so it needs an API change. Today the alert above the table carries it. |

### Guests (front desk, manager)

Purpose: recognise a returning guest and find a booking when someone calls.

| Change | Why |
| --- | --- |
| Search matches phone numbers (only for roles that can see contact details), tested | People often call with a number, not an email. Roles without contact access still cannot search by it. |
| Not done | Stays count and last stay beside each guest. Needs an API change. |

### Messages (front desk, manager)

Purpose: answer guests before they call.

| Change | Why |
| --- | --- |
| Empty text says where messages come from | "No guest has written yet" did not say how a message would arrive, so an empty list looked like a fault. |
| Not done | Unread count in the menu, and a populated-list review (no message data in the sample hotel). |
