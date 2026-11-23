import { apiOrigin } from '#lib/server/origin.ts';
import type { RequestHandler } from './$types';

const escape = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

// One line for each hotel that has rooms to sell.
export const GET: RequestHandler = async ({ url, fetch }) => {
	const res = await fetch(`${apiOrigin()}/api/public`);
	const slugs: string[] = res.ok ? await res.json() : [];
	const body = `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${slugs
		.map((s) => `<url><loc>${escape(`${url.origin}/book/${s}`)}</loc></url>`)
		.join('\n')}\n</urlset>\n`;
	return new Response(body, { headers: { 'Content-Type': 'application/xml; charset=utf-8', 'Cache-Control': 'public, max-age=3600' } });
};
