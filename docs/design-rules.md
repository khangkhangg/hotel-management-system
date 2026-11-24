# Design rules

Every screen passes these before it is called done. They came from the owner's review on 2026-11-24. The per-screen record is in `docs/ux-audit.md`.

1. Lead with the one thing this role came to do or decide. Everything else is quiet and one click away.
2. Group anything long by purpose, with a label for each group. Never a flat wall of buttons, cards or rows. Fold repeats into ranges or counts.
3. Each person logs in to their own view. The sidebar brand shows the role as a pill. Only the owner switches view and opens every screen.
4. A list that can grow has filter and search.
5. Forms use a grid on a wide screen, with short inputs short. Header and content share one left edge. Use logical CSS properties so right-to-left works.
6. Smart defaults instead of empty or disabled starts. A disabled button says why, or does not appear. Show the effect before an action that changes money, rooms or prices.
7. Empty states say what fills the list and what to do next. Numbers say what they are compared with. Reasons are shown for every suggestion.
8. Use the shared components (Section, FormGrid, PageHeader, StatTile) and tokens. No per-page card or button CSS.
9. Words follow the `no-ai-slop` skill: headings name their subject, short plain labels, no help desk or developer words.
10. Run `ai-design-check`, and look at every changed screen at desktop width, phone width and right to left.
11. Audit each screen one role at a time: who is looking, what they are responsible for, the 1 to 3 actions they came for, and what would help them do it better.
12. Write the changes with the reason for each before implementing. A change with no reason is cosmetic and is not made.
13. Walk the personas and journeys in the running app as that person. Every journey starts at sign-in, header and sidebar.
