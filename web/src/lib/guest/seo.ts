// What search engines and link previews read from a hotel's booking page: the description, the canonical address and the
// hotel structured data. These are plain functions so the server and the tests build the same text.
import { translate } from './locale.ts';

export type SeoReview = { rating: number; comment: string; name: string; reply: string; createdAt: string };
export type SeoInfo = {
	name: string;
	address: string;
	site: { logo: string; photos: { id: string; caption: string }[]; amenities: string[] };
	reviews: { count: number; average: number; recent: SeoReview[] };
};

const LIMIT = 155;

/** The text under the title in search results: the hotel's own words, cut at a word, or a plain sentence when it wrote none. */
export function metaDescription(info: SeoInfo, tagline: string, description: string): string {
	const text = [tagline, description]
		.map((s) => s.replace(/\s+/g, ' ').trim())
		.filter(Boolean)
		.join(' ');
	if (!text) return `Book ${info.name} direct.${info.address ? ` ${info.address.replace(/\s+/g, ' ').trim()}.` : ''}`;
	if (text.length <= LIMIT) return text;
	const cut = text.slice(0, LIMIT);
	return `${cut.slice(0, cut.lastIndexOf(' ') > 0 ? cut.lastIndexOf(' ') : LIMIT).trimEnd()}…`;
}

/** The one address search engines should list for the page, with no language or tracking query. */
export const canonicalUrl = (origin: string, slug: string): string => `${origin.replace(/\/+$/, '')}/book/${slug}`;

/** The hotel as schema.org describes it. The rating and reviews appear only once guests have written some. */
export function hotelJsonLd(info: SeoInfo, o: { origin: string; slug: string; description: string }): Record<string, unknown> {
	const base = o.origin.replace(/\/+$/, '');
	const ld: Record<string, unknown> = { '@context': 'https://schema.org', '@type': 'Hotel', name: info.name, url: canonicalUrl(o.origin, o.slug) };
	if (o.description) ld.description = o.description;
	const photos = [...info.site.photos.map((p) => `${base}/api/public/${o.slug}/photos/${p.id}`)];
	if (photos.length) ld.image = photos;
	if (info.address) ld.address = { '@type': 'PostalAddress', streetAddress: info.address };
	if (info.site.amenities.length) {
		ld.amenityFeature = info.site.amenities.map((a) => ({ '@type': 'LocationFeatureSpecification', name: translate('en', `amenity.${a}`), value: true }));
	}
	if (info.reviews.count > 0) {
		ld.aggregateRating = { '@type': 'AggregateRating', ratingValue: info.reviews.average, reviewCount: info.reviews.count, bestRating: 5, worstRating: 1 };
		ld.review = info.reviews.recent.map((r) => ({
			'@type': 'Review',
			author: { '@type': 'Person', name: r.name },
			reviewRating: { '@type': 'Rating', ratingValue: r.rating, bestRating: 5, worstRating: 1 },
			reviewBody: r.comment,
			datePublished: r.createdAt.slice(0, 10)
		}));
	}
	return ld;
}

/** JSON for a script tag. A "<" is escaped, so text from a hotel cannot close the tag and start a script. */
export const serializeJsonLd = (ld: unknown): string => JSON.stringify(ld).replace(/</g, '\\u003c');
