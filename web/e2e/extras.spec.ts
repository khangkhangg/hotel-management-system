import { expect, test } from '@playwright/test';

const day = (offset: number) => new Date(Date.now() + offset * 86400000).toISOString().slice(0, 10);

test('the owner adds an extra and the front desk puts it on a stay', async ({ page }) => {
	await page.request.get('/api/test/login?email=owner@hotel.com');
	await page.goto('/extras');
	await page.getByLabel('Name').fill('Airport pickup');
	await page.getByLabel('Price').fill('25');
	await page.getByRole('button', { name: 'Add extra' }).click();
	await expect(page.getByRole('heading', { name: 'Airport pickup' })).toBeVisible();

	await page.goto('/book/main-hotel');
	await page.getByLabel('Arrival').fill(day(10));
	await page.getByLabel('Departure').fill(day(12));
	await page.getByRole('button', { name: 'Show rooms' }).click();
	await page.getByRole('button', { name: 'Select room' }).first().click();
	await page.getByRole('checkbox', { name: /Airport pickup/ }).check();
	await expect(page.getByRole('listitem').filter({ hasText: 'Airport pickup' })).toBeVisible();
	await page.getByLabel('Full name').fill('Eva Lindqvist');
	await page.getByLabel('Email').fill('eva@example.com');
	await page.getByRole('radio', { name: /Pay at the hotel/ }).check();
	await page.getByRole('button', { name: 'Confirm booking' }).click();
	await expect(page.getByRole('heading', { name: 'Your booking is confirmed' })).toBeVisible();

	await page.request.get('/api/test/login?email=mai@hotel.com');
	await page.goto('/calendar');
	await page.getByRole('button', { name: /Eva Lindqvist/ }).first().click();
	await page.getByLabel('How many').fill('2');
	await page.getByRole('button', { name: 'Add to the stay' }).click();
	await expect(page.getByText('Extra added to the stay.')).toBeVisible();
	await expect(page.getByRole('cell', { name: /Airport pickup/ })).toBeVisible();
});
