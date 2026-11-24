<script lang="ts">
	import { addDays } from '#lib/booking/dates.ts';
	import { lastMonth, monthToDate } from '#lib/booking/span.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from './Button.svelte';

	// Month-end work runs on whole months, so these set both dates in one click.
	let { onpick }: { onpick: (from: string, to: string) => void } = $props();
	const today = session.today;
	const ranges = [
		{ id: 'month', label: t('reports.thisMonth'), get: () => ({ from: monthToDate(today).from, to: today }) },
		{ id: 'last', label: t('reports.lastMonth'), get: () => lastMonth(today) },
		{ id: 'thirty', label: t('reports.last30'), get: () => ({ from: addDays(today, -30), to: today }) }
	];
</script>

<div class="chips" role="group" aria-label={t('reports.quick')}>
	{#each ranges as r (r.id)}<Button variant="secondary" onclick={() => onpick(r.get().from, r.get().to)}>{r.label}</Button>{/each}
</div>

<style>
	.chips {
		display: flex;
		flex-wrap: wrap;
		align-self: flex-end;
		gap: var(--gap-related);
	}
</style>
