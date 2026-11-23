import { describe, expect, it } from 'vitest';
import { canonicalUrl, hotelJsonLd, metaDescription, serializeJsonLd, type SeoInfo } from './seo.ts';

const info: SeoInfo = {
	name: 'Sea View Hotel',
	address: '1 Beach Road, Da Nang',
	site: { logo: '', photos: [{ id: 'p1', caption: 'Pool' }], amenities: ['pool', 'wifi'] },
	reviews: { count: 0, average: 0, recent: [] }
};
const o = { origin: 'https://book.example.com', slug: 'sea-view' };

describe('meta description', () => {
	it('joins the tagline and the description and stops at a word within 155 characters', () => {
		const d = metaDescription(info, 'A quiet hotel by the sea.', `${'word '.repeat(60)}end`);
		expect(d.length).toBeLessThanOrEqual(156);
		expect(d.startsWith('A quiet hotel by the sea. word')).toBe(true);
		expect(d.endsWith('…')).toBe(true);
		expect(d).not.toMatch(/wor…$/);
	});
	it('falls back to a plain sentence with the name and address when the hotel wrote nothing', () => {
		expect(metaDescription(info, '', '')).toBe('Book Sea View Hotel direct. 1 Beach Road, Da Nang.');
	});
});

describe('canonical address', () => {
	it('is the booking page without a query', () => {
		expect(canonicalUrl(o.origin, o.slug)).toBe('https://book.example.com/book/sea-view');
		expect(canonicalUrl('https://book.example.com/', 'sea-view')).toBe('https://book.example.com/book/sea-view');
	});
});

describe('hotel structured data', () => {
	it('names the hotel, its address, absolute photo addresses and amenities, and no rating before the first review', () => {
		const ld = hotelJsonLd(info, { ...o, description: 'Nice.' }) as Record<string, any>;
		expect(ld['@type']).toBe('Hotel');
		expect(ld.name).toBe('Sea View Hotel');
		expect(ld.url).toBe('https://book.example.com/book/sea-view');
		expect(ld.image).toEqual(['https://book.example.com/api/public/sea-view/photos/p1']);
		expect(ld.address).toEqual({ '@type': 'PostalAddress', streetAddress: '1 Beach Road, Da Nang' });
		expect(ld.amenityFeature.map((a: any) => a.name)).toEqual(['Pool', 'Wi-Fi']);
		expect('aggregateRating' in ld).toBe(false);
		expect('review' in ld).toBe(false);
	});
	it('adds the rating and the newest reviews once guests have reviewed', () => {
		const withReviews = { ...info, reviews: { count: 2, average: 4.5, recent: [{ rating: 5, comment: 'Lovely', name: 'Mai P.', reply: '', createdAt: '2026-12-01T10:00:00Z' }] } };
		const ld = hotelJsonLd(withReviews, { ...o, description: '' }) as Record<string, any>;
		expect(ld.aggregateRating).toEqual({ '@type': 'AggregateRating', ratingValue: 4.5, reviewCount: 2, bestRating: 5, worstRating: 1 });
		expect(ld.review[0]).toMatchObject({ '@type': 'Review', reviewBody: 'Lovely', author: { '@type': 'Person', name: 'Mai P.' }, reviewRating: { '@type': 'Rating', ratingValue: 5 }, datePublished: '2026-12-01' });
		expect('description' in ld).toBe(false);
	});
	it('cannot be broken out of its script tag by a hotel name', () => {
		const text = serializeJsonLd(hotelJsonLd({ ...info, name: '</script><script>alert(1)</script>' }, { ...o, description: '' }));
		expect(text).not.toContain('</script>');
		expect(JSON.parse(text).name).toBe('</script><script>alert(1)</script>');
	});
});
