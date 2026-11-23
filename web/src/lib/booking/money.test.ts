import { expect, test } from 'vitest';
import { formatMoney } from './money.ts';

test('formats minor units in the currency', () => {
	expect(formatMoney(20000, 'USD')).toBe('$200.00');
	expect(formatMoney(5, 'USD')).toBe('$0.05');
	expect(formatMoney(-1500, 'USD')).toBe('-$15.00');
});

test('a currency without decimals is not divided', () => {
	expect(formatMoney(150000, 'VND')).toBe('₫150,000');
});

test('an unknown currency code still shows the amount and the code', () => {
	expect(formatMoney(1234, 'XXX')).toContain('12.34');
});
