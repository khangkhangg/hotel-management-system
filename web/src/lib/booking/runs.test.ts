import { expect, test } from 'vitest';
import { foldRuns } from './runs.ts';

const same = (a: number, b: number) => a === b;

test('days in a row with the same value become one run', () => {
	const days = ['d1', 'd2', 'd3', 'd4'];
	const v: Record<string, number> = { d1: 5, d2: 5, d3: 7, d4: 5 };
	expect(foldRuns(days, (d) => v[d], same)).toEqual([
		{ from: 'd1', to: 'd2', value: 5, n: 2 },
		{ from: 'd3', to: 'd3', value: 7, n: 1 },
		{ from: 'd4', to: 'd4', value: 5, n: 1 }
	]);
});

test('a day with no value breaks a run and is left out', () => {
	expect(foldRuns(['a', 'b', 'c'], (d) => (d === 'b' ? undefined : 1), same).map((r) => r.n)).toEqual([1, 1]);
});
