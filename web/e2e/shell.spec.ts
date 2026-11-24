import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';

const password = 'twelve chars ok';

/** Signs in without the code step, through the test server, so these tests do not spend the hourly limit on emailed codes. */
async function loginAs(page: Page, email: string) {
	const res = await page.request.get(`/api/test/login?email=${encodeURIComponent(email)}`);
	expect(res.status()).toBe(204);
}

const mainNav = (page: Page) => page.getByRole('navigation', { name: 'Main', exact: true });
const sidebar = (page: Page) => page.locator('aside.sidebar');

test('front desk does not see Staff, and /staff is refused', async ({ page }) => {
	await loginAs(page, 'mai@hotel.com');
	await page.goto('/');
	await expect(page).toHaveURL('/calendar'); // front desk has no overview, so it lands on its first item
	await expect(mainNav(page).getByRole('link', { name: 'Calendar' })).toBeVisible();
	await expect(mainNav(page).getByRole('link', { name: 'Staff' })).toHaveCount(0);
	await page.goto('/staff');
	await expect(page.getByText('You do not have access to this page.')).toBeVisible();
});

test('owner switches view and the banner returns', async ({ page }) => {
	await loginAs(page, 'owner@hotel.com');
	await page.goto('/');
	await expect(mainNav(page).getByRole('link', { name: 'Staff' })).toBeVisible();
	const view = page.getByRole('combobox', { name: 'Viewing as' });
	await view.selectOption({ label: 'Front desk' });
	await expect(page.getByText('Viewing as Front desk').first()).toBeVisible();
	await expect(mainNav(page).getByRole('link', { name: 'Calendar' })).toBeVisible();
	await expect(mainNav(page).getByRole('link', { name: 'Staff' })).toHaveCount(0);
	await page.getByRole('button', { name: 'Back to owner view' }).click();
	await expect(page.getByText('Viewing as Front desk')).toHaveCount(0);
	await expect(mainNav(page).getByRole('link', { name: 'Staff' })).toBeVisible();
});

test('manager has no Viewing as control', async ({ page }) => {
	await loginAs(page, 'daniel@hotel.com');
	await page.goto('/');
	await expect(mainNav(page).getByRole('link', { name: 'Staff' })).toBeVisible();
	await expect(page.getByRole('combobox', { name: 'Viewing as' })).toHaveCount(0);
});

test('layout per width', async ({ page }) => {
	await loginAs(page, 'owner@hotel.com');
	await page.setViewportSize({ width: 1280, height: 800 });
	await page.goto('/staff');
	await expect(sidebar(page)).toBeVisible();
	expect((await sidebar(page).boundingBox())?.width).toBe(240);

	await page.setViewportSize({ width: 744, height: 900 });
	await expect(sidebar(page)).toBeVisible();
	await expect.poll(async () => (await sidebar(page).boundingBox())?.width).toBe(68);

	await page.setViewportSize({ width: 390, height: 800 });
	await expect(sidebar(page)).toBeHidden();
	const bottom = page.getByRole('navigation', { name: 'Main, bottom bar' });
	await expect(bottom).toBeVisible();
	await expect(bottom.getByText('Overview')).toBeVisible();
	await expect(bottom.getByText('Activity')).toBeVisible();
});

test('the tablet rail expands over the page and closes again', async ({ page }) => {
	await loginAs(page, 'owner@hotel.com');
	await page.setViewportSize({ width: 744, height: 900 });
	await page.goto('/staff');
	await page.getByRole('button', { name: 'Expand menu' }).click();
	await expect.poll(async () => (await sidebar(page).boundingBox())?.width).toBe(240);
	await mainNav(page).getByRole('link', { name: 'Activity' }).click();
	await expect(page).toHaveURL('/activity');
	await expect.poll(async () => (await sidebar(page).boundingBox())?.width).toBe(68);
});

test('sidebar collapse is remembered after reload', async ({ page }) => {
	await loginAs(page, 'owner@hotel.com');
	await page.setViewportSize({ width: 1280, height: 800 });
	await page.goto('/staff');
	await page.getByRole('button', { name: 'Collapse menu' }).click();
	await expect.poll(async () => (await sidebar(page).boundingBox())?.width).toBe(68);
	await page.reload();
	await expect(sidebar(page)).toBeVisible();
	await expect.poll(async () => (await sidebar(page).boundingBox())?.width).toBe(68);
	await page.getByRole('button', { name: 'Expand menu' }).click();
	await expect.poll(async () => (await sidebar(page).boundingBox())?.width).toBe(240);
});

