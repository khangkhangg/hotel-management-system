# Spec 07: Extras

Part of the [Hotel PMS design](../superpowers/specs/2026-11-23-hotel-pms-design.md).

"Extras" is the menu name for everything a hotel sells besides the room: add-on services, tours and products.

## Three kinds

| Kind | Examples | What makes it different |
| --- | --- | --- |
| Service | Breakfast, airport pickup, late checkout, extra bed | Tied to the stay. May be priced per night or per person. |
| Tour | City walk, boat trip, cooking class | Has departure dates and times, and a limited number of places. |
| Product | Minibar items, souvenirs, toiletries | Sold by the piece. May have a stock count. |

All three are rows in `catalog_item` with a `kind`, so they share one catalog screen, one way of pricing and one way of posting to the folio.

## Catalog item fields

- Name and description, in the default language and each translation (spec 05).
- Kind: service, tour or product.
- Price and tax rule.
- Pricing unit, one of: per stay, per night, per person, per person per night, per item.
- Photo, optional.
- Where it is sold: booking page, front desk, or both.
- Active or hidden.
- For services: an optional limit per day, for example 4 airport pickups.
- For tours: duration, meeting point, minimum and maximum group size, and departures.
- For products: an optional stock count. When stock is tracked, a sale lowers it and the item hides from the booking page at 0.

## Tours

- A tour has departures, each with a start time and a capacity.
- Departures can be added one by one or by a weekly pattern, for example every Tuesday and Friday at 09:00.
- Booking a tour raises `tour_departure.booked` in the same transaction as the folio line. The `CHECK (booked <= capacity)` constraint stops overselling.
- The booking page shows only departures that fall inside the guest's stay and have enough places left.
- A tour run by an outside operator stores the operator's name and the amount owed to them, for the hotel's own records. Paying the operator is outside this system.
- Cancelling a tour booking frees the places.

## Where extras are sold

### On the booking page

- Step 2 of the booking flow lists the extras available for the guest's dates.
- Each item shows its price for this stay, worked out from the pricing unit. Breakfast at a price per person per night for 2 guests and 2 nights shows the total for 4 breakfasts.
- Selected extras appear as lines in the stay summary and in the total.

### At the front desk

- From a reservation, staff choose "Add extra", pick an item and a quantity, and the line is posted to the folio.
- A product sale to someone who is not a guest is a walk-in sale: a folio with no reservation, paid at once.
- Staff can change the quantity or remove a line until the folio is closed. Each change writes an audit row.

## Pricing rules

- The price of an extra is copied onto the folio line at the time of sale. A later price change does not alter past sales.
- Tax follows the item's tax rule.
- Extras are not sent to OTAs in the first release.

## Reports

- Extras revenue by item and by kind, for a date range.
- Tour list for a day: departures, guests booked, places left.
- Low stock list for products with tracked stock.

## Permissions

Owner and manager manage the catalog. Front desk sells extras. Other roles have no access (spec 01).

## Tests this spec needs

- Each pricing unit gives the right total for 2 guests and 3 nights.
- Two bookings for the last place on a tour: exactly one succeeds.
- A product with stock 1 sold twice at the same moment: one sale succeeds.
- A tour outside the guest's stay dates is not offered.
- Changing an item's price leaves existing folio lines unchanged.
- An item with no translation shows the default language text.
