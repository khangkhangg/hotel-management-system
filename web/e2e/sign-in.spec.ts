import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';

const password = 'twelve chars ok';

const lastCode = async (page: Page, email: string) =>
	(await (await page.request.get(`/api/test/last-code?email=${encodeURIComponent(email)}`)).text()).trim();

async function signInWithPassword(page: Page, email: string, pw = password) {
	await page.goto('/sign-in');
	await page.getByLabel('Email').fill(email);
	await page.getByLabel('Password').fill(pw);
	await page.getByRole('button', { name: 'Sign in' }).click();
}

test('sign in with password and code', async ({ page }) => {
	await signInWithPassword(page, 'mai@hotel.com');
	await expect(page.getByRole('heading', { name: 'Enter the code we emailed you' })).toBeVisible();
	await expect(page.getByText('mai@hotel.com')).toBeVisible();
	await page.getByLabel('Code').fill(await lastCode(page, 'mai@hotel.com'));
	await page.getByRole('button', { name: 'Continue' }).click();
	await expect(page).toHaveURL('/');
});

test('wrong password shows the message and keeps the email', async ({ page }) => {
	await signInWithPassword(page, 'mai@hotel.com', 'not the password');
	await expect(page.getByRole('alert')).toHaveText('The email or password is not right.');
	await expect(page.getByLabel('Email')).toHaveValue('mai@hotel.com');
});

test('wrong code shows the message', async ({ page }) => {
	await signInWithPassword(page, 'daniel@hotel.com');
	await page.getByLabel('Code').fill('000000');
	await page.getByRole('button', { name: 'Continue' }).click();
	await expect(page.getByRole('alert')).toHaveText('That code is not right. Check the email and try again.');
});

test('the code step offers a new code', async ({ page }) => {
	await signInWithPassword(page, 'daniel@hotel.com');
	await page.getByRole('button', { name: 'Send a new code' }).click();
	await expect(page.getByRole('status')).toHaveText('We sent a new code.');
	await page.getByLabel('Code').fill(await lastCode(page, 'daniel@hotel.com'));
	await page.getByRole('button', { name: 'Continue' }).click();
	await expect(page).toHaveURL('/');
});

test('invite link sets a password then asks for the code', async ({ page }) => {
	const invite = await (await page.request.get('/api/test/owner-invite?email=new@hotel.com')).json();
	await page.goto(invite.path);
	await expect(page.getByRole('heading', { name: 'Join Sample Hotel' })).toBeVisible();
	await page.getByLabel('Your name').fill('New Owner');
	await page.getByLabel('Choose a password').fill(password);
	await page.getByRole('button', { name: 'Create account' }).click();
	await expect(page.getByRole('heading', { name: 'Enter the code we emailed you' })).toBeVisible();
	await page.getByLabel('Code').fill(await lastCode(page, 'new@hotel.com'));
	await page.getByRole('button', { name: 'Continue' }).click();
	await expect(page).toHaveURL('/');
});

test('expired invite says so and offers no form', async ({ page }) => {
	await page.goto('/invite/not-a-real-token');
	await expect(page.getByText('This invite has expired')).toBeVisible();
	await expect(page.getByText('Ask your manager for a new one.')).toBeVisible();
	await expect(page.getByRole('button')).toHaveCount(0);
});

test('a forgotten password can be reset from the emailed link', async ({ page }) => {
	await page.goto('/reset');
	await page.getByLabel('Email').fill('lan@hotel.com');
	await page.getByRole('button', { name: 'Send the link' }).click();
	await expect(page.getByRole('status')).toContainText('a link is on its way');
	const link = (await (await page.request.get('/api/test/last-link?email=lan@hotel.com')).text()).trim();
	await page.goto(link);
	await page.getByLabel('New password').fill('a brand new passphrase');
	await page.getByRole('button', { name: 'Save password' }).click();
	await expect(page.getByRole('status')).toHaveText('Your password is changed. Sign in with it.');
	await signInWithPassword(page, 'lan@hotel.com', 'a brand new passphrase');
	await expect(page.getByRole('heading', { name: 'Enter the code we emailed you' })).toBeVisible();
});

test('a used reset link says it has expired', async ({ page }) => {
	await page.goto('/reset/not-a-real-token');
	await page.getByLabel('New password').fill('a brand new passphrase');
	await page.getByRole('button', { name: 'Save password' }).click();
	await expect(page.getByRole('alert')).toHaveText('This link has expired. Ask for a new one.');
});

for (const width of [390, 744, 1280]) {
	test(`sign-in has no sideways scroll at ${width}`, async ({ page }) => {
		await page.setViewportSize({ width, height: 800 });
		for (const path of ['/sign-in', '/reset', '/invite/not-a-real-token']) {
			await page.goto(path);
			await expect(page.locator('main')).toBeVisible();
			const fits = await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth);
			expect(fits, `${path} scrolls sideways`).toBe(true);
		}
	});
}

test('sign-in pages pass axe', async ({ page }) => {
	for (const path of ['/sign-in', '/reset', '/invite/not-a-real-token']) {
		await page.goto(path);
		await expect(page.locator('main')).toBeVisible();
		const result = await new AxeBuilder({ page }).analyze();
		expect(result.violations, path).toEqual([]);
	}
});
