import { describe, expect, it } from 'vitest';
import { direction, offered, pickLocale, translate } from './locale.ts';

describe('pickLocale', () => {
	it('prefers the address, then the saved choice, then the browser, then the hotel', () => {
		expect(pickLocale({ url: 'en', saved: 'en', browser: ['en'], fallback: 'en' })).toBe('en');
		expect(pickLocale({ url: undefined, saved: 'en', browser: ['fr'], fallback: 'en' })).toBe('en');
		expect(pickLocale({ url: undefined, saved: undefined, browser: ['de-DE', 'en-GB'], fallback: 'en' })).toBe('en');
		expect(pickLocale({ url: undefined, saved: undefined, browser: ['de'], fallback: 'en' })).toBe('en');
	});
	it('never picks a language that has not been reviewed', () => {
		expect(offered().map((l) => l.code)).toEqual(['en']);
		expect(pickLocale({ url: 'es', saved: 'ar', browser: ['fr'], fallback: 'en' })).toBe('en');
	});
});

describe('direction', () => {
	it('is right to left for Arabic and Urdu only', () => {
		expect(direction('ar')).toBe('rtl');
		expect(direction('ur')).toBe('rtl');
		expect(direction('en')).toBe('ltr');
		expect(direction('zh-Hans')).toBe('ltr');
	});
});

describe('translate', () => {
	it('fills placeholders and falls back to English, then to the key', () => {
		expect(translate('en', 'guest.nights', { count: 2 })).toBe('2 nights');
		expect(translate('en', 'guest.nights', { count: 1 })).toBe('1 night');
		expect(translate('de', 'guest.total')).toBe(translate('en', 'guest.total'));
		expect(translate('en', 'nope.nothing')).toBe('nope.nothing');
	});
});