test('bell count rises without reload', async ({ page }) => {
	await loginAs(page, 'daniel@hotel.com');
	await page.goto('/');
	const bell = page.getByRole('button', { name: /^Notifications/ });
	await expect(bell).toBeVisible();
	const count = async () => Number(/(\d+) unread/.exec((await bell.getAttribute('aria-label')) ?? '')?.[1] ?? 0);
	const before = await count();
	await page.request.get('/api/test/notify?email=daniel@hotel.com');
	await expect.poll(count, { timeout: 4000 }).toBe(before + 1);
	await bell.click();
	await expect(page.getByText('Refund needs approval')).toBeVisible();
	await page.getByRole('button', { name: 'Mark all read' }).click();
	await expect.poll(count).toBe(0);
});

test('owner invites a user and sees them as invited', async ({ page }) => {
	await loginAs(page, 'owner@hotel.com');
	await page.goto('/staff');
	await page.getByRole('button', { name: 'Invite user' }).click();
	await page.getByLabel('Email').fill('invitee@hotel.com');
	await page.getByLabel('Role').selectOption('revenue');
	await page.getByRole('button', { name: 'Send invite' }).click();
	await expect(page.getByRole('status')).toContainText('Invite sent to invitee@hotel.com.');
	const row = page.getByRole('row', { name: /invitee@hotel.com/ });
	await expect(row).toContainText('Invited');
	await expect(row).toContainText('Revenue');
});

test('deactivating a user needs a confirm and updates the row', async ({ page }) => {
	await loginAs(page, 'owner@hotel.com');
	await page.goto('/staff');
	const row = page.getByRole('row', { name: /Tuan Le/ });
	await expect(row).toContainText('Active');
	await row.getByRole('button', { name: 'Deactivate' }).click();
	const dialog = page.getByRole('dialog', { name: 'Deactivate Tuan Le?' });
	await expect(dialog).toBeVisible();
	await dialog.getByRole('button', { name: 'Cancel' }).click();
	await expect(row).toContainText('Active'); // cancelling changes nothing
	await row.getByRole('button', { name: 'Deactivate' }).click();
	await page.getByRole('dialog', { name: 'Deactivate Tuan Le?' }).getByRole('button', { name: 'Deactivate' }).click();
	await expect(row).toContainText('Deactivated');
	await expect(row.getByRole('button', { name: 'Reactivate' })).toBeVisible();
});

test('activity lists the invite with actor, time and what was set', async ({ page }) => {
	await loginAs(page, 'owner@hotel.com');
	await page.goto('/staff');
	await page.getByRole('button', { name: 'Invite user' }).click();
	await page.getByLabel('Email').fill('trail@hotel.com');
	await page.getByRole('button', { name: 'Send invite' }).click();
	await expect(page.getByRole('status')).toContainText('Invite sent');
	await page.goto('/activity');
	const entry = page.getByRole('listitem').filter({ hasText: 'invited trail@hotel.com' });
	await expect(entry).toContainText('Linh Nguyen');
	await expect(entry).toContainText('Role: Front desk');
	await expect(entry.locator('time')).toHaveAttribute('datetime', /^\d{4}-\d\d-\d\dT/);
});

test('expired session keeps the typed invite email', async ({ page }) => {
	await loginAs(page, 'owner@hotel.com');
	await page.goto('/staff');
	await page.getByRole('button', { name: 'Invite user' }).click();
	await page.getByLabel('Email').fill('kept@hotel.com');
	await page.request.get('/api/test/expire-session?email=owner@hotel.com');
	await page.getByRole('button', { name: 'Send invite' }).click();
	await expect(page).toHaveURL('/sign-in');
	await page.getByLabel('Email').fill('owner@hotel.com');
	await page.getByLabel('Password').fill(password);
	await page.getByRole('button', { name: 'Sign in' }).click();
	await expect(page.getByRole('heading', { name: 'Enter the code we emailed you' })).toBeVisible();
	await page.getByLabel('Code').fill((await (await page.request.get('/api/test/last-code?email=owner@hotel.com')).text()).trim());
	await page.getByRole('button', { name: 'Continue' }).click();
	await expect(page).toHaveURL('/staff');
	await expect(page.getByLabel('Email')).toHaveValue('kept@hotel.com');
});

