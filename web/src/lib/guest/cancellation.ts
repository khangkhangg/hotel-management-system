export type Terms = { freeCancelUntilDays: number; lateCancelPenalty: string; lateCancelPercent: number };

/** Which sentence tells a guest what cancelling costs, and the numbers it needs. */
export function cancellationTerms(p: Terms): { key: string; params: Record<string, number> } {
	if (p.lateCancelPenalty === 'none') return { key: 'guest.cancel.free', params: {} };
	const params: Record<string, number> = { days: p.freeCancelUntilDays };
	if (p.lateCancelPenalty === 'percent') params.percent = p.lateCancelPercent;
	return { key: `guest.cancel.${p.lateCancelPenalty}`, params };
}
