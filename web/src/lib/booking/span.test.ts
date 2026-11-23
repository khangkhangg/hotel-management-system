import { expect, test } from 'vitest';
import { monthToDate } from './span.ts';

test('month to date is compared with the same days of the month before', () => {
	expect(monthToDate('2026-11-24')).toEqual({ from: '2026-11-01', to: '2026-11-25', prevFrom: '2026-10-01', prevTo: '2026-10-25' });
});

test('the same days stop at the end of a shorter month', () => {
	expect(monthToDate('2026-03-31')).toEqual({ from: '2026-03-01', to: '2026-04-01', prevFrom: '2026-02-01', prevTo: '2026-03-01' });
});

test('January compares with December of the year before', () => {
	expect(monthToDate('2026-01-10').prevFrom).toBe('2025-12-01');
});
