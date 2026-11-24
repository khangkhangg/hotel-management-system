import { expect, test } from 'vitest';
import { formatMoney, plainAmount } from './money.ts';

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

import { parseMoney } from './money.ts';

test('typed amounts become minor units, by the currency', () => {
	expect(parseMoney('12.50', 'USD')).toBe(1250);
	expect(parseMoney('120', 'USD')).toBe(12000);
	expect(parseMoney('12,5', 'USD')).toBe(1250);
	expect(parseMoney('500000', 'VND')).toBe(500000);
	expect(parseMoney(' 40 ', 'USD')).toBe(4000);
});

test('text that is not a plain amount is refused, not guessed', () => {
	for (const bad of ['', 'abc', '1,500', '1.234.5', '-5', '12.345', '0', '1e3', '500.5']) {
		expect(parseMoney(bad, bad === '500.5' ? 'VND' : 'USD'), bad).toBeNull();
	}
});

test('plainAmount writes an amount the way a person types it, so it can be read back', () => {
	expect(plainAmount(24000, 'USD')).toBe('240.00');
	expect(plainAmount(150000, 'VND')).toBe('150000');
	expect(parseMoney(plainAmount(24050, 'USD'), 'USD')).toBe(24050);
});
