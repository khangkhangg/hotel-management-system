# Design system

Status: draft for review, 2026-11-24. Nothing in `web/` uses this yet. It replaces the look of `docs/design-rules.md` rule by rule where they overlap, and keeps its 13 rules.

Sources: the current `web/src/lib/tokens.css` (two layers, 4px grid, per-hotel brand), the adpanel.io token export the owner supplied, and the audit in `docs/ux-audit.md`.

## 1. How the tokens combine

The project already has two layers. Layer 1 holds raw values named by what they are. Layer 2 holds meanings named by what they do. Components read layer 2 only. `tokens.test.ts` enforces this.

The adpanel export changes layer 1 values and adds nothing to layer 2. Its names are not copied, because `--color-text-2` says nothing about use.

| Area | Decision |
| --- | --- |
| Neutrals | Take the adpanel greys: text `#101828`, body `#404040`, quiet text `#666666`, borders `#EBEBEB`, page `#FDFBFA`, cards `#FFFFFF`. Quiet `#6F6F6F` from adpanel is not used, because it drops under 4.5:1 on the sunken surface. |
| Text on any background | Never `#A1A1A1`. Quiet text is `#666666` (5.7:1 on white, 4.9:1 on the sunken surface). `tokens.test.ts` checks this. |
| Accent | Stays `--brand`, set per hotel. The adpanel palette has no brand colour, so nothing replaces it. Sign-in and the hotel list use the default brand `#1F5F4F`. |
| Status | Adopt adpanel backgrounds and borders for success, warning, error and info. Text colours stay the existing darker ones, because adpanel's own status text fails 4.5:1 on its backgrounds. Each tone has bg, fg, dot and border. Status never uses the brand. |
| Font | Keep IBM Plex Sans. It is free and covers Vietnamese and the other 8 languages. `sfProDisplay` is Apple's and not free. |
| Type scale | 12, 14, 16, 20, 28 px only. Adpanel's 36 to 74 px sizes are marketing sizes and are not used. |
| Spacing | 4px grid: 4, 8, 12, 16, 24, 32, 40. Adpanel's 2, 6, 10, 14 are off the grid and are not used. |
| Radius | Badge: full. Control (input, button, select): 12px. Card: 16px. Panel and dialog: 16px on the open side. Table rows: 0. |
| Shadow | Cards use a 1px border, not a shadow. Only things that float (panel, popover, dialog, toast) get a shadow. Use adpanel `--shadow-lg` for them. |
| Motion | 150ms for hover and focus, 200ms for panels, easing `cubic-bezier(0.4, 0, 0.2, 1)`. 1ms when reduced motion is on. |

## 2. Screen sizes

Three layouts. They match the breakpoints already in `tokens.css` (640px and 1024px) and the e2e overflow tests.

| | Mobile | Tablet (iPad mini) | Desktop |
| --- | --- | --- | --- |
| Width | under 640px | 640px to 1023px | 1024px and up |
| Test widths | 320, 375, 414 | 744 (portrait), 1133 (landscape) | 1280, 1440 |
| Navigation | Bottom bar with 4 items and a "More" sheet | Collapsed rail (68px), labels on tap and hold | Full sidebar (240px) |
| Header | Hotel name, bell, avatar. Role pill in the "More" sheet | As desktop, search collapses to an icon | Search, hotel, bell, name, role pill |
| List | Stacked rows, 2 lines each, status badge on the right | Table with the 4 most useful columns, rest in the detail | Full table, all columns, 40px rows |
| Detail | Full screen, back arrow at the top | Side panel, 480px, over the list | Side panel, 480px, list stays visible |
| Form | Full screen, one column, sticky Save bar | Side panel or centered dialog, one column | Side panel with a 2 column grid when over 640px wide |
| Filters | One "Filters" button opens a bottom sheet | Chips in a row, "More" menu | Chips in a row, "More" menu |
| Cards | 1 column | 2 columns | 3 to 4 columns, minimum 18rem |
| Controls | 44px high | 44px high | 36px compact in tables, 44px elsewhere |
| Input text | 16px, so iOS does not zoom | 16px | 14px |
| Hover | None. Everything works by tap | None. Treat as touch | Hover only adds a tint and a tooltip |

iPad mini landscape is 1133px wide, so it gets the desktop layout. The content area is then 893px, so tables must still fit there with the sidebar open.

Front desk uses a desktop and sometimes an iPad at the desk. Housekeeping uses a phone. Owners and managers use any of the three. Design the phone layout for housekeeping first, the desktop for front desk first.

## 3. Components

### List

