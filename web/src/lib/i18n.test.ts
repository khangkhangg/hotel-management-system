import { expect, test } from 'vitest';
import { t, has } from './i18n.ts';

test('t returns the English text for a key', () => {
	expect(t('signIn.title')).toBe('Sign in');
});

test('t fills {placeholders}', () => {
	expect(t('code.help', { email: 'mai@hotel.com' })).toBe('We sent a 6-digit code to mai@hotel.com. It works for 10 minutes.');
});

test('an unknown key shows the key, so a gap is visible', () => {
	expect(t('no.such.key')).toBe('no.such.key');
	expect(has('no.such.key')).toBe(false);
	expect(has('signIn.title')).toBe(true);
});

test('a placeholder with no value is left as written', () => {
	expect(t('code.help')).toContain('{email}');
});
