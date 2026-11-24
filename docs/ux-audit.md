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

### Issues (housekeeping, manager)

Purpose: report something broken in a room in a few seconds, and see what is still open.

| Change | Why |
| --- | --- |
| "New ticket" became "Report a problem", "Close ticket" became "Mark fixed" | "Ticket" is help desk wording. Tuan reports a broken lamp, and says it that way. |
| Empty text says nothing is broken and names the action | "No maintenance tickets" read as a missing feature. |

### Rooms, cleaning list (housekeeping)

| Change | Why |
| --- | --- |
| Rooms with a guest arriving today come first, finished rooms last | Tuan's fear is cleaning the room a guest is waiting for last. The list was in database order. |

### Channels (revenue, manager, owner)

Purpose: connect the hotel to booking sites and keep prices and stock right on them.

| Change | Why |
| --- | --- |
| The 11 channel managers that need vendor approval sit in one closed group | Eleven closed rows pushed the real choices and the setup form far down the page, for options Rita cannot use today. |
| Connection fields in a grid, the key hint on its own row | Three short codes were full-width boxes. |
| Not done | A health summary (connected, last sync, failed pushes) at the top once a channel is connected. The sample hotel has none connected, so it could not be reviewed on screen. |

### Sidebar brand (everyone)

| Change | Why |
| --- | --- |
| Long hotel names shorten with an ellipsis | At a narrow width "Sample Hotel" ran under the collapse button, so the name and the button overlapped. |

### Sync log (revenue, manager)

| Change | Why |
| --- | --- |
| Empty text names Channels and says what will be listed | Rita could not tell if the log was broken or just unused. |

### Price suggestions (revenue, manager)

Purpose: decide which suggested prices to apply, knowing why.

| Change | Why |
| --- | --- |
| Dates read "Nov 24", not "2026-11-24" | Machine dates are slower to scan, and the year is clear from context. |
| Apply button shows how many are checked | After scrolling a long list, Rita needs to know what the button will do. |
| Not done | Expected revenue effect per suggestion and for the whole set. Needs a forecast on the API. |

### No access page (everyone)

| Change | Why |
| --- | --- |
| Button "Go to your start screen" | The page said what was wrong but left a dead end. One click should return the person to work. |

### Reports (manager, owner)

Purpose: get records out for month-end accounting.

| Change | Why |
| --- | --- |
| Buttons "This month", "Last month", "Last 30 days" | Accounting runs on whole months. Picking two dates by hand each time invited a wrong range. |
| Dates side by side, page on Section and FormGrid | Two short fields were full-width, and the page had its own card style. |

### Rate plans (revenue, manager)

Purpose: see how each room type is sold, then set its prices.

| Change | Why |
| --- | --- |
| "Set rates" on each row opens Rates with that plan chosen | The table was a dead end. The next step is always the prices of that plan. |

### Yield rules (revenue)

Purpose: hold back rooms or adjust prices on booking sites without touching the hotel's own page.

| Change | Why |
| --- | --- |
| "Your rules" first, the new rule form second | What is already in force matters more than creating another, and a new rule should be written knowing what exists. |
| Short fields in a grid, built on Section | One column of full-width inputs made a short form look long. |

### Promo codes (owner, manager, revenue)

Purpose: give guests a discount code, knowing which codes are live.

| Change | Why |
| --- | --- |
| "Your codes" first, the new code form second | Before adding a code, the person should see which exist, so they do not duplicate or clash. |
| Seven short fields in a grid | A column of seven full-width boxes made adding a code look like a long task. |

### Reviews (owner, manager)

| Change | Why |
| --- | --- |
| Empty text says how a review arrives (link in the booking email, after check-out) | An empty list gave the owner no way to tell whether reviews were switched off or just not written. |

### Payouts (owner)

Purpose: know what to expect in the bank, by day.

| Change | Why |
| --- | --- |
| The same "This month, Last month, Last 30 days" buttons as Reports, from one shared component | The owner reconciles the bank by month, and one component keeps the two screens identical. |

### Lost and found (housekeeping, front desk, manager)

Purpose: front desk answers "did you find my charger?", housekeeping logs what turns up.

| Change | Why |
| --- | --- |
| Items waiting first, the log form second | The desk looks up items far more often than it adds them, and often with a guest on the phone. |
| Fields in a grid on Section | Three fields in one tall column, with the page's own card style. |

### All properties (owner)

| Change | Why |
| --- | --- |
| Tiles sized so all five sit in one row | The fifth tile ("Paid in") wrapped alone onto a second row. |
| Not done | Compare "Paid in" with the 30 days before. Needs a second figure from the API. |

