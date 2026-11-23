export type Run<T> = { from: string; to: string; value: T; n: number };

/** Folds consecutive days that share a value into one run, so sixty nights at one price read as one line. */
export function foldRuns<T>(days: string[], get: (day: string) => T | undefined, same: (a: T, b: T) => boolean): Run<T>[] {
	const runs: Run<T>[] = [];
	let last: string | null = null;
	for (const day of days) {
		const value = get(day);
		const tail = runs[runs.length - 1];
		if (value === undefined) {
			last = null;
			continue;
		}
		if (tail && last !== null && same(tail.value, value)) {
			tail.to = day;
			tail.n++;
		} else runs.push({ from: day, to: day, value, n: 1 });
		last = day;
	}
	return runs;
}
