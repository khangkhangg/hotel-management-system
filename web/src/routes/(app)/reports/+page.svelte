<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { addDays } from '#lib/booking/dates.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import Field from '#lib/ui/Field.svelte';
	import FormGrid from '#lib/ui/FormGrid.svelte';
	import Section from '#lib/ui/Section.svelte';
	import { lastMonth, monthToDate } from '#lib/booking/span.ts';
	import PageHeader from '#lib/ui/PageHeader.svelte';

	let from = $state(addDays(session.today, -30));
	let to = $state(session.today);
	let message = $state('');
	let done = $state('');
	let busy = $state(false);

	const ranges = [
		{ id: 'month', label: t('reports.thisMonth'), pick: () => ({ from: monthToDate(session.today).from, to: session.today }) },
		{ id: 'last', label: t('reports.lastMonth'), pick: () => lastMonth(session.today) },
		{ id: 'thirty', label: t('reports.last30'), pick: () => ({ from: addDays(session.today, -30), to: session.today }) }
	];
	const use = (r: (typeof ranges)[number]) => ({ from, to } = r.pick());

	async function download(event: SubmitEvent) {
		event.preventDefault();
		message = done = '';
		busy = true;
		try {
			const csv = await api.get<string>(`/api/reports/reservations.csv?from=${from}&to=${to}`);
			const link = document.createElement('a');
			link.href = URL.createObjectURL(new Blob([csv], { type: 'text/csv;charset=utf-8' }));
			link.download = `reservations-${from}-to-${to}.csv`;
			link.click();
			URL.revokeObjectURL(link.href);
			done = t('reports.downloaded');
		} catch (e) {
			message = errorText(e);
		}
		busy = false;
	}
</script>

<svelte:head><title>{t('reports.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('reports.title')} note={t('reports.note')} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
{#if done}<p class="notice success" role="status">{done}</p>{/if}

<Section title={t('reports.reservations')} note={t('reports.reservationsNote')}>
	<form class="stack" onsubmit={download}>
		<div class="chips" role="group" aria-label={t('reports.quick')}>
			{#each ranges as r (r.id)}<Button variant="secondary" onclick={() => use(r)}>{r.label}</Button>{/each}
		</div>
		<FormGrid>
			<Field label={t('reports.from')} type="date" bind:value={from} />
			<Field label={t('reports.to')} type="date" bind:value={to} />
		</FormGrid>
		<div><Button type="submit" disabled={busy || !from || !to || to < from}>{t('reports.download')}</Button></div>
	</form>
</Section>

<style>
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: var(--gap-related);
	}
</style>
