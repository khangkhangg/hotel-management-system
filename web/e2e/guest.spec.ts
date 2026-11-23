import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

const day = (offset: number) => new Date(Date.now() + offset * 86400000).toISOString().slice(0, 10);

async function search(page: import('@playwright/test').Page) {
	await page.goto('/book/main-hotel');
	await expect(page.getByRole('heading', { name: 'Main hotel', level: 1 })).toBeVisible();
	await page.getByLabel('Arrival').fill(day(10));
	await page.getByLabel('Departure').fill(day(12));
	await page.getByRole('button', { name: 'Show rooms' }).click();
	await page.getByRole('button', { name: 'Select room' }).first().click();
}

test('a guest books a room and pays at the hotel', async ({ page }) => {
	await search(page);
	await expect(page.getByText('2 nights').first()).toBeVisible();
	await page.getByLabel('Full name').fill('Lena Fischer');
	await page.getByLabel('Email').fill('lena@example.com');
	await page.getByRole('radio', { name: /Pay at the hotel/ }).check();
	await page.getByRole('button', { name: 'Confirm booking' }).click();
	await expect(page.getByRole('heading', { name: 'Your booking is confirmed' })).toBeVisible();
	await expect(page.getByText('Booking code')).toBeVisible();
});

test('a bank transfer booking shows the bank details and a hold', async ({ page }) => {
	await search(page);
	await page.getByLabel('Full name').fill('Omar Haddad');
	await page.getByLabel('Email').fill('omar@example.com');
	await page.getByRole('radio', { name: /Bank transfer/ }).check();
	await page.getByRole('button', { name: 'Confirm booking' }).click();
	await expect(page.getByRole('heading', { name: 'Your booking is waiting for payment' })).toBeVisible();
	await expect(page.getByText('IBAN VN00 0000')).toBeVisible();
	await expect(page.getByText(/We hold your room until/)).toBeVisible();
});

test('the guest pages have no accessibility violations, on a phone too', async ({ page }) => {
	await page.setViewportSize({ width: 375, height: 812 });
	await search(page);
	await page.waitForTimeout(700);
	const results = await new AxeBuilder({ page }).analyze();
	expect(results.violations).toEqual([]);
	const overflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth);
	expect(overflow).toBe(false);
});

test('the owner turns a payment type off and guests stop seeing it', async ({ page }) => {
	const res = await page.request.get('/api/test/login?email=owner@hotel.com');
	expect(res.status()).toBe(204);
	await page.goto('/booking-site');
	const card = page.getByRole('listitem').filter({ has: page.getByRole('heading', { name: 'Card online' }) });
	await card.getByLabel('Offer this to guests').uncheck();
	await card.getByRole('button', { name: 'Save' }).click();
	await expect(card.getByRole('status')).toHaveText('Saved');
	await page.goto('/book/main-hotel');
	await page.getByLabel('Arrival').fill(day(14));
	await page.getByLabel('Departure').fill(day(15));
	await page.getByRole('button', { name: 'Show rooms' }).click();
	await page.getByRole('button', { name: 'Select room' }).first().click();
	await expect(page.getByRole('radio', { name: /Card online/ })).toHaveCount(0);
	await expect(page.getByRole('radio', { name: /Bank transfer/ })).toBeVisible();
});