1. Desktop and tablet use a table. Columns: one main column (name or reference) in the first position, then status, dates, numbers, and the actions menu last.
2. Numbers and money align right and use tabular figures. Text aligns to the start edge.
3. Rows are 40px (compact) or 52px (with a second line). A click or tap anywhere on the row opens the detail.
4. The header row sticks under the page header. Sort arrows show on the sortable columns only.
5. Bulk select appears only when the list has a bulk action. The bar says what will happen and to how many rows.
6. Long lists page at 25 rows, with a count ("26 to 50 of 312").
7. Mobile rows show: main text, one supporting line, status badge. Nothing else.
8. Group long lists by purpose with a label (arrivals today, tomorrow, later), never one flat wall.

### Detail

1. The header holds the name, the status badge and the one main action. Other actions go in a "More" menu.
2. Facts sit in a two column list of label and value. Labels are quiet, values are normal.
3. Related items (payments, messages, stays) go in tabs below, each with a count.
4. A timeline shows who did what and when, newest first.
5. Money facts come first when the role handles money. Front desk sees the balance due before the address.

### Form

1. A form opens in a side panel (6 fields or fewer) or a full page (long or multi-step). Never above or below the list on the same screen.
2. Labels sit above the fields. Short values (dates, counts, codes) get short fields. A two column grid starts at 640px of panel width.
3. Fields group under labelled sections: who, when, how much.
4. Smart defaults fill every field they can: today's date, the usual room type, the hotel currency.
5. Errors show beside the field, in words that say what to change. The form scrolls to the first error.
6. The Save bar sticks to the bottom. It names the action ("Save promo code"), shows saving state, and is disabled only with a reason shown.
7. A change that affects money, rooms or prices shows its effect before saving (new total, rooms affected, price per night).
8. Closing with unsaved changes asks first. Saving closes the panel and marks the new or changed row.

### Search

1. One box, top left of the list. It finds names, phone numbers and references together.
2. Results update as the person types, after 250ms. The list says what matched.
3. Search keeps the filters that are already set.
4. An empty result says what was searched and offers "Clear search".
5. Global search (header) jumps to a guest, a booking or a room. It is for finding, not for filtering.

### Filters

1. The 2 to 4 most used filters are chips in a row: status, date range, room type, staff member.
2. Other filters go in a "More" menu, so the row stays one line.
3. Active filters show as removable chips with their value. "Clear all" appears when two or more are set.
4. Date filters offer quick ranges first (today, this week, this month) and a custom range second.
5. The list header shows the result count and updates it as filters change.
6. A filter that matches nothing shows an empty state that names the filters.

### Cards

1. Use cards for things people compare or scan: rooms, rate plans, channels, stat tiles. Do not use them for records in a long list.
2. Every card has a 1px border, a 16px radius, 16px padding and no shadow.
3. A card has a title, one main fact, and at most 2 supporting lines. One action at most.
4. Sibling cards end the same way (same footer, same height).
5. Stat tiles read: label, value, what it is compared with.

### Other components

| Component | Rule |
| --- | --- |
| Button | One primary per page header. Secondary and quiet for the rest. Icon-only buttons have a name and a tooltip. |
| Status badge | Dot, short word, status colour. Same word and colour on every screen. Max 5 kinds per screen. |
| Dialog | Only for a decision (confirm a refund, cancel a stay). Not for forms. States the effect and the choices by name ("Refund 120 USD", "Keep the stay"). |
| Toast | Confirms a finished action, disappears after 5 seconds, offers Undo where the action can be undone. |
| Empty state | Says what belongs here and what to do next, with one button. |
| Loading | Skeleton rows in the shape of the content. No spinner for lists. |
| Error | Says what failed and what to do, with a retry button. Never a raw error code alone. |
| Calendar | Rooms down, days across. Colour shows stay status. Today is marked. A stay can be opened by tap. Drag is a desktop extra, never the only way. |
| Tabs | For related content of one thing. Not for switching between unrelated screens. |

## 4. Always follow

1. Lead with the one thing this role came to do or decide. Everything else is quiet and one click away.
2. Use the shared components and layer 2 tokens. A new look is a new variant of a shared component.
3. Put a label on every group. Fold repeats into ranges or counts.
4. Give every control a visible focus ring, a name, and a 44px touch target on touch screens.
5. Show the hotel's own day and time zone. Show money with the hotel's currency, and numbers in the person's locale.
6. Use logical CSS properties (`inline-start`, `block-end`) so right-to-left works.
7. Write headings that name their subject. Use sentence case. Keep labels short.
8. Say what a number is compared with.
9. Show the reason for every suggestion.
10. Give every list search and, when it can grow, filters.
11. Keep header and content on one left edge.
12. Test each changed screen at 375, 744 and 1280px, and in right-to-left.
13. Check that a label survives 40% more length (German and Finnish) without cutting or wrapping badly.

## 5. Avoid

