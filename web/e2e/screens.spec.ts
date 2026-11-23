import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';

async function loginAs(page: Page, email: string) {
	const res = await page.request.get(`/api/test/login?email=${encodeURIComponent(email)}`);
	expect(res.status()).toBe(204);
}

test('the attention panel offers a fix, shows what it will change, and applies it', async ({ page }) => {
	await loginAs(page, 'mai@hotel.com');
	await page.goto('/arrivals');
	const panel = page.getByRole('region', { name: 'Needs attention' });
	const card = panel.getByRole('listitem').filter({ hasText: 'Ana Reyes arrives today and has no room number.' });
	await expect(card).toBeVisible({ timeout: 8000 });
	await card.getByRole('button', { name: /Assign room/ }).click();
	const dialog = page.getByRole('dialog', { name: /Assign room/ });
	await expect(dialog.getByText(/gets room 10\d/)).toBeVisible();
	await dialog.getByRole('button', { name: 'Do it' }).click();
	await expect(card).toHaveCount(0, { timeout: 8000 });
	const row = page.getByRole('row', { name: /Ana Reyes/ });
	await expect(row).toContainText(/10\d/);
	await expect(row.getByRole('button', { name: 'Check in' })).toBeVisible();
});

test('arrivals lets the front desk check a guest in from the list', async ({ page }) => {
	await loginAs(page, 'mai@hotel.com');
	await page.goto('/arrivals');
	await expect(page.getByRole('heading', { name: 'Arrivals' })).toBeVisible();
	const row = page.getByRole('row', { name: /Ana Reyes/ });
	await expect(row).toBeVisible();
	if (await row.getByRole('button', { name: 'Check in' }).count()) {
		await row.getByRole('button', { name: 'Check in' }).click();
		await expect(page.getByRole('row', { name: /Ana Reyes/ })).toHaveCount(0);
	}
});

test('guests can be searched and opened', async ({ page }) => {
	await loginAs(page, 'mai@hotel.com');
	await page.goto('/guests');
	await page.getByLabel('Search by name, email or phone').fill('ben');
	await expect(page.getByRole('row', { name: /Ben Carter/ })).toBeVisible();
	await expect(page.getByRole('row', { name: /Ana Reyes/ })).toHaveCount(0);
	await page.getByRole('row', { name: /Ben Carter/ }).getByRole('button', { name: 'Open' }).click();
	await expect(page.getByRole('dialog', { name: 'Ben Carter' })).toContainText('ben@example.com');
});

test('revenue sees guest names but never their contact details', async ({ page }) => {
	await loginAs(page, 'rita@hotel.com');
	await page.request.get('/api/test/login?email=rita@hotel.com');
	await page.goto('/rates');
	await expect(page.getByRole('heading', { name: 'Rates' })).toBeVisible();
	const res = await page.request.get('/api/guests?q=ben', { headers: { 'X-Property-Id': (await (await page.request.get('/api/properties')).json())[0].id } });
	expect(res.status()).toBe(200);
	const body = await res.text();
	expect(body).toContain('Ben Carter');
	expect(body).not.toContain('ben@example.com');
});

test('payments show what was taken today', async ({ page }) => {
	await loginAs(page, 'mai@hotel.com');
	await page.goto('/calendar');
	await page.getByRole('region', { name: 'Bookings without a room' }).getByRole('button', { name: /Ben Carter/ }).click();
	const panel = page.getByRole('dialog').filter({ has: page.getByText('Activity and team notes') });
	await panel.getByLabel('Amount').fill('50');
	await panel.getByRole('button', { name: 'Take payment' }).click();
	await expect(panel.getByText('Payment recorded.')).toBeVisible();
	await page.goto('/payments');
	await expect(page.getByRole('row', { name: /Ben Carter/ })).toContainText('$50.00');
	await expect(page.getByText('Taken today', { exact: true })).toBeVisible();
});

