import { expect, test } from '@playwright/test';
import { readFile } from 'node:fs/promises';

test('the owner downloads the reservations of a period as a CSV file', async ({ page }) => {
	await page.request.get('/api/test/login?email=owner@hotel.com');
	await page.goto('/reports');
	const [file] = await Promise.all([page.waitForEvent('download'), page.getByRole('button', { name: 'Download CSV' }).click()]);
	expect(file.suggestedFilename()).toMatch(/^reservations-\d{4}-\d{2}-\d{2}-to-\d{4}-\d{2}-\d{2}\.csv$/);
	const text = await readFile((await file.path()) as string, 'utf8');
	expect(text.split('\n')[0]).toBe('code,status,guest,arrival,departure,rooms,total,paid,currency');
	await expect(page.getByRole('status')).toHaveText('The file is downloaded.');
});
