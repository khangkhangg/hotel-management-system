import { expect, test } from 'vitest';
import { cancellationTerms } from './cancellation.ts';

test('a plan with no penalty is free to cancel at any time', () => {
	expect(cancellationTerms({ freeCancelUntilDays: 0, lateCancelPenalty: 'none', lateCancelPercent: 0 })).toEqual({ key: 'guest.cancel.free', params: {} });
});

test('a late cancellation charge is named with the day the free period ends', () => {
	expect(cancellationTerms({ freeCancelUntilDays: 3, lateCancelPenalty: 'first_night', lateCancelPercent: 0 })).toEqual({ key: 'guest.cancel.first_night', params: { days: 3 } });
	expect(cancellationTerms({ freeCancelUntilDays: 0, lateCancelPenalty: 'full', lateCancelPercent: 0 })).toEqual({ key: 'guest.cancel.full', params: { days: 0 } });
	expect(cancellationTerms({ freeCancelUntilDays: 7, lateCancelPenalty: 'percent', lateCancelPercent: 50 })).toEqual({ key: 'guest.cancel.percent', params: { days: 7, percent: 50 } });
});
