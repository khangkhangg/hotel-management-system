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
