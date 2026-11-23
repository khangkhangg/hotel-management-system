import { describe, expect, test } from 'vitest';
import { allowed, homeFor, isBuilt, itemsFor, milestoneFor, moreFor, type Role } from './nav.ts';

const labels = (role: Role) => itemsFor(role).map((i) => i.label);

describe('menus', () => {
	test('each role gets exactly its menu', () => {
		expect(labels('owner')).toEqual(['Overview', 'Calendar', 'Payments', 'Staff', 'Activity']);
		expect(labels('manager')).toEqual(['Overview', 'Calendar', 'Rooms', 'Staff', 'Activity']);
		expect(labels('front_desk')).toEqual(['Calendar', 'Messages', 'Arrivals', 'Guests', 'Payments']);
		expect(labels('revenue')).toEqual(['Rates', 'Rate plans', 'Channels', 'Sync log']);
		expect(labels('housekeeping')).toEqual(['Rooms', 'Issues', 'Lost and found']);
	});

	test('front desk has no Staff and owner has Activity', () => {
		expect(labels('front_desk')).not.toContain('Staff');
		expect(labels('owner')).toContain('Activity');
	});

	test('a menu fits the phone bottom bar', () => {
		for (const role of ['owner', 'manager', 'front_desk', 'revenue', 'housekeeping'] as Role[]) {
			expect(itemsFor(role).length).toBeLessThanOrEqual(5);
		}
	});

	test('every item has an icon, an address and a unique id', () => {
		const all = (['owner', 'manager', 'front_desk', 'revenue', 'housekeeping'] as Role[]).flatMap(itemsFor);
		for (const i of all) expect(i.icon && i.href.startsWith('/') && i.id).toBeTruthy();
		const byId = new Map(all.map((i) => [i.id, i.href]));
		for (const i of all) expect(byId.get(i.id)).toBe(i.href); // the same id always means the same address
	});
});

describe('allowed', () => {
	test('a role may open only the addresses in its menu', () => {
		expect(allowed('front_desk', '/staff')).toBe(false);
		expect(allowed('owner', '/staff')).toBe(true);
		expect(allowed('manager', '/activity')).toBe(true);
		expect(allowed('housekeeping', '/activity')).toBe(false);
	});

	test('sub-pages follow their parent, and lookalike prefixes do not', () => {
		expect(allowed('manager', '/staff/123')).toBe(true);
		expect(allowed('manager', '/staffing')).toBe(false);
	});

	test('the overview is for owners and managers only', () => {
		expect(allowed('owner', '/')).toBe(true);
		expect(allowed('manager', '/')).toBe(true);
		expect(allowed('front_desk', '/')).toBe(false);
	});
});

describe('more screens', () => {
	test('owners and managers reach the rest of the hotel screens from the overview, outside the five-item menu', () => {
		expect(moreFor('manager').map((i) => i.label)).toEqual(['Arrivals', 'Guests', 'Payments', 'Issues', 'Rates', 'Rate plans', 'Extras', 'Reports', 'Chain plans', 'Channels']);
		expect(moreFor('owner').map((i) => i.label)).toEqual(['All properties', 'Reports', 'Chain plans', 'Channels', 'Booking site', 'Site content', 'Branding', 'Arrivals', 'Guests', 'Rooms', 'Issues', 'Rates', 'Rate plans', 'Extras']);
		expect(allowed('manager', '/rates')).toBe(true);
		expect(allowed('owner', '/arrivals')).toBe(true);
		expect(moreFor('front_desk')).toEqual([]);
		expect(moreFor('revenue').map((i) => i.label)).toEqual(['Chain plans']);
	});

	test('a role with no extra screens is still refused the others', () => {
		expect(allowed('front_desk', '/rates')).toBe(false);
		expect(allowed('housekeeping', '/guests')).toBe(false);
		expect(allowed('revenue', '/payments')).toBe(false);
	});

	test('a screen is never in a role menu and in its more list at once', () => {
		for (const role of ['owner', 'manager'] as Role[]) {
			const menu = new Set(itemsFor(role).map((i) => i.id));
			for (const i of moreFor(role)) expect(menu.has(i.id), i.id).toBe(false);
		}
	});
});

describe('home and milestones', () => {
	test('home is the first menu item', () => {
		expect(homeFor('owner')).toBe('/');
		expect(homeFor('front_desk')).toBe('/calendar');
		expect(homeFor('housekeeping')).toBe('/rooms');
	});

	test('the screens of milestones 1 and 2 are built, the rest are not', () => {
		for (const p of ['/', '/staff', '/activity', '/calendar', '/arrivals', '/guests', '/payments', '/rates', '/rate-plans', '/rooms', '/issues', '/reports', '/extras', '/group']) expect(isBuilt(p), p).toBe(true);
		expect(isBuilt('/messages')).toBe(false);
		expect(isBuilt('/payouts')).toBe(false);
	});

	test('a screen that is not built names the milestone that builds it', () => {
		for (const href of ['/messages', '/lost-and-found', '/payouts']) {
			expect(milestoneFor(href), href).toMatch(/^Milestone \d/);
		}
	});
});
