import { expect, test } from 'vitest';
import { cleaningOrder } from './housekeeping.ts';

const task = (n: string, arrivalToday: boolean, status = 'todo') => ({ roomNumber: n, arrivalToday, status });

test('rooms with a guest arriving today come first, finished ones last', () => {
	const list = [task('102', false), task('101', true), task('103', false, 'done'), task('104', true)];
	expect(cleaningOrder(list).map((t) => t.roomNumber)).toEqual(['101', '104', '102', '103']);
});

test('the order inside a group follows the room number', () => {
	expect(cleaningOrder([task('20', false), task('3', false)]).map((t) => t.roomNumber)).toEqual(['3', '20']);
});
