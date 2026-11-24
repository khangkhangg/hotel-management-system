import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

// A page that is still fading in fails the contrast check, so the pages here do not animate.
test.use({ reducedMotion: 'reduce' });

test('a manager adds, sees and removes a promo code', async ({ page }) => {
	await page.request.get('/api/test/login?email=daniel@hotel.com');
	await page.goto('/promo-codes');
	await expect(page.getByText('No codes yet.')).toBeVisible();
	await page.getByRole('button', { name: 'New code' }).click();
	await page.getByLabel('Code', { exact: true }).fill('E2E10');
	await page.getByLabel('Percent off (1 to 100)').fill('10');
	await page.getByRole('button', { name: 'Add code' }).click();
	await expect(page.getByRole('status')).toHaveText('The code is added.');
	await expect(page.locator('tr[data-marked="true"]')).toContainText('E2E10');
	await expect(page.getByRole('cell', { name: '10 percent off' })).toBeVisible();
	expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
	await page.getByRole('button', { name: 'Remove', exact: true }).click();
	await page.getByRole('button', { name: 'Remove code' }).click();
	await expect(page.getByText('No codes yet.')).toBeVisible();
});