1. Cards for long lists of records.
2. Full-width inputs for short values.
3. A form stacked above or below a list.
4. Modal dialogs that hold forms.
5. More than 3 text sizes on one screen.
6. Colour as the only way to tell states apart.
7. Icons without text for any action that is not universal (close, search, menu).
8. Tooltips that hold information needed to finish a task.
9. Hover-only controls.
10. Horizontal scroll on a page. A wide table scrolls inside its own box with a fixed first column.
11. Disabled buttons with no reason.
12. Long dropdowns for choices with 5 options or fewer. Use a segmented control or radios.
13. Auto-moving content, carousels and banners that push the page down.
14. Greeting headlines ("Welcome back") and filler words.

## 6. Must not do

1. Do not use a colour, font, size, radius or spacing value that is not a token. No raw hex, no literal `px` in components.
2. Do not use `--color-text-subtle` or any grey below 4.5:1 for text.
3. Do not derive status colours from the hotel brand.
4. Do not use emoji or sparkle icons. Use the one Font Awesome line set at one size.
5. Do not use gradients, glows, blurred blobs, side stripes or heavy shadows.
6. Do not use tracked uppercase labels or font weights above 600.
7. Do not remove a focus ring without a replacement.
8. Do not show private data to more roles than before. Guest contact details stay with the roles that see them today.
9. Do not delete or refund, or send a message to a guest, on one click with no confirmation that names the effect.
10. Do not put a destructive action next to the primary action, or give it the primary style.
11. Do not hide a failed save. The form stays open with the error shown and the data kept.
12. Do not trap focus outside a panel or dialog, or leave focus on a closed one.
13. Do not ship a screen that has not passed `tokens.test.ts`, the axe test, the overflow test and `ai-design-check`.
14. Do not write a design change with no reason (rule 12 of `design-rules.md`).

## 7. What else the system must cover

Each topic has a status. Done means tokens or tests exist in `tokens.css` and `tokens.test.ts`. Next means it is decided here and gets built with the shared components. Open means it needs the owner's decision or its own design.

| Topic | What to decide | Status |
| --- | --- | --- |
| Roles and permissions | A hidden action and a disabled action look different. Say which. A role without access sees a screen that says who to ask. | Next |
| Destructive actions and undo | Which actions offer Undo (archive, move a stay) and which need a typed confirmation (delete a hotel, bulk cancel). | Open |
| Concurrent edits | Two staff change the same stay. Show "changed by Lan 2 minutes ago" and let the second person review before saving. | Open (needs API) |
| Real time | New bookings and messages appear without a reload. Mark them, do not move rows under the cursor. | Token done: `--color-row-marked`. Behaviour next |
| Poor connection | Housekeeping works on a phone in basements and corridors. Cached lists, a clear "offline" bar in the warning status colours, queued changes. | Open |
| Print | Arrivals list, housekeeping sheet, invoice and folio need a print layout. | Open |
| Keyboard use | Front desk works fast. Shortcuts for search (`/`), new booking (`n`), close panel (`Esc`). | Next |
| Density | Compact and comfortable row sizes, chosen per person. | Token done: `--row-height`, `data-density="compact"`. Setting screen next |
| Dark mode | Not in this round, because hotel brand colours need their own checks. | Decided: no |
| Languages | 9 draft languages need native review. Right-to-left needs a pass per screen. | Token done: `--line-height-xs` is 18px for Vietnamese marks. Review open |
| Dates, money, time zones | One formatter for each. The hotel's day, not the browser's. Currency per hotel. | Already in `format.ts` and `session.today` |
| Photos and imagery | Aspect ratios, placeholders (palette photo tints exist), alt text rules. | Next |
| Data tables at scale | Behaviour with 10,000 guests: server paging, no client sort over the full set. | Next |
| Notifications | Which events alert whom, in the bell and by email, and how a person turns them off. | Open |
| Content style | A short glossary so "stay", "booking" and "reservation" mean one thing each. | Next |
| Performance | Budget per screen: first content under 2 seconds on a mid phone. | Token done: `--duration-skeleton`. Budget open |
| Toasts and loading | Toast stays 5 seconds, skeleton pulses at 1.2 seconds. | Token done: `--duration-toast`, `--duration-skeleton` |
| Testing | Contrast of every text and status pair, token rules, axe, overflow. Visual snapshots at 3 widths for shared components. | Contrast test done. Snapshots next |
| Change process | A change to a token or shared component needs a note in this file and a screenshot before and after. | Rule in place |

## 8. Next steps

1. Review this file. Change anything that does not fit how the hotels work.
2. Done 2026-11-24: `tokens.css` layer 1 values, new layer 2 names (radius, row height, status border, marked row) and their tests.
3. Build the new shared components: table, detail panel, side-panel form, search box, filter chips, card, bottom bar.
4. Re-audit every screen as each persona at the 3 widths, list the changes with reasons in `docs/ux-audit.md`, then fix one screen at a time.