test('a revenue manager sets rates and sees the grid', async ({ page }) => {
	await loginAs(page, 'daniel@hotel.com');
	await page.goto('/rates');
	// The manager sets a price for the next three days.
	await page.getByRole('button', { name: 'Set rates' }).click();
	const dialog = page.getByRole('dialog', { name: 'Set rates' });
	await dialog.getByLabel('Price per night').fill('130');
	await dialog.getByRole('button', { name: 'Save rates' }).click();
	await expect(page.getByText(/Rates saved|Saved\./)).toBeVisible();
	await expect(page.getByRole('row').nth(1)).toContainText('$130.00');
});

test('a manager sets up a room type and a room; housekeeping works a cleaning task without seeing a guest', async ({ page, browser }) => {
	await loginAs(page, 'daniel@hotel.com');
	await page.goto('/rooms');
	await page.getByRole('button', { name: 'Add room type' }).click();
	await page.getByRole('dialog', { name: 'Add room type' }).getByLabel('Room type name').fill('Family');
	await page.getByRole('dialog', { name: 'Add room type' }).getByRole('button', { name: 'Create room type' }).click();
	await page.getByRole('button', { name: 'Add room', exact: true }).click();
	const dialog = page.getByRole('dialog', { name: 'Add room' });
	await dialog.getByLabel('Room type').selectOption({ label: 'Family' });
	await dialog.getByLabel('Room', { exact: true }).fill('301');
	await dialog.getByRole('button', { name: 'Create room' }).click();
	await expect(page.getByText('301')).toBeVisible();
	// Mark it dirty, then housekeeping sees it on the board.
	const card = page.getByRole('listitem').filter({ hasText: '301' });
	await card.getByRole('button', { name: 'Dirty' }).click();
	await expect(card).toContainText('Dirty');

	const other = await browser.newContext();
	const hk = await other.newPage();
	await loginAs(hk, 'tuan@hotel.com');
	await hk.goto('/rooms');
	await expect(hk.getByRole('listitem').filter({ hasText: '301' })).toContainText('Dirty');
	await hk.getByRole('listitem').filter({ hasText: '301' }).getByRole('button', { name: 'Clean' }).click();
	await expect(hk.getByRole('listitem').filter({ hasText: '301' })).toContainText('Clean');
	const text = await hk.locator('main').innerText();
	for (const secret of ['Ana', 'Ben', 'Reyes', 'Carter', 'example.com', '$']) expect(text, secret).not.toContain(secret);
	await other.close();
});

test('maintenance tickets can be opened and closed', async ({ page }) => {
	await loginAs(page, 'tuan@hotel.com');
	await page.goto('/issues');
	await page.getByRole('button', { name: 'New ticket' }).click();
	const dialog = page.getByRole('dialog', { name: 'New ticket' });
	await dialog.getByLabel('Problem').fill('Tap drips in the bathroom');
	await dialog.getByRole('button', { name: 'Open ticket' }).click();
	await expect(page.getByText('Ticket opened.')).toBeVisible();
	const row = page.getByRole('row', { name: /Tap drips/ });
	await row.getByRole('button', { name: 'Close ticket' }).click();
	const close = page.getByRole('dialog', { name: /Close the ticket/ });
	await close.getByLabel('What was done').fill('Washer replaced');
	await close.getByRole('button', { name: 'Close ticket' }).click();
	await expect(page.getByRole('row', { name: /Tap drips/ })).toContainText('Washer replaced');
});

for (const [email, paths] of [
	['mai@hotel.com', ['/calendar', '/arrivals', '/guests', '/payments']],
	['daniel@hotel.com', ['/', '/rooms', '/rates', '/rate-plans']],
	['tuan@hotel.com', ['/rooms', '/issues']]
] as const) {
	for (const width of [390, 744, 1280]) {
		test(`${email} screens pass axe and fit at ${width}`, async ({ page }) => {
			await loginAs(page, email);
			await page.setViewportSize({ width, height: 900 });
			for (const path of paths) {
				await page.goto(path);
				await expect(page.locator('main .screen h1')).toBeVisible();
				await page.waitForTimeout(500);
				expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth), `${path} scrolls sideways`).toBe(true);
				const result = await new AxeBuilder({ page }).analyze();
				expect(result.violations.map((v) => `${v.id}: ${v.nodes[0]?.html}`), path).toEqual([]);
			}
		});
	}
}
