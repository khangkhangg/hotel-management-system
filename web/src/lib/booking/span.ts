import { addDays } from './dates.ts';

/** This month so far and the same days of the month before. Both ends are exclusive on `to`, like the payments API. */
export function monthToDate(today: string) {
	const [y, m, d] = today.split('-').map(Number);
	const pad = (n: number) => String(n).padStart(2, '0');
	const first = (year: number, month: number) => `${year}-${pad(month)}-01`;
	const prevY = m === 1 ? y - 1 : y;
	const prevM = m === 1 ? 12 : m - 1;
	const prevFrom = first(prevY, prevM);
	const prevLength = new Date(Date.UTC(prevY, prevM, 0)).getUTCDate();
	return { from: first(y, m), to: addDays(today, 1), prevFrom, prevTo: addDays(prevFrom, Math.min(d, prevLength)) };
}

/** The whole month before today's, both days included. */
export function lastMonth(today: string) {
	const { prevFrom } = monthToDate(today);
	const [y, m] = prevFrom.split('-').map(Number);
	return { from: prevFrom, to: addDays(`${y}-${String(m).padStart(2, '0')}-01`, new Date(Date.UTC(y, m, 0)).getUTCDate() - 1) };
}
