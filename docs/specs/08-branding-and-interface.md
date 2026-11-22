# Spec 08: Branding and interface

Part of the [Hotel PMS design](../superpowers/specs/2026-11-23-hotel-pms-design.md).

The mockup canvas at https://claude.ai/artifact/XLfV5xcXb79z6Wy6HymYiU shows this spec's tokens, components and motion.

## Hotel branding

The owner sets branding per property under Settings, Branding.

### Logo

- Accepted: PNG, JPG, WebP and SVG, up to 1 MB.
- SVG files are cleaned on the server: scripts, external links and embedded files are removed. A file that cannot be cleaned is refused with a message that says why.
- The owner uploads one logo and may add a second, square one for small places. Without a square logo the hotel's initials are used.
- The logo appears in the booking page header, emails, invoices and the staff app's sidebar.
- The preview shows the logo on white at header size before saving.

### Colour scheme

The owner picks one brand colour. The system builds the rest.

1. The owner chooses from 8 presets or enters any colour.
2. The system derives the hover shade, the pressed shade and a light tint in the OKLCH colour space.
3. The system checks white text on the brand colour. If contrast is under 4.5 to 1, it darkens the colour until it passes and shows both versions, so the owner sees what changed.
4. A live preview shows the booking page and a staff screen with the new colour before saving.

Where the brand colour is used:

| Surface | Use |
| --- | --- |
| Booking page | Main button, selected room outline, links, progress |
| Emails and invoices | Header line and main button |
| Staff app | Main button, current selection, highlighted chart bar |

Backgrounds, borders, text and status colours stay neutral and fixed on every surface. Status colours (success, warning, danger, info) never change with the brand, so "Paid" looks the same in every hotel.

A reset returns the property to the default colour.

## Design tokens

These values are the only ones allowed. They are defined once, in `web/src/lib/tokens.css`, as CSS custom properties. There is no CSS framework. Components style themselves with `var(--token)` in their own scoped CSS, so one file controls the whole look.

### One source of truth

The owner asked for design elements and colours to be centralised as far as possible. The rules:

1. **Two layers.** Raw values (the palette, the size scale) are named by what they are, for example `--palette-green-700`. Meaning tokens are named by what they do, for example `--color-action`, `--color-text-muted`, `--color-status-success-bg`. Components use only meaning tokens.
2. **Everything that varies is a token:** colour, font family, font size and line height, weight, spacing, radius, control height, border width, shadow, motion duration and easing, breakpoints, z-index layers, and icon size.
3. **No literal values in components.** A component file contains no hex colour, no `px` value for spacing, radius or type, no duration and no `z-index` number. A test scans every `.svelte` and `.css` file except `tokens.css` and fails on any of these.
4. **Branding is a token override.** The hotel's brand colour (this spec, above) is one value, `--brand`. The action tokens and the hover, tint and ink shades are derived from it in OKLCH, so nothing else is set by hand. Set `--brand` on the page root for the whole page, or on an element marked `data-brand-scope` to brand one part, such as a live preview. Status colours are never derived from the brand. CSS resolves `var()` where a property is declared, so the derived tokens are declared again under `[data-brand-scope]`, and a test checks that every token derived from `--brand` is.
5. **Themes later.** A dark theme or a high-contrast theme is a second set of meaning-token values under `[data-theme]`. It needs no change to any component.
6. **The mockup and the app share the tokens.** The mockup canvas used literal values per file. That was acceptable for a mockup and is the reason the token table below exists: it is the list the mockup used, now promoted to named tokens.

| Token | Values |
| --- | --- |
| Font | IBM Plex Sans, with script fonts per language (spec 05) |
| Font sizes | 12, 14, 16, 20, 28 px |
| Font weights | 400, 500, 600 |
| Spacing | 4, 8, 12, 16, 24, 32 px |
| Corner radius | 4 px for small inner parts, 8 px for controls, 12 px for cards, full for pills |
| Control height | 44 px |
| Page background | `#F4F5F3` |
| Card surface | `#FFFFFF` |
| Borders | `#E2E5E1`, and `#8B9590` for input edges |
| Text | `#17201D`, and `#56615C` for secondary text |
| Default brand colour | `#1F5F4F` |

Cards use a hairline border and no shadow. Shadows are for things that float: menus, dialogs and the segmented control's thumb.

## Shared components

Every screen is built from these. A new look is a new variant on the component, never a one-off style on a page.

| Component | Variants |
| --- | --- |
| Button | Primary, secondary, quiet. Optional icon. Full width. Disabled. |
| Field | Text, date, email, phone, time, number, with a label above |
| Select, Checkbox, Textarea | Built on Bits UI |
| Segmented control | 2 to 4 options, sliding thumb |
| Status badge | Success, warning, danger, info, neutral. A dot and a word. |
| Stat tile | Label, value, change with its period |
| Section title | Title and optional note |
| Page header | Title, note, one primary action, optional secondary action |
| Card | Hairline border, 12 px radius, 16 px padding |
| Dialog, Menu, Toast | Built on Bits UI |
| Calendar grid | Room rows, day columns, booking bars |
| Icon | Lucide icons at 20 px |
| Needs attention list | Alert rows with a level, a reason, one action and "Dismiss" |
| Hover card | The rich tooltip: title, status, up to 5 fact lines, one alert line |
| Note thread | System events and staff notes in time order, with a box to write and tag |
| Stepper | A progress bar with one segment per step. Named segments on desktop, one summary line on a phone. |
| Room gallery | Photos that advance on tap, click or swipe, with a counter and no arrow buttons |
| Booking bar | The phone bottom bar: what the price covers, the total, an expandable breakdown, the next-step button |

Links that act as navigation or actions are styled as quiet buttons, not underlined text. Underlines are kept for links inside sentences.

