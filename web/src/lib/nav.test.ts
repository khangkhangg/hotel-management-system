import { describe, expect, test } from 'vitest';
import { allowed, homeFor, isBuilt, itemsFor, milestoneFor, type Role } from './nav.ts';

const labels = (role: Role) => itemsFor(role).map((i) => i.label);

describe('menus', () => {
	test('each role gets exactly its menu', () => {
		expect(labels('owner')).toEqual(['Overview', 'Reports', 'Payouts', 'Staff', 'Activity']);
		expect(labels('manager')).toEqual(['Overview', 'Staff', 'Activity']);
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

describe('home and milestones', () => {
	test('home is the first menu item', () => {
		expect(homeFor('owner')).toBe('/');
		expect(homeFor('front_desk')).toBe('/calendar');
		expect(homeFor('housekeeping')).toBe('/rooms');
	});

	test('only the overview, staff and activity are built in this milestone', () => {
		expect(isBuilt('/')).toBe(true);
		expect(isBuilt('/staff')).toBe(true);
		expect(isBuilt('/activity')).toBe(true);
		expect(isBuilt('/rates')).toBe(false);
	});

	test('a screen that is not built names the milestone that builds it', () => {
		for (const href of ['/reports', '/calendar', '/rates', '/rooms', '/channels', '/lost-and-found']) {
			expect(milestoneFor(href), href).toMatch(/^Milestone \d/);
		}
	});
});
