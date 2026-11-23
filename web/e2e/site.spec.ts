import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

const png = { name: 'logo.png', mimeType: 'image/png', buffer: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==', 'base64') };

// A page that is still fading in fails the contrast check, so the pages here do not animate.
test.use({ reducedMotion: 'reduce' });

test.describe.configure({ mode: 'serial' });

test('the owner brands the booking page and fills in its content, and a guest sees it', async ({ page }) => {
	await page.request.get('/api/test/login?email=owner@hotel.com');
	await page.goto('/branding');
	await page.getByRole('button', { name: 'Ocean' }).click();
	await page.getByRole('button', { name: 'Save', exact: true }).click();
	await expect(page.getByRole('status')).toHaveText('The colour is saved.');
	await page.getByLabel('Colour code').fill('#FFE680');
	await expect(page.getByText('too light')).toBeVisible();
	await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
	await page.getByRole('button', { name: 'Ocean' }).click();
	await page.getByLabel('Choose a logo').setInputFiles(png);
	await expect(page.getByRole('status')).toHaveText('The logo is saved.');
	await expect(page.getByRole('img', { name: 'Your logo' })).toBeVisible();
	expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);

	await page.goto('/site-content');
	await page.getByLabel('About the hotel').fill('A quiet hotel by the sea.');
	await page.getByRole('button', { name: 'Save', exact: true }).first().click();
	await expect(page.getByRole('status')).toHaveText('The English text is saved.');
	await page.getByLabel('Pool', { exact: true }).check();
	await page.getByLabel('Wi-Fi').check();
	await page.locator('section', { hasText: 'Amenities' }).getByRole('button', { name: 'Save', exact: true }).click();
	await expect(page.getByRole('status')).toHaveText('The amenities are saved.');
	await page.getByLabel('Caption').fill('Pool view');
	await page.getByLabel('Add a photo').setInputFiles(png);
	await expect(page.getByRole('status')).toHaveText('The photo is added.');
	await expect(page.getByRole('img', { name: 'Pool view' })).toBeVisible();
	expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);

	await page.goto('/book/main-hotel');
	await expect(page.getByText('A quiet hotel by the sea.')).toBeVisible();
	await expect(page.getByRole('figure', { name: 'Pool view' })).toBeVisible();
	await expect(page.getByRole('listitem').filter({ hasText: /^Pool$/ })).toBeVisible();
	await expect(page.getByRole('listitem').filter({ hasText: /^Wi-Fi$/ })).toBeVisible();
	await expect(page.locator('html')).toHaveAttribute('data-brand-scope', '');
	expect(await page.evaluate(() => document.documentElement.style.getPropertyValue('--brand'))).toBe('#1D5C8F');
	expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
});

test('the owner fills the other languages from English and each translation is marked as a draft', async ({ page }) => {
	await page.request.get('/api/test/login?email=owner@hotel.com');
	await page.goto('/site-content');
	await page.getByLabel('About the hotel').fill('A quiet hotel by the sea.');
	await page.getByRole('button', { name: 'Save', exact: true }).first().click();
	await expect(page.getByRole('status')).toHaveText('The English text is saved.');
	await page.getByRole('button', { name: 'Translate into the other languages' }).click();
	await expect(page.getByRole('status')).toContainText('texts translated');
	await page.getByLabel('Language').selectOption('es');
	await expect(page.getByLabel('About the hotel')).toHaveValue('[es] A quiet hotel by the sea.');
	await expect(page.getByRole('note')).toContainText('A translation service wrote some of these texts');
	expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
});