### Site content (owner, manager)

Purpose: make the booking page persuasive, since guests decide on its words and photos.

| Change | Why |
| --- | --- |
| "See the page as a guest" in the page header | The owner edits blind otherwise. One click shows what a guest sees, so each edit can be checked. |
| Not done | A completeness hint (tagline, description, photo missing), and the file chooser still shows the browser's own "Choose File" wording. |

### Branding (owner)

| Change | Why |
| --- | --- |
| Preview moved above the colour form | The owner picks a colour to see its effect. With the preview below a long form, the result was out of sight while choosing. |
| Built on Section | The three blocks each had their own card style. |

### Booking site, room choice (guest)

Purpose: find a room, see the full price and know the rules before paying.

| Change | Why |
| --- | --- |
| Each room card states the cancellation rule in a sentence, from the rate plan's own terms (API tested) | Ana's fear is not knowing what cancelling costs. The card said only "Flexible", which is a name, not a rule. |
| The sentence is translated in all 9 draft languages | The guest page is multilingual. The drafts wait for native review like the rest. |
| Not done | Room photos and descriptions beside each room (the sample hotel has none), and a total with taxes broken down. |

## Round 4: design system pass (2026-11-24, /khang-design-audit)

Method: docs/design-system.md and the khang-design-audit skill. Baseline before changes: 146 unit tests, svelte-check clean, 90 Playwright tests, ai-design-check 3 findings (2 locale-aware-case, 1 no-exclamation-copy). Screens done so far: Promo codes, Lost and found, Guests, and the shell edge. Screens still on the old pattern are listed at the end.

### Promo codes as manager at 1280
Purpose: let the manager give guests a discount code for the booking page.
Role and actions: manager. Add a code, switch it on or off, remove it.
Findings:
1. The new-code form sits under the list, with 7 fields in one row (A, slows the task).
2. Codes were a list of boxes, so status, uses and rules were hard to compare (A).
3. The empty state said "No codes yet." and nothing else (D).
Changes:
1. A "New code" button in the header opens a side panel with the form - Why: the list stays in view and the page stays short.
2. Codes are a table with discount, rules, uses and an On or Off badge - Why: a manager can see which codes are live at one look.
3. The saved row is marked and the panel asks before discarding typed values - Why: the person sees where the new code went and never loses typing by accident.
4. The empty state says to use New code - Why: it names the next step without a second identical button.
Not done: search shows only above 6 codes. Dates in rules show as typed (ISO), not as local dates.

### Lost and found as manager, housekeeping and front desk at 1280
Purpose: keep a record of what guests left behind, and who got it back.
Role and actions: housekeeping logs and hands over. Manager and owner the same. Front desk only looks.
Findings:
1. Every item showed two inline fields for the owner's name and contact, even when nothing was being handed over (A).
2. The log form sat under the list (A).
Changes:
1. "Log an item" opens a side panel, shown only to roles that may log - Why: the front desk never sees a button it cannot use.
2. "Hand over" on a shelf item opens a panel for who collected it, with "Mark returned" and a quieter "Mark thrown away" - Why: closing an item is a decision, so it gets its own space and the destructive choice is not the primary button.
3. Table with item, place and day, status - Why: scanning what is on the shelf is the main job.
Not done: a photo for each item needs file storage.

### Guests as front desk and revenue at 1280
Purpose: find a guest and see their stays.
Findings:
1. The detail opened in a wide centered dialog, covering the list (A).
2. The search box was a plain field, and an empty result gave no hint (A, D).
Changes:
1. Detail opens in the side panel - Why: the list and the search stay in view.
2. Search says what was searched when nothing matches - Why: the person knows whether to fix a spelling or try a phone number.
3. Revenue still gets no email or phone columns - Why: privacy rule, unchanged.
Not done: stays count and last stay in the table need an API change.

### Shell edge at 1280
Findings: the page title in the top bar sat 8px to the left of the page content, and content was centered while the title was not (C, looks).
Changes: content is left aligned and the top bar uses the same side padding - Why: the title and the page now start on one edge, as rule 5 asks.
Not done: other screens still to move to the table and panel pattern are Arrivals, Payments, Activity, Yield rules, Rates, Rate plans, Chain plans, Staff, Rooms, Issues, Channels, Pricing and Reviews. Phone and tablet checked only by the Playwright axe and fit tests at 390, 744 and 1280, not by eye.

