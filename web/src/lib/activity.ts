import { roleLabels, type Role } from './nav.ts';

export type Entry = {
	id: string;
	at: string;
	actor: { id: string | null; name: string; role: string };
	viewAs: string;
	action: string;
	target: { type: string; id: string | null; label: string };
	before: Record<string, unknown> | null;
	after: Record<string, unknown> | null;
	reason: string;
	device: string;
	ip?: string;
	place?: string;
	traceId?: string | null;
};

/** Each action the trail records, as a phrase that follows the person's name. */
const phrases: Record<string, (e: Entry) => string> = {
	'user.invited': (e) => `invited ${e.target.label}`,
	'user.joined': (e) => `joined as ${e.target.label}`,
	'user.role_changed': (e) => `changed ${e.target.label}'s role`,
	'user.moved': (e) => `moved ${e.target.label} to another property`,
	'user.invite_revoked': (e) => `cancelled the invite to ${e.target.label}`,
	'user.deactivated': (e) => `deactivated ${e.target.label}`,
	'user.reactivated': (e) => `reactivated ${e.target.label}`,
	'user.signin_reset': (e) => `reset sign-in for ${e.target.label}`,
	'password.changed': () => 'changed their password',
	'signin.succeeded': () => 'signed in',
	'signin.failed': () => 'had a failed sign-in',
	signout: () => 'signed out',
	'view_as.changed': () => 'switched the view'
};

/** "alert.opened" reads as "opened alert". */
const plain = (e: Entry) => {
	const [noun, verb] = e.action.split('.');
	return [verb, noun, e.target.label].filter(Boolean).join(' ').replace(/_/g, ' ');
};

export const describeEntry = (e: Entry): string => phrases[e.action]?.(e) ?? plain(e);

/** The actions the filter offers, with the first meaning "everything". */
export const actionOptions: { id: string; label: string }[] = [
	{ id: '', label: 'All activity' },
	{ id: 'user.invited', label: 'Invites' },
	{ id: 'user.invite_revoked', label: 'Cancelled invites' },
	{ id: 'user.joined', label: 'New people joined' },
	{ id: 'user.role_changed', label: 'Role changes' },
	{ id: 'user.deactivated', label: 'Deactivations' },
	{ id: 'user.reactivated', label: 'Reactivations' },
	{ id: 'user.signin_reset', label: 'Sign-in resets' },
	{ id: 'password.changed', label: 'Password changes' },
	{ id: 'signin.succeeded', label: 'Sign-ins' },
	{ id: 'signin.failed', label: 'Failed sign-ins' },
	{ id: 'view_as.changed', label: 'View switches' }
];

const word = (field: string, value: unknown): string => {
	if (field === 'role' && typeof value === 'string' && value in roleLabels) return roleLabels[value as Role];
	return String(value);
};

/** What changed, in a few words, such as "Role: Front desk to Revenue". Empty when there is nothing to show. */
export function describeChange(e: Entry): string {
	const after = e.after ?? {};
	const before = e.before ?? {};
	for (const field of ['role', 'status']) {
		if (after[field] === undefined || after[field] === null) continue;
		const name = field === 'role' ? 'Role' : 'Status';
		// Roles read as titles ("Front desk"); a status stays lower case ("deactivated").
		const show = (v: unknown) => (field === 'status' ? String(v) : word(field, v));
		return before[field] !== undefined ? `${name}: ${show(before[field])} to ${show(after[field])}` : `${name}: ${show(after[field])}`;
	}
	return '';
}
