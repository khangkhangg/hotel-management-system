import { describe, expect, test } from 'vitest';
import { addDays, daysBetween, rangeDays, formatDay, weekday, overlapColumns } from './dates.ts';

describe('dates', () => {
	test('adds days across a month end without a time zone shift', () => {
		expect(addDays('2026-12-30', 3)).toBe('2027-01-02');
		expect(addDays('2026-03-01', -1)).toBe('2026-02-28');
	});

	test('counts nights between two dates', () => {
		expect(daysBetween('2026-12-10', '2026-12-13')).toBe(3);
		expect(daysBetween('2026-12-13', '2026-12-10')).toBe(-3);
	});

	test('lists a run of days', () => {
		expect(rangeDays('2026-12-30', 4)).toEqual(['2026-12-30', '2026-12-31', '2027-01-01', '2027-01-02']);
	});

	test('formats a day and its weekday', () => {
		expect(formatDay('2026-12-05')).toBe('Dec 5');
		expect(weekday('2026-12-05')).toBe('Sat');
	});

	test('places a stay on the grid: first column and how many it spans, cut to the visible days', () => {
		const days = rangeDays('2026-12-10', 7); // 10th to 16th
		expect(overlapColumns(days, '2026-12-12', '2026-12-15')).toEqual({ start: 2, span: 3, cutLeft: false, cutRight: false });
		expect(overlapColumns(days, '2026-12-08', '2026-12-12')).toEqual({ start: 0, span: 2, cutLeft: true, cutRight: false });
		expect(overlapColumns(days, '2026-12-15', '2026-12-20')).toEqual({ start: 5, span: 2, cutLeft: false, cutRight: true });
		expect(overlapColumns(days, '2026-12-01', '2026-12-05')).toBeNull();
		expect(overlapColumns(days, '2026-12-17', '2026-12-19')).toBeNull();
	});
});
