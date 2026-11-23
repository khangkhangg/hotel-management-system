import { error } from '@sveltejs/kit';
import { apiOrigin } from '#lib/server/origin.ts';
import type { PageServerLoad } from './$types';

// The booking page is the one page search engines read, so it is rendered on the server with the hotel already in it.
export const ssr = true;

export const load: PageServerLoad = async ({ params, fetch }) => {
	const res = await fetch(`${apiOrigin()}/api/public/${encodeURIComponent(params.slug)}`);
	if (res.status === 404) error(404, 'We could not find this hotel.');
	if (!res.ok) error(502, 'The booking page is not available right now.');
	return { info: await res.json() };
};
