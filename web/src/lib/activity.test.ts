import { expect, test } from 'vitest';
import { actionOptions, describeEntry, describeChange, type Entry } from './activity.ts';

const entry = (over: Partial<Entry>): Entry => ({
	id: '1',
	at: '2026-11-22T08:30:00Z',
	actor: { id: 'u1', name: 'Linh Nguyen', role: 'owner' },
	viewAs: '',
	action: 'user.invited',
	target: { type: 'invite', id: 'i1', label: 'new@hotel.com' },
	before: null,
	after: null,
	reason: '',
	device: '',
	...over
});

test('describes each action in a sentence that names the target', () => {
	expect(describeEntry(entry({}))).toBe('invited new@hotel.com');
	expect(describeEntry(entry({ action: 'user.joined', target: { type: 'user', id: 'u', label: 'Mai Pham' } }))).toBe('joined as Mai Pham');
	expect(describeEntry(entry({ action: 'user.role_changed', target: { type: 'user', id: 'u', label: 'Mai Pham' } }))).toBe("changed Mai Pham's role");
	expect(describeEntry(entry({ action: 'user.deactivated', target: { type: 'user', id: 'u', label: 'Mai Pham' } }))).toBe('deactivated Mai Pham');
	expect(describeEntry(entry({ action: 'user.invite_revoked' }))).toBe('cancelled the invite to new@hotel.com');
	expect(describeEntry(entry({ action: 'signin.succeeded' }))).toBe('signed in');
	expect(describeEntry(entry({ action: 'view_as.changed' }))).toBe('switched the view');
});

test('an action the screen does not know yet is shown with its code', () => {
	expect(describeEntry(entry({ action: 'booking.moved', target: { type: 'booking', id: 'b', label: 'Room 4' } }))).toBe('booking.moved Room 4');
});

test('a change shows what moved, in words', () => {
	expect(describeChange(entry({ before: { role: 'front_desk' }, after: { role: 'revenue' } }))).toBe('Role: Front desk to Revenue');
	expect(describeChange(entry({ before: { status: 'active' }, after: { status: 'deactivated' } }))).toBe('Status: active to deactivated');
	expect(describeChange(entry({ after: { role: 'owner', propertyId: null } }))).toBe('Role: Owner');
	expect(describeChange(entry({}))).toBe('');
});

test('the filter offers every action the screen can describe', () => {
	const ids = actionOptions.map((o) => o.id);
	expect(ids[0]).toBe('');
	expect(ids).toContain('user.invited');
	expect(new Set(ids).size).toBe(ids.length);
});
