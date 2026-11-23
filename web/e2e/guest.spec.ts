import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

const day = (offset: number) => new Date(Date.now() + offset * 86400000).toISOString().slice(0, 10);

async function search(page: import('@playwright/test').Page) {
	await page.goto('/book/main-hotel');
	await page.waitForLoadState('networkidle'); // the page is drawn on the server first, so wait until it is live before typing
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
	await page.waitForLoadState('networkidle'); // the page is drawn on the server first, so wait until it is live before typing
	await page.getByLabel('Arrival').fill(day(14));
	await page.getByLabel('Departure').fill(day(15));
	await page.getByRole('button', { name: 'Show rooms' }).click();
	await page.getByRole('button', { name: 'Select room' }).first().click();
	await expect(page.getByRole('radio', { name: /Card online/ })).toHaveCount(0);
	await expect(page.getByRole('radio', { name: /Bank transfer/ })).toBeVisible();
});

test('front desk confirms a bank transfer and the booking is kept', async ({ page }) => {
	await search(page);
	await page.getByLabel('Full name').fill('Ines Costa');
	await page.getByLabel('Email').fill('ines@example.com');
	await page.getByRole('radio', { name: /Bank transfer/ }).check();
	await page.getByRole('button', { name: 'Confirm booking' }).click();
	await expect(page.getByRole('heading', { name: 'Your booking is waiting for payment' })).toBeVisible();
	const code = (await page.getByText('Booking code').locator('strong').textContent()) ?? '';
	const res = await page.request.get('/api/test/login?email=mai@hotel.com');
	expect(res.status()).toBe(204);
	await page.goto('/calendar');
	await page.getByRole('button', { name: new RegExp('Ines Costa') }).first().click();
	await page.getByRole('button', { name: 'Mark payment received' }).click();
	await expect(page.getByText('Payment recorded and booking confirmed.')).toBeVisible();
	expect(code.length).toBeGreaterThan(5);
});

test('a guest opens their booking from the emailed link and cancels it', async ({ page }) => {
	await search(page);
	await page.getByLabel('Full name').fill('Tomas Novak');
	await page.getByLabel('Email').fill('tomas@example.com');
	await page.getByRole('radio', { name: /Pay at the hotel/ }).check();
	await page.getByRole('button', { name: 'Confirm booking' }).click();
	const code = (await page.getByText('Booking code').locator('strong').textContent()) ?? '';
	await page.goto('/book/main-hotel/manage');
	await page.getByLabel('Booking code').fill(code);
	await page.getByLabel('Email').fill('tomas@example.com');
	await page.getByRole('button', { name: 'Send me the link' }).click();
	await expect(page.getByText(/we have sent a link/)).toBeVisible();
	const link = await page.request.get('/api/test/last-link?email=tomas@example.com');
	await page.goto(await link.text());
	await expect(page.getByText(code)).toBeVisible();
	await page.getByRole('button', { name: 'Cancel booking' }).click();
	await expect(page.getByText('Nothing was paid online')).toBeVisible();
	await page.getByRole('button', { name: 'Yes, cancel my booking' }).click();
	await expect(page.getByRole('status')).toHaveText('Your booking is cancelled.');
	const results = await new AxeBuilder({ page }).analyze();
	expect(results.violations).toEqual([]);
});
