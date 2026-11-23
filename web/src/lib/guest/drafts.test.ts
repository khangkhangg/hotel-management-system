import { describe, expect, it } from 'vitest';
import { locales, offered, translate } from './locale.ts';

const files = import.meta.glob('../../../messages/guest.*.json', { eager: true, import: 'default' }) as Record<string, Record<string, string>>;
const dict = (code: string) => files[`../../../messages/guest.${code}.json`];
const holes = (s: string) => (s.match(/\{\w+\}/g) ?? []).sort().join();
const forms = /\.(zero|one|two|few|many|other)$/;
const plurals = ['guest.nights', 'guest.reviewsCount'];

describe('draft translations wait for a native reviewer', () => {
	const en = dict('en');
	for (const l of locales.filter((x) => x.code !== 'en')) {
		it(`${l.code} has every key of English with the same {placeholders}`, () => {
			const d = dict(l.code);
			expect(d, `messages/guest.${l.code}.json is missing`).toBeDefined();
			for (const [k, v] of Object.entries(en)) {
				if (forms.test(k)) continue;
				expect(d[k], `${k} is missing`).toBeTruthy();
				expect(holes(d[k]), k).toBe(holes(v));
			}
			for (const base of plurals) expect(d[`${base}.other`], `${base}.other`).toContain('{count}');
			for (const k of Object.keys(d)) expect(k in en || (forms.test(k) && plurals.some((b) => k.startsWith(`${b}.`))), `${k} is not an English key`).toBe(true);
		});
	}

	it('plural forms follow each language', () => {
		expect(translate('ru', 'guest.nights', { count: 1 })).toBe('1 ночь');
		expect(translate('ru', 'guest.nights', { count: 3 })).toBe('3 ночи');
		expect(translate('ru', 'guest.nights', { count: 5 })).toBe('5 ночей');
		expect(translate('ar', 'guest.nights', { count: 2 })).toBe('ليلتان');
		expect(translate('ar', 'guest.nights', { count: 11 })).toBe('11 ليلة');
		expect(translate('zh-Hans', 'guest.nights', { count: 3 })).toBe('3 晚');
		expect(translate('fr', 'guest.next', { step: 'Détails' })).toBe('Suivant : Détails');
	});

	it('offers none of them until a native speaker has reviewed it', () => {
		expect(offered().map((l) => l.code)).toEqual(['en']);
	});
});
