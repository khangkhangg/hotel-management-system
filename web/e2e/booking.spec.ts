import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';

async function loginAs(page: Page, email: string) {
	const res = await page.request.get(`/api/test/login?email=${encodeURIComponent(email)}`);
	expect(res.status()).toBe(204);
}

const panel = (page: Page) => page.getByRole('dialog').filter({ has: page.getByText('Activity and team notes') });

test('the calendar shows the rooms, the bookings and the ones that still need a room', async ({ page }) => {
	await loginAs(page, 'mai@hotel.com');
	await page.goto('/calendar');
	await expect(page.getByRole('group', { name: 'Room 101' })).toBeVisible();
	await expect(page.getByRole('group', { name: 'Room 202' })).toBeVisible();
	const tray = page.getByRole('region', { name: 'Bookings without a room' });
	await expect(tray.getByRole('button', { name: /Ana Reyes/ })).toBeVisible();
	await expect(tray.getByRole('button', { name: /Ben Carter/ })).toBeVisible();
});

test('a front desk person books a room, assigns it, checks in, takes payment and checks out', async ({ page }) => {
	await loginAs(page, 'mai@hotel.com');
	await page.goto('/calendar');
	await page.getByRole('button', { name: 'New booking', exact: true }).click();
	const dialog = page.getByRole('dialog', { name: 'New booking' });
	await dialog.getByLabel('Guest name').fill('Walk In Guest');
	await expect(dialog.getByText('Available.')).toBeVisible();
	await expect(dialog.getByText(/1 night, \$120\.00/)).toBeVisible();
	await dialog.getByRole('button', { name: 'Create booking' }).click();

	const p = panel(page);
	await expect(p.getByRole('heading', { name: /Walk In Guest/ })).toBeVisible();
	await p.getByLabel('Room', { exact: true }).selectOption({ label: '104 (clean)' });
	await p.getByRole('button', { name: 'Assign room' }).click();
	await expect(p.getByText('Room 104')).toBeVisible();
	await p.getByRole('button', { name: 'Check in' }).click();
	await expect(p.getByText('Checked in', { exact: true }).first()).toBeVisible();

	await expect(p.getByRole('row', { name: /Balance due/ })).toContainText('$120.00');
	await p.getByLabel('Amount').fill('120');
	await p.getByRole('button', { name: 'Take payment' }).click();
	await expect(p.getByRole('row', { name: /Balance due/ })).toContainText('$0.00');

	await p.getByRole('button', { name: 'Check out' }).click();
	await expect(page.getByText('Checked out. The room is marked dirty.')).toBeVisible();
	await expect(p.getByText('checked the guest out')).toBeVisible();
});

test('a booking cannot be made on a full night, and the form says so before saving', async ({ page }) => {
	await loginAs(page, 'mai@hotel.com');
	await page.goto('/calendar');
	await page.getByRole('button', { name: 'New booking', exact: true }).click();
	const dialog = page.getByRole('dialog', { name: 'New booking' });
	await dialog.getByLabel('Room type').selectOption({ label: 'Suite' });
	await dialog.getByLabel('Guest name').fill('Suite Seeker');
	await expect(dialog.getByText('Available.')).toBeVisible();
	// Two suites exist: take both, and the third is refused.
	for (const name of ['One', 'Two']) {
		await dialog.getByLabel('Guest name').fill(name);
		await dialog.getByRole('button', { name: 'Create booking' }).click();
		await page.getByRole('dialog').filter({ has: page.getByText('Activity and team notes') }).getByRole('button', { name: 'Close' }).click();
		await page.getByRole('button', { name: 'New booking', exact: true }).click();
		await dialog.getByLabel('Room type').selectOption({ label: 'Suite' });
	}
	await dialog.getByLabel('Guest name').fill('Three');
	await expect(dialog.getByText('No room is left on one of those nights.')).toBeVisible();
	await expect(dialog.getByRole('button', { name: 'Create booking' })).toBeDisabled();
});

test('a note tags a colleague, whose bell count rises, and the thread shows it live', async ({ page, browser }) => {
	await loginAs(page, 'mai@hotel.com');
	await page.goto('/calendar');
	await page.getByRole('region', { name: 'Bookings without a room' }).getByRole('button', { name: /Ben Carter/ }).click();
	const p = panel(page);
	await expect(p.getByRole('heading', { name: /Ben Carter/ })).toBeVisible();

	// The colleague has the same booking open in another browser.
	const other = await browser.newContext();
	const second = await other.newPage();
	await loginAs(second, 'daniel@hotel.com');
	await second.goto('/calendar');
	await second.getByRole('region', { name: 'Bookings without a room' }).getByRole('button', { name: /Ben Carter/ }).click();
	await expect(panel(second).getByRole('heading', { name: /Ben Carter/ })).toBeVisible();

	await p.getByLabel('Write a note').fill('Please check the deposit @Dan');
	await p.getByRole('option', { name: /Daniel Tran/ }).click();
	await p.getByRole('button', { name: 'Add note' }).click();
	await expect(p.getByText(/Please check the deposit @Daniel Tran/)).toBeVisible();

	// Without a reload, the second browser sees the note and the bell count.
	await expect(panel(second).getByText(/Please check the deposit/)).toBeVisible({ timeout: 4000 });
	await expect(second.getByRole('button', { name: /Notifications, \d+ unread/ })).toBeVisible({ timeout: 4000 });
	await panel(second).getByRole('button', { name: 'Mark done' }).click();
	await expect(panel(second).getByText(/@Daniel Tran ✓/)).toBeVisible();
	await other.close();
});

test('the calendar and the booking panel pass axe and fit at every width', async ({ page }) => {
	await loginAs(page, 'mai@hotel.com');
	for (const width of [390, 744, 1280]) {
		await page.setViewportSize({ width, height: 900 });
		await page.goto('/calendar');
		await expect(page.getByRole('group', { name: 'Room 101' })).toBeVisible();
		await page.waitForTimeout(700);
		expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth), `scrolls sideways at ${width}`).toBe(true);
		let result = await new AxeBuilder({ page }).analyze();
		expect(result.violations.map((v) => `${v.id}: ${v.nodes[0]?.html}`), `calendar at ${width}`).toEqual([]);
		await page.getByRole('region', { name: 'Bookings without a room' }).getByRole('button', { name: /Ana Reyes/ }).click();
		await expect(panel(page).getByRole('heading', { name: /Ana Reyes/ })).toBeVisible();
		await page.waitForTimeout(700);
		result = await new AxeBuilder({ page }).analyze();
		expect(result.violations.map((v) => `${v.id}: ${v.nodes[0]?.html}`), `panel at ${width}`).toEqual([]);
	}
});
