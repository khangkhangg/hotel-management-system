import { expect, test } from '@playwright/test';

// These read the first response, like a search engine that does not run scripts.
test('the booking page answers with the hotel already in it, ready for search engines', async ({ request, baseURL }) => {
	test.setTimeout(180_000); // the first server render compiles the page
	const res = await request.get('/book/main-hotel');
	expect(res.status()).toBe(200);
	const html = (await res.text()).replace(/<!--.*?-->/g, '');
	expect(html).toContain('<title>Main hotel</title>');
	expect(html).toMatch(/<meta name="description" content="[^"]+"/);
	expect(html).toContain(`<link rel="canonical" href="${baseURL}/book/main-hotel"`);
	expect(html).toContain('content="index,follow,max-image-preview:large"');
	expect(html).toContain('<meta property="og:title" content="Main hotel"');
	expect(html).toMatch(/<h1[^>]*>[^<]*Main hotel/);
	const ld = html.match(/<script type="application\/ld\+json">([\s\S]*?)<\/script>/);
	expect(ld).not.toBeNull();
	const data = JSON.parse(ld![1]);
	expect(data['@type']).toBe('Hotel');
	expect(data.name).toBe('Main hotel');
	expect(data.url).toBe(`${baseURL}/book/main-hotel`);
});

test('a hotel that does not exist is a real 404, not a blank page', async ({ request }) => {
	expect((await request.get('/book/no-such-hotel')).status()).toBe(404);
});

test('robots.txt keeps crawlers off the private paths and points to the sitemap', async ({ request, baseURL }) => {
	const text = await (await request.get('/robots.txt')).text();
	expect(text).toContain('Allow: /book/');
	expect(text).toContain('Disallow: /api/');
	expect(text).toContain('Disallow: /book/*/manage');
	expect(text).toContain('Disallow: /reconfirm/');
	expect(text).toContain(`Sitemap: ${baseURL}/sitemap.xml`);
});

test('the sitemap lists the hotels that have rooms', async ({ request, baseURL }) => {
	const res = await request.get('/sitemap.xml');
	expect(res.headers()['content-type']).toContain('xml');
	const text = await res.text();
	expect(text).toContain(`<loc>${baseURL}/book/main-hotel</loc>`);
	expect(text).not.toContain('harbour-house');
});

test('private guest pages ask search engines to stay away', async ({ page }) => {
	await page.goto('/book/main-hotel/manage');
	await expect(page.locator('meta[name="robots"]')).toHaveAttribute('content', 'noindex,nofollow');
	await page.goto('/sign-in');
	await expect(page.locator('meta[name="robots"]')).toHaveAttribute('content', 'noindex,nofollow');
});
