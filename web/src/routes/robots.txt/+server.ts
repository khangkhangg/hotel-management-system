import type { RequestHandler } from './$types';

// Search engines may read the booking pages. They are kept away from the API, the staff app and the private guest links.
export const GET: RequestHandler = ({ url }) =>
	new Response(
		['User-agent: *', 'Allow: /book/', 'Disallow: /book/*/manage', 'Disallow: /api/', 'Disallow: /reconfirm/', 'Disallow: /sign-in', 'Disallow: /dev/', '', `Sitemap: ${url.origin}/sitemap.xml`, ''].join('\n'),
		{ headers: { 'Content-Type': 'text/plain; charset=utf-8', 'Cache-Control': 'public, max-age=3600' } }
	);
