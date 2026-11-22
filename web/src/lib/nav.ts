import type { IconName } from './ui/Icon.svelte';

export type Role = 'owner' | 'manager' | 'front_desk' | 'revenue' | 'housekeeping';
export type NavItem = { id: string; label: string; icon: IconName; href: string };

export const roleLabels: Record<Role, string> = {
	owner: 'Owner',
	manager: 'Manager',
	front_desk: 'Front desk',
	revenue: 'Revenue',
	housekeeping: 'Housekeeping'
};
export const roles = Object.keys(roleLabels) as Role[];

const item = (id: string, label: string, icon: IconName, href: string): NavItem => ({ id, label, icon, href });

const all = {
	overview: item('overview', 'Overview', 'grid', '/'),
	reports: item('reports', 'Reports', 'chart', '/reports'),
	payouts: item('payouts', 'Payouts', 'card', '/payouts'),
	staff: item('staff', 'Staff', 'users', '/staff'),
	activity: item('activity', 'Activity', 'activity', '/activity'),
	calendar: item('calendar', 'Calendar', 'calendar', '/calendar'),
	messages: item('messages', 'Messages', 'chat', '/messages'),
	arrivals: item('arrivals', 'Arrivals', 'arrivals', '/arrivals'),
	guests: item('guests', 'Guests', 'guests', '/guests'),
	payments: item('payments', 'Payments', 'card', '/payments'),
	rates: item('rates', 'Rates', 'tag', '/rates'),
	ratePlans: item('rate-plans', 'Rate plans', 'list', '/rate-plans'),
	channels: item('channels', 'Channels', 'link', '/channels'),
	syncLog: item('sync-log', 'Sync log', 'file', '/sync-log'),
	rooms: item('rooms', 'Rooms', 'bed', '/rooms'),
	issues: item('issues', 'Issues', 'alert', '/issues'),
	lost: item('lost-and-found', 'Lost and found', 'box', '/lost-and-found')
};

const menus: Record<Role, NavItem[]> = {
	owner: [all.overview, all.reports, all.payouts, all.staff, all.activity],
	manager: [all.overview, all.staff, all.activity],
	front_desk: [all.calendar, all.messages, all.arrivals, all.guests, all.payments],
	revenue: [all.rates, all.ratePlans, all.channels, all.syncLog],
	housekeeping: [all.rooms, all.issues, all.lost]
};

/** The menu for a role, in order. At most 5 items, so it fits the phone's bottom bar. */
export const itemsFor = (role: Role): NavItem[] => menus[role];

/** The screen a role lands on: the first item of its menu. */
export const homeFor = (role: Role): string => menus[role][0].href;

const matches = (href: string, path: string) => (href === '/' ? path === '/' : path === href || path.startsWith(href + '/'));

/** Whether a role's menu leads to the path (or to a page under it). */
export const allowed = (role: Role, path: string): boolean => menus[role].some((i) => matches(i.href, path));

/** Screens that exist in this milestone. The rest open a placeholder. */
const built = ['/', '/staff', '/activity'];
export const isBuilt = (path: string): boolean => built.some((href) => matches(href, path));

const milestones: [string[], string][] = [
	[['/calendar', '/arrivals', '/guests', '/payments', '/rates', '/rate-plans', '/rooms', '/issues', '/lost-and-found'], 'Milestone 2: Core PMS'],
	[['/messages'], 'Milestone 3: Guest booking'],
	[['/channels', '/sync-log'], 'Milestone 4: Channel sync'],
	[['/reports', '/payouts'], 'Milestone 6: Reports and chains']
];

/** The milestone that builds a screen, for its placeholder. */
export const milestoneFor = (path: string): string => milestones.find(([hrefs]) => hrefs.some((h) => matches(h, path)))?.[1] ?? 'a later milestone';
