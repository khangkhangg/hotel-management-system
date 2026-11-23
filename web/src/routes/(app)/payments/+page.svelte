<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { addDays } from '#lib/booking/dates.ts';
	import { formatMoney } from '#lib/booking/money.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import Section from '#lib/ui/Section.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import StatTile from '#lib/ui/StatTile.svelte';

	type Pay = { id: string; reservationId: string; code: string; guestName: string; method: string; amount: number; status: string; currency: string; createdAt: string };
	type Waiting = { id: string; reservationId: string; code: string; guestName: string; amount: number; reason: string; currency: string };

	const today = session.today;
	let data = $state<{ payments: Pay[]; pendingRefunds: Waiting[]; totals: Record<string, number> }>({ payments: [], pendingRefunds: [], totals: {} });
	let loaded = $state(false);
	let message = $state('');
	const methodWords: Record<string, string> = { cash: 'Cash', card_terminal: 'Card terminal', bank_transfer: 'Bank transfer', other: 'Other' };
	const currency = $derived(data.payments[0]?.currency ?? 'USD');
	const total = $derived(Object.values(data.totals).reduce((a, b) => a + b, 0));
	const canApprove = $derived(session.effectiveRole === 'owner' || session.effectiveRole === 'manager');

	async function load() {
		try {
			data = await api.get(`/api/payments?from=${today}&to=${addDays(today, 1)}`);
			loaded = true;
		} catch (e) {
			message = errorText(e);
		}
	}
	$effect(() => {
		load();
	});

	async function decide(r: Waiting, action: 'approve' | 'reject') {
		message = '';
		try {
			await api.post(`/api/refunds/${r.id}/${action}`);
			await load();
		} catch (e) {
			message = errorText(e);
		}
	}
	const time = (iso: string) => new Intl.DateTimeFormat('en', { timeStyle: 'short' }).format(new Date(iso));
</script>

<svelte:head><title>{t('payments.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('payments.title')} note={t('payments.note')} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

<div class="tiles">
	<StatTile label={t('payments.takenToday')} value={formatMoney(total, currency)} note={t('payments.count', { n: data.payments.length })} />
	{#each Object.entries(data.totals) as [method, amount] (method)}
		<StatTile label={methodWords[method] ?? method} value={formatMoney(amount, currency)} note={t('payments.today')} />
	{/each}
</div>

{#if loaded}
	<Section title={t('payments.waiting')}>
		{#if data.pendingRefunds.length === 0}
			<p class="muted">{t('payments.noRefunds')}</p>
		{:else}
			<ul class="waiting">
				{#each data.pendingRefunds as r (r.id)}
					<li>
						<span>{r.guestName} ({r.code}) {formatMoney(r.amount, r.currency)} {r.reason}</span>
						{#if canApprove}<span class="row-actions"><Button onclick={() => decide(r, 'approve')}>{t('payments.approve')}</Button><Button variant="secondary" onclick={() => decide(r, 'reject')}>{t('payments.reject')}</Button></span>{/if}
					</li>
				{/each}
			</ul>
		{/if}
	</Section>
{/if}

<Section title={t('payments.list')}>
{#if loaded && data.payments.length === 0}
	<p class="muted">{t('payments.empty')}</p>
{:else if data.payments.length}
	<!-- svelte-ignore a11y_no_noninteractive_tabindex -- a scrolling table must be reachable by keyboard -->
	<div class="table-scroll" role="region" aria-label={t('payments.title')} tabindex="0">
		<table class="data-table">
			<thead>
				<tr>
					<th scope="col">{t('payments.time')}</th>
					<th scope="col">{t('arrivals.guest')}</th>
					<th scope="col">{t('panel.method')}</th>
					<th scope="col" class="num">{t('panel.amount')}</th>
					<th scope="col">{t('arrivals.status')}</th>
				</tr>
			</thead>
			<tbody>
				{#each data.payments as p (p.id)}
					<tr>
						<td>{time(p.createdAt)}</td>
						<th scope="row"><a href="/calendar?reservation={p.reservationId}">{p.guestName}</a> <small class="muted">{p.code}</small></th>
						<td>{methodWords[p.method] ?? p.method}</td>
						<td class="num">{formatMoney(p.amount, p.currency)}</td>
						<td>{p.status.replace('_', ' ')}</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}
</Section>

<style>
	.tiles {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(var(--field-min), 1fr));
		gap: var(--gap-group);
	}
	.waiting {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
		font: var(--font-body);
	}
	.waiting li {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		align-items: center;
		gap: var(--gap-related);
		padding: var(--space-3);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
	}
</style>