test('user in two organizations must pick one', async ({ page }) => {
	await page.goto('/sign-in');
	await page.getByLabel('Email').fill('multi@hotel.com');
	await page.getByLabel('Password').fill(password);
	await page.getByRole('button', { name: 'Sign in' }).click();
	await expect(page.getByRole('heading', { name: 'Enter the code we emailed you' })).toBeVisible();
	await page.getByLabel('Code').fill((await (await page.request.get('/api/test/last-code?email=multi@hotel.com')).text()).trim());
	await page.getByRole('button', { name: 'Continue' }).click();
	await expect(page).toHaveURL('/sign-in/organization');
	await page.getByRole('button', { name: 'Second Hotel' }).click();
	await expect(page).toHaveURL('/'); // a manager lands on the overview
	await expect(page.getByText('Harbour house').first()).toBeVisible();
});

test('a person with no access sees why, not a blank page', async ({ page }) => {
	await loginAs(page, 'nobody@hotel.com');
	await page.goto('/');
	await expect(page.getByText('Your account has no access. Ask your manager.')).toBeVisible();
	await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
});

test('an invite can be cancelled', async ({ page }) => {
	await loginAs(page, 'owner@hotel.com');
	await page.goto('/staff');
	await page.getByRole('button', { name: 'Invite user' }).click();
	await page.getByLabel('Email').fill('typo@hotel.com');
	await page.getByRole('button', { name: 'Send invite' }).click();
	const row = page.getByRole('row', { name: /typo@hotel.com/ });
	await expect(row).toContainText('Invited');
	await row.getByRole('button', { name: 'Cancel invite' }).click();
	await expect(page.getByRole('row', { name: /typo@hotel.com/ })).toHaveCount(0);
});

test('the organization list loads from the server when the browser has none', async ({ page }) => {
	await page.goto('/sign-in');
	await page.getByLabel('Email').fill('multi@hotel.com');
	await page.getByLabel('Password').fill(password);
	await page.getByRole('button', { name: 'Sign in' }).click();
	await expect(page.getByRole('heading', { name: 'Enter the code we emailed you' })).toBeVisible();
	await page.getByLabel('Code').fill((await (await page.request.get('/api/test/last-code?email=multi@hotel.com')).text()).trim());
	await page.getByRole('button', { name: 'Continue' }).click();
	await expect(page).toHaveURL('/sign-in/organization');
	await page.evaluate(() => sessionStorage.clear());
	await page.reload();
	await expect(page.getByRole('button', { name: 'Second Hotel' })).toBeVisible();
});

test('reduced motion removes the screen animation', async ({ page }) => {
	await page.emulateMedia({ reducedMotion: 'reduce' });
	await loginAs(page, 'owner@hotel.com');
	await page.goto('/staff');
	const ms = await page.locator('.screen').evaluate((el) => parseFloat(getComputedStyle(el).animationDuration) * 1000);
	expect(ms).toBeLessThanOrEqual(1);
	await page.emulateMedia({ reducedMotion: 'no-preference' });
	await page.goto('/activity');
	const full = await page.locator('.screen').evaluate((el) => parseFloat(getComputedStyle(el).animationDuration) * 1000);
	expect(full).toBe(200);
});

for (const width of [390, 744, 1280]) {
	test(`shell, staff and activity pass axe and do not scroll sideways at ${width}`, async ({ page }) => {
		await loginAs(page, 'owner@hotel.com');
		await page.setViewportSize({ width, height: 900 });
		for (const path of ['/', '/staff', '/activity']) {
			await page.goto(path);
			await expect(page.locator('main .screen')).toBeVisible();
			await expect(page.locator('main .screen h1')).toBeVisible();
			await page.waitForTimeout(300); // let the screen animation finish before axe measures contrast
			const fits = await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth);
			expect(fits, `${path} scrolls sideways at ${width}`).toBe(true);
			const result = await new AxeBuilder({ page }).analyze();
			expect(result.violations.map((v) => `${v.id}: ${v.nodes[0]?.html}`), path).toEqual([]);
		}
	});
}

test('revenue reaches yield rules and promo codes from the sidebar, with the current one marked', async ({ page }) => {
	await page.request.get('/api/test/login?email=rita@hotel.com');
	await page.goto('/rates');
	const more = page.getByRole('navigation', { name: 'More screens' });
	await expect(more.getByRole('link', { name: 'Promo codes' })).toBeVisible();
	await more.getByRole('link', { name: 'Yield rules' }).click();
	await expect(page).toHaveURL('/yield');
	await expect(more.getByRole('link', { name: 'Yield rules' })).toHaveAttribute('aria-current', 'page');
});

test('housekeeping has no more screens, so the sidebar shows none', async ({ page }) => {
	await page.request.get('/api/test/login?email=tuan@hotel.com');
	await page.goto('/rooms');
	await expect(page.getByRole('navigation', { name: 'More screens' })).toHaveCount(0);
});

