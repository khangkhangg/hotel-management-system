import { expect, test } from '@playwright/test';
import { allowed, itemsFor, moreFor, roles, type Role } from '../src/lib/nav.ts';

// Each of the five roles signs in and opens every screen. The menu it sees and the screens it may open must match the
// permission table in the app, and a screen it may not open must say so instead of showing data.
test.use({ reducedMotion: 'reduce', viewport: { width: 1280, height: 800 } });

const login: Record<Role, string> = {
	owner: 'owner@hotel.com',
	manager: 'daniel@hotel.com',
	front_desk: 'mai@hotel.com',
	housekeeping: 'tuan@hotel.com',
	revenue: 'rita@hotel.com'
};

const everyScreen = [...new Set(roles.flatMap((r) => [...itemsFor(r), ...moreFor(r)].map((i) => i.href)))].filter((p) => p !== '/').sort(); // the overview is where everyone lands, and it sends a role to its own home

for (const role of roles) {
	test(`${role} sees its own menu and opens exactly the screens it may`, async ({ page }) => {
		test.setTimeout(240_000);
		await page.request.get(`/api/test/login?email=${login[role]}`);
		await page.goto('/');
		await page.waitForLoadState('networkidle');
		const nav = page.getByRole('navigation', { name: 'Main' });
		for (const item of itemsFor(role)) await expect.soft(nav.getByRole('link', { name: item.label, exact: true }), `${role} menu: ${item.label}`).toBeVisible();
		const notInMenu = roles.flatMap((r) => itemsFor(r)).filter((i) => !itemsFor(role).some((m) => m.href === i.href));
		for (const item of notInMenu) await expect.soft(nav.getByRole('link', { name: item.label, exact: true }), `${role} menu hides: ${item.label}`).toHaveCount(0);

		for (const path of everyScreen) {
			await page.goto(path);
			await page.waitForLoadState('networkidle');
			const denied = page.getByText('You do not have access to this page.');
			if (allowed(role, path)) await expect.soft(denied, `${role} should open ${path}`).toHaveCount(0);
			else await expect.soft(denied, `${role} should be refused ${path}`).toBeVisible();
		}
	});
}
