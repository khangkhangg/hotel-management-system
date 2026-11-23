import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

test('a manager adds a yield rule, switches it off and removes it', async ({ page }) => {
	await page.request.get('/api/test/login?email=daniel@hotel.com');
	await page.goto('/yield');
	await expect(page.getByText('No rules yet')).toBeVisible();
	await page.getByLabel('Most rooms to show').fill('2');
	await page.getByRole('button', { name: 'Add rule' }).click();
	await expect(page.getByRole('status')).toHaveText('The rule is added.');
	await expect(page.getByText('Show at most 2 rooms')).toBeVisible();
	expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
	await page.getByRole('button', { name: 'Switch off' }).click();
	await expect(page.getByText('Switched off')).toBeVisible();
	await page.getByRole('button', { name: 'Remove' }).click();
	await expect(page.getByText('No rules yet')).toBeVisible();
});
