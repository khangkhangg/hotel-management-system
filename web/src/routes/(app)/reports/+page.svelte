<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { addDays } from '#lib/booking/dates.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';

	let from = $state(addDays(session.today, -30));
	let to = $state(session.today);
	let message = $state('');
	let done = $state('');
	let busy = $state(false);

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

<form class="card" onsubmit={download}>
	<h2>{t('reports.reservations')}</h2>
	<p class="muted">{t('reports.reservationsNote')}</p>
	<Field label={t('reports.from')} type="date" bind:value={from} />
	<Field label={t('reports.to')} type="date" bind:value={to} />
	<div><Button type="submit" disabled={busy || !from || !to || to < from}>{t('reports.download')}</Button></div>
</form>

<style>
	h2 {
		margin: 0;
		font: var(--font-heading);
	}
	p {
		margin: 0;
	}
	.card {
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
		max-inline-size: var(--page-width);
		padding: var(--space-4);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
		font: var(--font-body);
	}
	.muted {
		color: var(--color-text-muted);
	}
</style>
