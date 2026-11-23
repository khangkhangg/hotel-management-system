import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

test('the setup recommends Channex first and a manager connects through the steps', async ({ page }) => {
	await page.request.get('/api/test/login?email=daniel@hotel.com');
	await page.goto('/channels');
	await expect(page.getByRole('heading', { name: 'Channels', level: 1 })).toBeVisible();
	await expect(page.getByRole('radio').first()).toBeChecked();
	await expect(page.getByText('Recommended')).toBeVisible();
	await page.getByLabel('Your hotel code on their side').fill('PROP-1');
	await page.getByLabel('Name of the server setting that holds the password or key').fill('CHANNEX_KEY');
	await page.getByRole('button', { name: 'Save', exact: true }).first().click();
	await expect(page.getByRole('status')).toHaveText('The connection is saved.');
	await page.getByRole('button', { name: 'Test the connection' }).click();
	await expect(page.getByText('The connection did not work')).toBeVisible(); // the server has no key under that name
	await page.getByRole('button', { name: 'Find matches' }).click();
	await expect(page.getByRole('alert')).toBeVisible(); // nothing to list without a working connection
});

test('a direct OTA is a choice too, and an address that points at the server itself is refused', async ({ page }) => {
	await page.request.get('/api/test/login?email=daniel@hotel.com');
	await page.goto('/channels');
	await page.getByRole('radio', { name: /^Agoda/ }).check();
	await page.getByLabel('Address to connect to').fill('https://127.0.0.1/x');
	await page.getByRole('button', { name: 'Save', exact: true }).first().click();
	await expect(page.getByRole('alert')).toBeVisible();
	await page.getByLabel('Address to connect to').fill('https://ota.example.com/connect');
	await page.getByRole('button', { name: 'Save', exact: true }).first().click();
	await expect(page.getByRole('status')).toHaveText('The connection is saved.');
});

test('Google is a choice and says no bookings come back', async ({ page }) => {
	await page.request.get('/api/test/login?email=daniel@hotel.com');
	await page.goto('/channels');
	await page.getByRole('radio', { name: /^Google Hotels/ }).check();
	await expect(page.getByRole('note').filter({ hasText: 'Google shows your prices' })).toBeVisible();
	await page.getByRole('radio', { name: /^Expedia/ }).check();
	await expect(page.getByText('does not take direct connections from a single hotel')).toBeVisible();
});

test('the sync log and the channels page are accessible', async ({ page }) => {
	await page.request.get('/api/test/login?email=daniel@hotel.com');
	for (const path of ['/channels', '/sync-log']) {
		await page.goto(path);
		await page.waitForTimeout(500);
		const results = await new AxeBuilder({ page }).analyze();
		expect(results.violations, path).toEqual([]);
	}
});

test('picking a country brings that market\u2019s channel managers forward, and the ones that need approval show their steps', async ({ page }) => {
	await page.request.get('/api/test/login?email=daniel@hotel.com');
	await page.goto('/channels');
	await expect(page.getByRole('radio', { name: /^Beds24/ })).toBeVisible();
	await page.getByLabel('Where is your hotel?').selectOption('JP');
	await expect(page.getByText('TL-Lincoln', { exact: true })).toBeVisible();
	await expect(page.getByText('Neppan', { exact: true })).toBeVisible();
	await expect(page.getByText('STAAH', { exact: true })).toHaveCount(0);
	await expect(page.getByRole('radio', { name: /^Channex/ })).toBeVisible();
	await page.getByText('TL-Lincoln', { exact: true }).click();
	await expect(page.getByText(/applies\. Until TL-Lincoln approves/)).toBeVisible();
	await page.getByLabel('Where is your hotel?').selectOption('VN');
	await expect(page.getByText('STAAH', { exact: true })).toBeVisible();
});
