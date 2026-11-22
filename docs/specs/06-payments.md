# Spec 06: Payments

Part of the [Hotel PMS design](../superpowers/specs/2026-11-23-hotel-pms-design.md).

## What the owner configures

Under Settings, Payments, the owner turns each payment type on or off per property and sets its rules. Only the owner role can open this screen.

| Payment type | What the guest or staff does | Owner settings |
| --- | --- | --- |
| Pay at the hotel | Nothing online. Front desk records cash or card at the desk. | On or off for the booking page. Which desk methods exist: cash, card terminal, other. |
| Bank transfer | Guest sees the hotel's bank details and a payment reference. Staff mark the transfer as received. | Bank details, how many hours the booking waits for payment before it is cancelled. |
| Card online | Guest pays on the payment provider's hosted page. | Provider account, which cards and wallets to show. |
| Local gateway | Guest pays through a local method such as a QR or e-wallet gateway. | Added per market after the first release. |

For each type the owner also sets:

- **Where it is offered:** booking page, front desk, or both.
- **How much is taken at booking:** nothing, the first night, a percent, or the full amount.
- **Which rate plans allow it.** A non-refundable rate plan can require full payment by card.

At least one payment type must stay on for the booking page.

## Card payments

- The first provider is Stripe **(Claude's call)**. The choice of the first local gateway waits for the owner's answer on country.
- Card data is entered only on the provider's hosted page or inside its embedded fields. Our servers never receive, store or log a card number.
- Each hotel connects its own provider account, so money goes straight to the hotel. We never hold the hotel's money.
- The booking is confirmed by the provider's signed webhook, not by the guest's browser returning.
- Every call to the provider carries an idempotency key, so a retry cannot charge twice.
- 3-D Secure and other bank checks are handled by the provider's page.
- Cards are not saved for later charges in the first release. A guest who chooses "Pay at the hotel" gives no card, so a no-show cannot be charged automatically. A hotel that wants protection sets a deposit. Saving a card as a guarantee is open question 5 in the design doc.
- How a hotel connects its provider account depends on open question 4. If the product is sold to other hotels, hotels connect through Stripe Connect. If it runs only for the owner's hotels, the owner enters a restricted API key.

## Provider interface

```go
type PaymentProvider interface {
    CreateCheckout(ctx context.Context, a Account, r CheckoutRequest) (Checkout, error)
    ParseWebhook(ctx context.Context, a Account, req *http.Request) (PaymentEvent, error)
    Refund(ctx context.Context, a Account, paymentRef string, amount Money, key string) (RefundResult, error)
}
```

A fake provider is built first for tests. Stripe is the first real one.

## Payment states

`pending` then `authorized` (where used) then `paid`. A payment can also end as `failed`, `cancelled`, `refunded` or `partly_refunded`.

- A booking that needs payment at booking stays `pending` and holds the room for 15 minutes for card, or the configured hours for bank transfer.
- An unpaid `pending` booking is cancelled by a job and the room is released.
- A payment that arrives after the booking was cancelled is flagged for staff and is never dropped.

## At the front desk

- Front desk records a payment on the folio: method, amount, and a note.
- A card taken on a physical terminal is recorded by hand in the first release, with the terminal's reference.
- The folio shows the balance at all times: charges, payments, amount due.
- OTA bookings show who collects: "Paid to the channel" or "Guest pays at the hotel". For OTA virtual cards, the booking shows that a virtual card exists and the date it can be charged. The card number itself is read on the OTA's own site, never stored here.

## Refunds

- A refund is tied to one payment and cannot exceed what is left of it.
- Each role has a refund limit per property (spec 01). A refund above the limit waits for a manager or owner.
- Card refunds go through the provider. Cash and bank refunds are recorded by hand.
- Every refund needs a reason and writes an audit row.

## Money rules

- Amounts are integers in the smallest currency unit, with the currency code.
- A property has one currency. Guests see prices in that currency in every language.
- Tax rules per property are percent-based, either included in the price or added on top. The price lines always show tax as its own line.
- The folio total is always the sum of its lines. It is never stored separately and edited.

## Tests this spec needs

- The webhook confirms a booking when the guest closes the browser before returning.
- The same webhook delivered twice records one payment.
- A retried checkout request does not create a second charge.
- A refund larger than the remaining amount is refused.
- A front desk refund above the limit waits for approval.
- Turning off a payment type removes it from the booking page at once.
- No card number appears in the database, logs or audit log after a full card booking.
