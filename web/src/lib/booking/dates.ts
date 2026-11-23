// Calendar dates as "YYYY-MM-DD" strings. All the maths runs in UTC, so a time zone never moves a day.
const DAY = 86_400_000;
const at = (iso: string) => Date.parse(iso + 'T00:00:00Z');
const iso = (ms: number) => new Date(ms).toISOString().slice(0, 10);

export const addDays = (date: string, n: number) => iso(at(date) + n * DAY);
export const daysBetween = (from: string, to: string) => Math.round((at(to) - at(from)) / DAY);
export const rangeDays = (from: string, n: number) => Array.from({ length: n }, (_, i) => addDays(from, i));

const fmt = (opts: Intl.DateTimeFormatOptions) => new Intl.DateTimeFormat('en', { ...opts, timeZone: 'UTC' });
const dayFormat = fmt({ month: 'short', day: 'numeric' });
const weekdayFormat = fmt({ weekday: 'short' });
export const formatDay = (date: string) => dayFormat.format(at(date));
export const weekday = (date: string) => weekdayFormat.format(at(date));

export type Placement = { start: number; span: number; cutLeft: boolean; cutRight: boolean };

/** Where a stay [from, to) sits on a row of days: its first column and how many it spans, or null when it is off the grid. */
export function overlapColumns(days: string[], from: string, to: string): Placement | null {
	const first = days[0];
	const end = addDays(days[days.length - 1], 1); // exclusive
	if (to <= first || from >= end) return null;
	const start = Math.max(0, daysBetween(first, from));
	const stop = Math.min(days.length, daysBetween(first, to));
	return { start, span: stop - start, cutLeft: from < first, cutRight: to > end };
}
