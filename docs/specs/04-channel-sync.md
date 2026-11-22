# Spec 04: Channel sync

Part of the [Hotel PMS design](../superpowers/specs/2026-11-23-hotel-pms-design.md).

## What sync must do

- Send availability, rates and restrictions to every connected channel.
- Receive new bookings, changes and cancellations from every channel.
- Never lose an update, never apply one twice, and never send them in the wrong order.

## What "channel" means here

In the first release there is one connection per property, to the channel manager. The channel manager holds the links to each OTA, and the hotel maps OTA rooms and rates in the channel manager's own screens **(Claude's call)**.

- Our mapping screen maps our room types and rate plans to the channel manager's rooms and rates. It is one mapping, not one per OTA.
- The app lists each OTA with its state only where the channel manager's API reports it.
- "Confirmed" means the channel manager accepted the update. Whether it also reports each OTA's confirmation must be checked with the provider.
- When a direct OTA adapter is added later, that OTA becomes its own connection with its own mapping.

## Targets

| Measure | Target |
| --- | --- |
| Time from a booking to the update leaving our server | Under 2 seconds for 95 of 100 updates |
| Time from a booking to the channel confirming it | Under 10 seconds for 95 of 100, where the provider allows |
| Lost updates | 0 |
| Differences found by the nightly compare | 0. Any difference raises an alert |

## Outgoing updates

1. A change to inventory or rates writes an `outbox` row in the same transaction.
2. A River worker picks up the row and creates one `sync_attempt` per connected channel.
3. Each attempt sends the current value for that room type and date range, not the difference. Sending the current value makes a repeat harmless.
4. Updates for the same property and channel run one at a time, in order. Updates for different properties run in parallel.
5. Several waiting updates for the same room type and dates are merged into one before sending.
6. Each request carries the `dedupe_key`, so a repeat is ignored by providers that support it.
7. A failure is retried with growing waits: 5 s, 20 s, 1 min, 5 min, then every 15 min for 24 hours.
8. After 3 failures the connection shows "Retrying" in the app. After 30 minutes of failure the owner and revenue manager get an alert.
9. An update is "confirmed" only when the provider says so. "Sent" and "confirmed" are stored as separate times.

## Incoming bookings

1. The provider calls our webhook. Where a provider has no webhook, a worker polls it.
2. The request signature is checked before anything else.
3. The raw event is stored in `inbound_event`. Its external ID is unique, so a repeat is dropped.
4. A worker turns the event into a reservation inside one transaction: guest, reservation, inventory change and outbox row.
5. If the room type is full when a channel booking arrives, the booking is still stored, marked "Overbooked", counted in `inventory.oversold` and not in `sold` (spec 03), and the owner, manager and front desk are alerted at once. A channel booking is a promise the hotel already made, so it is never rejected silently.
6. Changes and cancellations update the same reservation, matched on the channel's booking reference.

## Nightly full compare

Every night at 02:00 property time, a job reads availability and rates from each connection for the next 365 days and compares them with our database.

With a channel manager, this compares against the channel manager's copy. It proves that our updates arrived there. It does not prove what each OTA shows, unless the provider's API returns that. This limit must be confirmed with the provider and stated in the app.

- A difference is fixed by sending our value again.
- Every difference is logged with the channel, date and both values.
- The owner overview shows the count from the last run.

## Channel adapter interface

The sync engine talks to channels through one Go interface, so a provider can be replaced.

```go
type ChannelAdapter interface {
    PushAvailability(ctx context.Context, c Connection, u AvailabilityUpdate) (Receipt, error)
    PushRates(ctx context.Context, c Connection, u RateUpdate) (Receipt, error)
    FetchState(ctx context.Context, c Connection, r DateRange) (ChannelState, error)
    ParseWebhook(ctx context.Context, c Connection, req *http.Request) ([]InboundEvent, error)
    ListRoomsAndRates(ctx context.Context, c Connection) (ExternalCatalog, error)
}
```

The first adapter is a channel manager **(Claude's call)**. Channex is the first candidate. Its API, pricing and rate limits were not checked and must be confirmed before this milestone starts.

A fake adapter that runs in memory is built first. It lets the sync engine be tested without a provider account.

## Screens

- **Channels.** Each connection with its state (in sync, retrying, disconnected), the last confirmed update and the number waiting.
- **Mapping.** Match each of our room types and rate plans to the channel's rooms and rates. Unmapped items are listed first.
- **Sync log.** Every update with its cause, the channels it went to, and the time each one confirmed. Filter by channel, room type and date.
- **Compare now.** Runs the full compare on demand.

## Rate rules

- One rate plan can be linked to a connection with a markup or markdown in percent, so a price change is entered once. A different markup per OTA is set in the channel manager.
- Stop sell, minimum nights, closed to arrival and closed to departure are sent with rates.
- The hotel's own booking page reads the database directly, so it is never out of date.

## Tests this spec needs

- A crash between saving a booking and sending the update does not lose the update.
- The same outbox row processed twice sends the same value and changes nothing.
- Two updates for the same dates arrive at the channel in the order they were made.
- A webhook delivered 3 times creates one reservation.
- A webhook with a bad signature is refused and nothing is stored.
- The compare job finds and repairs a value that was changed on the fake channel.
- An incoming booking for a full room type is stored and flagged, and alerts are sent.