Rules:

- One filled primary button per page.
- A status has the same colour and word on every screen.
- Numbers in tables and tiles use tabular figures.
- Headline numbers show what they are compared with.
- Empty data shows a short message and the next step, never an empty chart.

## Screen sizes

| Size | Width | Navigation |
| --- | --- | --- |
| Phone | Under 640 px | Top bar and a bottom tab bar with up to 5 items |
| Tablet, including iPad mini | 640 to 1023 px | Icon rail 68 px wide, which the user can expand |
| Desktop | 1024 px and up | Sidebar 240 px wide, which the user can collapse |

- The same content is on every size. Nothing is hidden on a phone to make it fit.
- Wide tables scroll inside their own box. The page never scrolls sideways.
- Stat tiles sit 2 per row on a phone.
- The calendar shows 3 or 7 days on a phone and 7 or 14 days on larger screens.
- Layout uses CSS logical properties so that right-to-left languages work.
- Every page sets the mobile viewport tag (`width=device-width, initial-scale=1`). Without it a phone shows the desktop layout shrunk. This was a real fault in the first mockups.
- A phone header is one row. Anything that does not fit moves into the page and is never wrapped onto a second header row.

## Motion

| Moment | Motion | Time |
| --- | --- | --- |
| Changing screen | New content fades in and rises 8 px | 200 ms |
| Collapsing the sidebar | Width changes, labels fade | 200 ms |
| Changing the calendar range | Day columns stretch, bars scale with them | 250 ms |
| Segmented control | Thumb slides to the new option | 200 ms |
| Hover and press | Colour change only | 150 ms |
| Progress bars | Width grows | 250 ms |

- Easing is `cubic-bezier(0.2, 0, 0, 1)`. Nothing bounces or overshoots.
- Motion uses transform and opacity. Width is animated only on the sidebar, the calendar track and progress bars.
- Nothing grows on hover and nothing pulses unless it shows live activity.
- All motion is reduced to an instant change when the user's device asks for reduced motion.
- Screen changes in the real app use the View Transitions API where the browser supports it, with the fade as the fallback.

## Rich tooltips and smart interactions

A rich tooltip is a small card that appears next to an item and answers "what is this, and what state is it in" without opening a new screen. It is the main way dense screens stay quiet and still give detail on demand.

### Where they are used

| Item | What the tooltip shows |
| --- | --- |
| Booking bar on the calendar | Guest, room, stay, payment status, any open alert, and unread notes |
| Rate or restriction cell | Value, who changed it last and when, and how many channels confirmed it |
| Status badge | What the status means and what moves it to the next state |
| Disabled button | Why it is disabled and what to do: "Sold out for these dates" |
| Chart bar or point | The exact value, its date and the comparison |
| Shortened text | The full text |
| Staff name | Role, property and whether the person is on shift |
| Channel name | State, last confirmed update and updates waiting |
| Room number | Room type, cleaning status, current guest and next arrival |

### Rules

1. A tooltip opens after 300 ms of hover, or at once on keyboard focus. It closes when the pointer or focus leaves, or on Escape.
2. It never holds the only way to do something. Every action in a tooltip also exists on the item's own screen.
3. On touch screens there is no hover. A tap opens the item's detail, which shows the same facts.
4. It shows facts and at most one alert line. Forms, long text and more than 5 lines belong in the detail panel.
5. It stays inside the screen: it flips above the item when there is no room below.
6. It uses the shared hover card component, 280 px wide, with the floating shadow.
7. It is announced to screen readers through `aria-describedby`.
8. A label that is already clear gets no tooltip. Tooltips that repeat the visible text fail the "what is the value" question below.

### Other smart interactions

- **Inline edit.** Rates, minimum nights and room assignments are edited in place. A changed cell is highlighted until every channel has confirmed it.
- **Suggested actions.** Alerts carry their fix as one button (spec 09).
- **Undo.** A change shows "Undo" for 10 seconds in a toast, and for 24 hours in the history (spec 10).
- **Drag on the calendar.** Dragging a bar moves a booking to another room or changes its dates. The bar shows the new price and any conflict before the drop is accepted. The same change is available from the booking detail for keyboard and touch users.
- **Tagging.** Typing `@` in a note opens the list of colleagues (spec 11).
- **Command search.** One search box finds guests, bookings, rooms and screens, and runs actions such as "check in Khan".
- **Remembered choices.** The app remembers the calendar range, the sidebar state and the last filters per user.

## Four questions for every design area

The owner set these as rules for all design work. Each area of a screen (a card, a panel, a row of tiles, a control) must pass all four before it ships.

1. **What is the value?** Name what the user decides or does with it. An area that only displays data, and changes no decision, is removed or moved to a detail screen.
2. **Is it user friendly?** The words are plain, the main action is obvious, and a mistake can be undone.
3. **Is it taking too much space?** The space matches the value. Counts become one line of text. Lists of identical "all fine" rows collapse to the exceptions.
4. **Is it intuitive yet?** A new staff member understands it without a caption. An area that needs an explaining sentence is redesigned.

Every area should also be smart: it surfaces the problem and offers the next action (spec 09), and does not leave the user to find both.

The design review of each screen records the answers per area, and what was removed, shrunk or changed.

## Quality gate

- `npx @enmera/ai-design-check` runs in CI and must report 0 errors and 0 warnings.
- Each new screen is checked by a person at 390, 744 and 1280 px before merge.
- An accessibility check with axe runs on each screen in CI.

## Tests this spec needs

- A brand colour with poor contrast is darkened and both versions are shown.
- An SVG logo with a script is cleaned or refused.
- Status colours do not change when the brand colour changes.
- Each screen has no sideways page scroll at 320 px.
- With reduced motion on, no animation lasts longer than 1 ms.
