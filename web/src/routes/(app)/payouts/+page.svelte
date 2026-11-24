<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { formatMoney } from '#lib/booking/money.ts';
	import { t } from '#lib/i18n.ts';
	import QuickRanges from '#lib/ui/QuickRanges.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';

	type Row = { date: string; provider: string; currency: string; payments: number; gross: number; refunds: number; net: number };

	const iso = (d: Date) => d.toISOString().slice(0, 10);
	let from = $state(iso(new Date(Date.now() - 29 * 86400000)));
	let to = $state(iso(new Date()));
	let rows = $state<Row[]>([]);
	let loaded = $state(false);
	let message = $state('');

	async function load() {
		message = '';
		try {
			rows = await api.get<Row[]>(`/api/payouts?from=${from}&to=${to}`);
			loaded = true;
		} catch (e) {
			message = errorText(e);
		}
	}

	$effect(() => {
		from;
		to;
		load();
	});

	const total = $derived(rows.reduce((n, r) => n + r.net, 0));
	const currency = $derived(rows[0]?.currency ?? 'USD');
</script>

<svelte:head><title>{t('payouts.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('payouts.title')} note={t('payouts.note')} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

<section class="card">
	<div class="row">
		<QuickRanges onpick={(f, t2) => ((from = f), (to = t2))} />
		<Field label={t('payouts.from')} type="date" bind:value={from} />
		<Field label={t('payouts.to')} type="date" bind:value={to} />
	</div>
	{#if loaded}
		{#if rows.length === 0}
			<p class="muted">{t('payouts.none')}</p>
		{:else}
			<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
			<div class="scroll" role="region" aria-label={t('payouts.title')} tabindex="0">
				<table>
					<thead>
						<tr><th scope="col">{t('payouts.date')}</th><th scope="col">{t('payouts.provider')}</th><th scope="col">{t('payouts.payments')}</th><th scope="col">{t('payouts.gross')}</th><th scope="col">{t('payouts.refunds')}</th><th scope="col">{t('payouts.net')}</th></tr>
					</thead>
					<tbody>
						{#each rows as r (r.date + r.provider)}
							<tr><td>{r.date}</td><td>{r.provider}</td><td>{r.payments}</td><td>{formatMoney(r.gross, r.currency)}</td><td>{formatMoney(r.refunds, r.currency)}</td><td>{formatMoney(r.net, r.currency)}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
			<p><strong>{t('payouts.total', { amount: formatMoney(total, currency) })}</strong></p>
		{/if}
	{/if}
</section>

<style>
	p {
		margin: 0;
	}
	.card {
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
		padding: var(--space-4);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
		font: var(--font-body);
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: var(--gap-related);
	}
	.scroll {
		overflow-x: auto;
	}
	table {
		inline-size: 100%;
		border-collapse: collapse;
		font-variant-numeric: tabular-nums;
	}
	th,
	td {
		padding: var(--space-2) var(--space-3);
		text-align: start;
		border-block-end: var(--border-width) solid var(--color-border);
	}
	.muted {
		color: var(--color-text-muted);
	}
</style>
