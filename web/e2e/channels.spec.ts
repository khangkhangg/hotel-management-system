import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

test('a manager chooses how to connect and matches a rate plan', async ({ page }) => {
	await page.request.get('/api/test/login?email=daniel@hotel.com');
	await page.goto('/channels');
	await expect(page.getByRole('heading', { name: 'Channels', level: 1 })).toBeVisible();
	await page.getByRole('button', { name: 'Straight to the booking sites' }).click();
	await page.getByLabel('Connect to', { exact: true }).selectOption({ label: 'Agoda' });
	await page.getByLabel('Your hotel code on their side').fill('AG-1');
	await page.getByLabel('Address to connect to').fill('https://ota.example.com/connect');
	await page.getByRole('button', { name: 'Save', exact: true }).first().click();
	await expect(page.getByRole('status')).toHaveText('The connection is saved.');
	await page.getByLabel('Their room code').first().fill('ROOM-9');
	await page.getByRole('button', { name: 'Save', exact: true }).nth(1).click();
	await expect(page.getByRole('status')).toContainText('is matched.');
	await page.reload();
	await expect(page.getByLabel('Your hotel code on their side')).toHaveValue('AG-1');
});

test('an address that points at the server itself is refused', async ({ page }) => {
	await page.request.get('/api/test/login?email=daniel@hotel.com');
	await page.goto('/channels');
	await page.getByLabel('Address to connect to').fill('https://127.0.0.1/x');
	await page.getByRole('button', { name: 'Save', exact: true }).first().click();
	await expect(page.getByRole('alert')).toBeVisible();
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
