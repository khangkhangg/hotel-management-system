# Spec 02: Sign-in and security

Part of the [Hotel PMS design](../superpowers/specs/2026-11-23-hotel-pms-design.md).

## Staff sign-in

Staff sign in with two steps.

1. **First step, one of two.**
   - Email and password.
   - "Continue with Google".
2. **Second step.** A 6-digit code sent to the user's email.

The email code is asked for after Google sign-in too **(Claude's call)**. The reason is that a Google account may have no second step of its own, and the owner asked for two steps for all internal staff.

A user can tick "Trust this device for 30 days". A trusted device skips the code until the 30 days end, the user signs out everywhere, or an owner or manager resets the user's sign-in.

### Passwords

- Minimum 12 characters. No forced mix of character types and no forced rotation.
- Checked against a list of known breached passwords at set and change.
- Stored with Argon2id.
- Reset by an emailed link that is valid for 30 minutes and can be used once. A reset ends all sessions and trusted devices.

### Google sign-in

- OpenID Connect with the authorization code flow and PKCE.
- Only the `openid` and `email` scopes are requested.
- The Google account's verified email must match an invited or active user. There is no sign-up through Google.
- The Google subject ID is stored on first use, and later sign-ins match on it.
- Libraries: `coreos/go-oidc` and `golang.org/x/oauth2`.

### Email codes

- 6 digits from a secure random source.
- Valid for 10 minutes and usable once.
- Stored as a hash, never in plain text.
- 5 wrong attempts cancel the code.
- At most 5 codes per user per hour. Requests are also limited per IP address.
- The email states the time, the browser and the rough location of the attempt, and never contains a sign-in link.
- Sign-in answers the same way for a known and an unknown email, so nobody can test which emails are staff.

A code sent by email is weaker than an authenticator app or a passkey, because access to the mailbox is enough. Both are the planned upgrade and are out of scope for the first release.

### Sessions

- Sessions are stored in PostgreSQL. The browser holds only a random session ID.
- The cookie is `HttpOnly`, `Secure` and `SameSite=Lax`.
- A session ends after 12 hours without activity and after 7 days in any case.
- A new session ID is issued after the second step.
- Users see their active sessions and can end any of them.
- Requests that change data need a CSRF token.

## Guest access

Guests have no account in the first release.

- After booking, the guest gets a booking code and an emailed link.
- "Manage a booking" asks for the booking code and the email, then sends a one-time link to that email.
- The link is valid for 30 minutes and opens only that booking.
- Spec 05 lists what the guest can do there.

## Data protection

- All traffic uses HTTPS. HSTS is on.
- Card numbers never reach our servers (spec 06).
- Guest ID document scans, if stored, are encrypted at rest with a key held outside the database and are visible only to owner, manager and front desk.
- Secrets such as provider API keys are stored encrypted, and the app shows only the last 4 characters after saving.
- A strict Content-Security-Policy is set on both web surfaces.
- Uploaded files are checked for type and size, renamed, and served from a separate domain.
- Backups run daily and are encrypted. A restore is tested each month.

## Audit log

Every change to bookings, rates, payments, refunds, users, roles and settings writes one audit row.

- Fields: time, organization, property, actor user, role view active, action, target, before and after values, IP address.
- Rows are append-only. The application's database role has no update or delete right on the table.
- Owners and managers can filter the log by user, action and date.
- Passwords, codes and card data are never written to the log.

## Rate limits

| Action | Limit |
| --- | --- |
| Sign-in attempts per account | 10 per 15 minutes, then a 15-minute lock |
| Sign-in attempts per IP address | 50 per 15 minutes |
| Email codes per user | 5 per hour |
| Password reset emails per account | 3 per hour |
| Guest availability searches per IP address | 60 per minute |

## Tests this spec needs

- A correct password without the email code gives no session.
- A code fails after 10 minutes, after one use and after 5 wrong attempts.
- Google sign-in with an email that was never invited is refused.
- A trusted device skips the code, and stops skipping after a sign-in reset.
- A password reset ends every session.
- Sign-in responses for known and unknown emails match in content and in timing within a small margin.
- The audit table rejects update and delete from the application role.
