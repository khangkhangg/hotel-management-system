import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

test.use({ reducedMotion: 'reduce' });

test('a manager logs a lost item and marks it returned, and the front desk can only look', async ({ page }) => {
	await page.request.get('/api/test/login?email=daniel@hotel.com');
	await page.goto('/lost-and-found');
	await page.getByRole('button', { name: 'Log an item' }).click();
	await page.getByLabel('What is it').fill('Blue umbrella');
	await page.getByLabel('Where it was found').fill('Room 101');
	await page.getByRole('button', { name: 'Log item' }).click();
	await expect(page.getByRole('status')).toHaveText('The item is logged.');
	await expect(page.getByText('Blue umbrella')).toBeVisible();
	expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
	await page.getByRole('button', { name: 'Hand over' }).click();
	await page.getByLabel('Given back to').fill('Kim Park');
	await page.getByRole('button', { name: 'Mark returned' }).click();
	await expect(page.getByRole('status')).toHaveText('The item is marked returned.');

	await page.request.get('/api/test/login?email=mai@hotel.com');
	await page.goto('/lost-and-found');
	await expect(page.getByText('Blue umbrella')).toBeVisible();
	await expect(page.getByRole('button', { name: 'Log an item' })).toHaveCount(0); // the front desk looks and does not log
});

test('the owner sees messages, reviews and payouts, all empty for a quiet hotel, and they pass the accessibility check', async ({ page }) => {
	await page.request.get('/api/test/login?email=owner@hotel.com');
	for (const [path, text] of [
		['/messages', 'No guest has written yet.'],
		['/reviews', 'No guest has reviewed a stay yet.'],
		['/payouts', 'No online payments in these dates.']
	]) {
		await page.goto(path);
		await expect(page.getByText(text)).toBeVisible();
		expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
	}
});
