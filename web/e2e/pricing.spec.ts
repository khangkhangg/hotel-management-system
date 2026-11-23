import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

// A page that is still fading in fails the contrast check, so the pages here do not animate.
test.use({ reducedMotion: 'reduce' });

test('a manager reads price suggestions with their reasons, sets limits and keeps competitor prices', async ({ page }) => {
	await page.request.get('/api/test/login?email=daniel@hotel.com');
	await page.goto('/pricing');
	await expect(page.getByRole('heading', { name: 'Suggestions for the next 30 days' })).toBeVisible();
	await expect(page.getByText(/Occupancy is \d+ percent/).first()).toBeVisible();
	await expect(page.getByRole('button', { name: 'Apply the checked prices' })).toBeVisible();
	await page.getByLabel('Night', { exact: true }).fill('2099-01-01');
	await page.getByLabel('Hotel name').fill('Sea Hotel');
	await page.getByLabel(/^Price \(/).fill('120');
	await page.getByRole('button', { name: 'Save price' }).click();
	await expect(page.getByRole('status')).toHaveText('The price is saved.');
	expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
});

test('the yield and review screens pass the accessibility check', async ({ page }) => {
	await page.request.get('/api/test/login?email=daniel@hotel.com');
	for (const path of ['/yield', '/risk']) {
		await page.goto(path);
		await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
		expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
	}
	await page.goto('/risk');
	await expect(page.getByText('Nobody is waiting.')).toBeVisible();
	await expect(page.getByText('No night needs a hint.')).toBeVisible();
});

test('a guest who opens a bad reconfirm link is told it is not valid', async ({ page }) => {
	await page.goto('/reconfirm/00000000-0000-0000-0000-000000000000/' + '0'.repeat(64));
	await page.getByRole('button', { name: 'Yes, I am coming' }).click();
	await expect(page.getByRole('alert')).toHaveText('This link is not valid any more.');
});
