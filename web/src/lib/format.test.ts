import { expect, test } from 'vitest';
import { formatWhen } from './format.ts';

test('a missing time reads as Never', () => {
	expect(formatWhen(null)).toBe('Never');
});

test('a time reads as a short date and time', () => {
	expect(formatWhen('2026-11-22T08:30:00Z', 'UTC')).toBe('Nov 22, 2026, 8:30 AM');
});

test('a time that is not a date is shown as it came', () => {
	expect(formatWhen('soon')).toBe('soon');
});
