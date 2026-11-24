import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

// A page that is still fading in fails the contrast check, so the pages here do not animate.
test.use({ reducedMotion: 'reduce' });

test('a manager adds a yield rule, switches it off and removes it', async ({ page }) => {
	await page.request.get('/api/test/login?email=daniel@hotel.com');
	await page.goto('/yield');
	await expect(page.getByText('No rules yet')).toBeVisible();
	await page.getByRole('button', { name: 'New rule' }).click();
	await page.getByLabel('Most rooms to show').fill('2');
	await page.getByRole('button', { name: 'See the effect' }).click();
	await expect(page.getByText('This rule would change nothing right now.')).toBeVisible(); // no booking site is connected here
	await page.getByRole('button', { name: 'Add rule' }).click();
	await expect(page.getByRole('status').filter({ hasText: 'The rule is added.' })).toBeVisible();
	await expect(page.getByRole('cell', { name: 'Show at most 2 rooms', exact: true })).toBeVisible();
	expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
	await page.getByRole('button', { name: /^Switch off/ }).click();
	await expect(page.getByText('Switched off')).toBeVisible();
	await page.getByRole('button', { name: /^Remove Show at most/ }).click();
	await page.getByRole('button', { name: 'Remove rule' }).click();
	await expect(page.getByText('No rules yet')).toBeVisible();
});
