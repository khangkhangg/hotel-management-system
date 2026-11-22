# Spec 01: Roles and access

Part of the [Hotel PMS design](../superpowers/specs/2026-11-23-hotel-pms-design.md).

## Rules

1. Each staff member has their own account. Shared logins are not supported.
2. A user gets a role per property through a membership. One person can be front desk at hotel A and manager at hotel B.
3. The API checks permissions on every request. Hiding a menu item is never the only protection.
4. The first release has 5 fixed roles. A role is a named set of permissions in code, so custom roles can be added later without a redesign.
5. An organization always has at least one owner. The last owner cannot be removed or demoted.

## Roles

| Role | Job |
| --- | --- |
| Owner | Everything, including users, payment setup, branding and billing data. Works across all properties of the organization. |
| Manager | Runs one property. Everything except payment setup, branding and owner management. |
| Front desk | Bookings, check-in, check-out, guests, folios, taking payments, selling extras. |
| Revenue manager | Rates, availability, restrictions, channel mapping, sync log. |
| Housekeeping | Room cleaning tasks, issues, lost and found. |

## Permission matrix

| Permission | Owner | Manager | Front desk | Revenue | Housekeeping |
| --- | --- | --- | --- | --- | --- |
| View calendar and reservations | yes | yes | yes | yes | no |
| Create and change reservations | yes | yes | yes | no | no |
| Check in and check out | yes | yes | yes | no | no |
| View guest contact details | yes | yes | yes | no | no |
| Take a payment | yes | yes | yes | no | no |
| Refund up to the role limit | yes | yes | yes | no | no |
| Approve a refund above the limit | yes | yes | no | no | no |
| Edit rates and restrictions | yes | yes | no | yes | no |
| Manage channel mapping, view sync log | yes | yes | no | yes | no |
| View room cleaning status | yes | yes | yes | no | yes |
| Update cleaning tasks and issues | yes | yes | no | no | yes |
| Manage extras catalog | yes | yes | no | no | no |
| View reports | yes | yes | no | yes (revenue reports only) | no |
| Invite, edit and deactivate users | yes | yes (not owners) | no | no | no |
| Configure payment types | yes | no | no | no | no |
| Configure branding | yes | no | no | no | no |
| View audit log | yes | yes | no | no | no |

Housekeeping sees a room number, the task and the next arrival time. Housekeeping never sees guest names, prices or contact details.

Refund limits are set per property by the owner. The default is 0 for front desk, which means every front desk refund needs approval.

## Owner role switching

The owner can view the app as any of the other 4 roles.

- A "View as" control in the top bar is shown only to owners.
- Switching changes the navigation and screens to that role's version.
- The owner keeps owner permissions. The switch changes what is shown, not what is allowed.
- A banner states "Viewing as Front desk" with a button to return.
- The audit log records the real user and the role view that was active.
- The owner cannot sign in as a named staff member in the first release.

## User management

Screens live under Staff in the owner and manager menus.

1. **Invite.** Enter an email, pick a property and a role. The system emails a link that is valid for 7 days and can be used once.
2. **Accept.** The invited person sets a password or uses Google sign-in with the same email, then confirms an email code (spec 02).
3. **Edit.** Change role or property. The change applies on the user's next request.
4. **Deactivate.** The user's sessions end at once. Their history stays in the audit log. Users are never hard-deleted while they have audit records.
5. **Reset sign-in.** An owner or manager can end all sessions and trusted devices of a user.

The user list shows name, email, role, property, last sign-in and status (invited, active, deactivated).

Nobody can sign up for staff access without an invite.

## Tests this spec needs

- Each permission in the matrix, for each role, allowed and denied, at the API.
- A user with roles at two properties gets the right permissions in each.
- Row-level security blocks a query for another organization's data even when the API check is skipped.
- The last owner cannot be removed.
- Deactivating a user ends their sessions.
- Actions taken while viewing as another role are logged with the owner as the actor.
