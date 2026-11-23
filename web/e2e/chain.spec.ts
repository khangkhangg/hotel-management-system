import { expect, test } from '@playwright/test';

test('the owner sets a chain rate plan and a property adds it to a room type', async ({ page }) => {
	await page.request.get('/api/test/login?email=owner@hotel.com');
	await page.goto('/chain-plans');
	await page.getByLabel('Name').fill('Chain flexible');
	await page.getByLabel('Free cancellation until (days before arrival)').fill('3');
	await page.getByRole('button', { name: 'Add rate plan' }).click();
	await expect(page.getByRole('heading', { name: 'Chain flexible' })).toBeVisible();
	await page.getByRole('button', { name: 'Add to this property' }).click();
	await expect(page.getByRole('status')).toHaveText('Chain flexible is now a rate plan at this property.');
});

test('a manager sees the chain plans but cannot set one', async ({ page }) => {
	await page.request.get('/api/test/login?email=daniel@hotel.com');
	await page.goto('/chain-plans');
	await expect(page.getByRole('heading', { name: 'Chain flexible' })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Add rate plan' })).toHaveCount(0);
});
