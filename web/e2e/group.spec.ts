import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

test('the owner compares properties and the page is accessible on a phone', async ({ page }) => {
	await page.request.get('/api/test/login?email=owner@hotel.com');
	await page.setViewportSize({ width: 375, height: 812 });
	await page.goto('/group');
	await expect(page.getByRole('heading', { name: 'All properties', level: 1 })).toBeVisible();
	await expect(page.getByRole('row', { name: /Main hotel/ })).toBeVisible();
	await expect(page.getByRole('heading', { name: 'All properties, USD' })).toBeVisible();
	const results = await new AxeBuilder({ page }).analyze();
	expect(results.violations).toEqual([]);
	expect(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth)).toBe(false);
});

test('a front desk person cannot open the group view', async ({ page }) => {
	await page.request.get('/api/test/login?email=mai@hotel.com');
	const res = await page.request.get('/api/group');
	expect(res.status()).toBe(403);
});