### Round 4b: Neo check of the finished screens, and Arrivals, Payments, Activity
A separate agent walked Promo codes, Lost and found, Guests and the shell in the Neo browser at 1797px against the skill checklist, and reported 13 failures. Arrivals and Payments moved to the table, and Activity got the search box and filter chips.
Fixed:
1. Remove promo code deleted on one click - now asks, names the code and says it cannot be undone. Why: a mistaken click loses a code guests may be using.
2. Mark thrown away sat beside Mark returned and acted at once - now asks first, and the discard panel's "Discard" is the quiet button while "Keep editing" is primary. Why: the loud button should be the safe one.
3. Panel opened with focus on Close - the cursor now starts in the first field. Why: typing can start at once.
4. Promo panel gave no example of the discount - it now shows what a 100 USD stay costs after the code. Why: money effect shown before saving.
5. Guests: no "Clear search" on an empty result, the search box had no visible text, email and phone ran together, status words were lowercase, the stay link used the browser default - all fixed.
6. Lost and found: place and date were one column - now two.
Not fixed (needs work of its own):
1. Success messages are banners that push the table down. Needs a Toast component (design system: 5 seconds, Undo where possible).
2. Guest panel is not the two column label and value layout, and money is not first for front desk.
3. Guests as revenue shows a no-access page, so the "names without contact" columns were not looked at in the browser (the Playwright API test covers it).
4. Sidebar of manager has no Promo codes, front desk no Lost and found: they sit behind the Overview links by the role menu design. Decide if that is right.
5. The "Viewing as" yellow banner pushes the page down.
6. Phone, tablet, right-to-left and long text were not looked at by eye.

### Round 4c: Toast, more screens on the pattern, second Neo check
New: Toast (bottom right, 3 at most, about 5 seconds, no layout shift). On the table and side panel pattern now: Yield rules, Issues, Staff, Rate plans, Chain plans, Arrivals, Payments. Activity has the shared search box and filter chips. Not changed on purpose: Rooms and Pricing (cards people compare), Rates (a grid), Channels (setup steps), Reviews (moderation list).
A second agent walked Arrivals, Payments, Activity, Yield, Issues and Staff in Neo at 1797px and reported 15 failures. Fixed:
1. Issues room list read "101 (undefined)" - Why: wrong data. The room type name is shown only when the API sends it.
2. Take payment started empty - Why: the usual payment is what is owed. The amount now starts as the balance (new helper plainAmount, tested).
3. Activity showed raw "system:alerts" - now "System".
4. Row buttons had no names ("Switch off", "Remove") - they now carry the code or rule name for screen readers.
5. Empty tables sat in a second bordered box inside a card - the empty message has no own border now.
6. Staff "Last sign-in" was blank - now "Not yet".
Not fixed, and why:
1. Yield rules, Issues and Promo codes are not in the sidebar of their roles and nothing is highlighted on them. By the role menu design they sit behind the Overview links. Needs an owner decision: add a "More" entry to the sidebar, or keep.
2. The booking detail (ReservationPanel) is still a centered dialog and loses focus after Escape. Converting it to the side panel touches the calendar, arrivals and the attention panel.
3. Toast has no Undo yet, because remove and archive have no restore call in the API.
4. Disabled buttons without a reason (Assign room, Take payment, Report problem). Needs wording for each.
5. "Cancel booking" sits beside "Check in", and "Deactivate" beside "Reset sign-in". Needs a "More actions" menu component.
6. Activity: no day groups or result count, and filters are selects with chips below. Needs a grouping decision.
7. Manager rows in Activity show an IP and owner rows did not in the sample data. The API decides this by role (withWhere). Not confirmed as a leak.
8. Payments "Taken today" tile has no comparison, because the page asks the API for today only.
9. Channels spec (the sync log and channels accessibility test) failed in 2 of 3 full Playwright runs and passes alone. Cause not found.

### Round 4d: sidebar lists every screen a role may open
Finding: Yield rules, Issues, Promo codes, Lost and found and others were reachable only through Overview links. Revenue and front desk have no Overview, so for them those screens had no link at all, and nothing was marked on them.
Decision (the sidebar item from round 4c): the sidebar keeps the 5 main items, which are also the phone bottom bar, and adds a "More screens" list below them, grouped as Hotel, Prices and money, and Online and guests. It shows when the sidebar is open (desktop, and the phone menu), the marked screen scrolls into view, and it is hidden in the narrow rail. Housekeeping has no extra screens, so it shows none. Why: every screen a person may open has a visible way in, and the marked item tells them where they are.
Also fixed: the channels accessibility test failed in full runs because the page was still fading in when the contrast check ran. It now runs without animation, as the other specs do.

