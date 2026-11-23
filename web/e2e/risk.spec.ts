import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

test('the review queue is empty for a quiet hotel and passes the accessibility check', async ({ page }) => {
	await page.request.get('/api/test/login?email=daniel@hotel.com');
	await page.goto('/risk');
	await expect(page.getByText('Nothing is waiting for review.')).toBeVisible();
	expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
});
