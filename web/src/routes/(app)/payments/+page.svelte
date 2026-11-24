<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { addDays } from '#lib/booking/dates.ts';
	import { formatMoney } from '#lib/booking/money.ts';
	import { goto } from '$app/navigation';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import DataTable from '#lib/ui/DataTable.svelte';
	import Section from '#lib/ui/Section.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import StatTile from '#lib/ui/StatTile.svelte';
	import StatusBadge from '#lib/ui/StatusBadge.svelte';

	type Pay = { id: string; reservationId: string; code: string; guestName: string; method: string; amount: number; status: string; currency: string; createdAt: string };
	type Waiting = { id: string; reservationId: string; code: string; guestName: string; amount: number; reason: string; currency: string };

	const today = session.today;
	let data = $state<{ payments: Pay[]; pendingRefunds: Waiting[]; totals: Record<string, number> }>({ payments: [], pendingRefunds: [], totals: {} });
	let loaded = $state(false);
	let yesterday = $state<number | null>(null);
	let message = $state('');
	const methodWords: Record<string, string> = { cash: 'Cash', card_terminal: 'Card terminal', bank_transfer: 'Bank transfer', other: 'Other' };
	const currency = $derived(data.payments[0]?.currency ?? 'USD');
	const total = $derived(Object.values(data.totals).reduce((a, b) => a + b, 0));
	const canApprove = $derived(session.effectiveRole === 'owner' || session.effectiveRole === 'manager');

	async function load() {
		try {
			data = await api.get(`/api/payments?from=${today}&to=${addDays(today, 1)}`);
			loaded = true;
			// The headline number says what it is compared with.
			const before = await api.get<typeof data>(`/api/payments?from=${addDays(today, -1)}&to=${today}`);
			yesterday = Object.values(before.totals).reduce((a, b) => a + b, 0);
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
	const word = (v: string) => { const w = v.replace('_', ' '); return w.charAt(0).toLocaleUpperCase('en') + w.slice(1); };
	const refundColumns = [
		{ id: 'guest', label: t('arrivals.guest'), value: (r: Waiting) => `${r.guestName} (${r.code})`, priority: 'main' as const },
		{ id: 'amount', label: t('panel.amount'), value: (r: Waiting) => formatMoney(r.amount, r.currency), priority: 'status' as const, align: 'end' as const },
		{ id: 'reason', label: t('payments.reason'), value: (r: Waiting) => r.reason, priority: 'detail' as const },
		{ id: 'actions', label: t('staff.col.actions'), value: () => '', priority: 'detail' as const, align: 'end' as const, hideLabel: true }
	];
	const paymentColumns = [
		{ id: 'guest', label: t('arrivals.guest'), value: (p: Pay) => p.guestName, priority: 'main' as const },
		{ id: 'amount', label: t('panel.amount'), value: (p: Pay) => formatMoney(p.amount, p.currency), priority: 'status' as const, align: 'end' as const },
		{ id: 'method', label: t('panel.method'), value: (p: Pay) => `${methodWords[p.method] ?? p.method} · ${p.code}`, priority: 'detail' as const },
		{ id: 'time', label: t('payments.time'), value: (p: Pay) => time(p.createdAt), priority: 'extra' as const },
		{ id: 'status', label: t('arrivals.status'), value: (p: Pay) => word(p.status), priority: 'extra' as const }
	];
	const time = (iso: string) => new Intl.DateTimeFormat('en', { timeStyle: 'short' }).format(new Date(iso));
</script>

<svelte:head><title>{t('payments.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('payments.title')} note={t('payments.note')} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

<div class="tiles">
	<StatTile label={t('payments.takenToday')} value={formatMoney(total, currency)} note={yesterday === null ? t('payments.count', { n: data.payments.length }) : `${t('payments.count', { n: data.payments.length })} · ${t('payments.vsYesterday', { amount: formatMoney(yesterday, currency) })}`} />
	{#each Object.entries(data.totals) as [method, amount] (method)}
		<StatTile label={methodWords[method] ?? method} value={formatMoney(amount, currency)} note={t('payments.today')} />
	{/each}
</div>

{#if loaded}
	<Section title={t('payments.waiting')}>
		<DataTable label={t('payments.waiting')} rows={data.pendingRefunds} columns={refundColumns} rowKey={(r) => r.id}>
			{#snippet cell(r, column)}
				{#if column.id === 'actions'}
					{#if canApprove}
						<span class="row-actions">
							<Button onclick={() => decide(r, 'approve')}>{t('payments.approve')}</Button>
							<Button variant="secondary" onclick={() => decide(r, 'reject')}>{t('payments.reject')}</Button>
						</span>
					{/if}
				{:else}
					{column.value(r)}
				{/if}
			{/snippet}
			{#snippet empty()}<p class="muted">{t('payments.noRefunds')}</p>{/snippet}
		</DataTable>
	</Section>

	<Section title={t('payments.list')}>
		<DataTable label={t('payments.list')} rows={data.payments} columns={paymentColumns} rowKey={(p) => p.id} onopen={(p) => goto(`/calendar?reservation=${p.reservationId}`)}>
			{#snippet cell(p, column)}
				{#if column.id === 'status'}
					<StatusBadge tone={p.status === 'paid' ? 'success' : 'neutral'}>{column.value(p)}</StatusBadge>
				{:else}
					{column.value(p)}
				{/if}
			{/snippet}
			{#snippet empty()}<p class="muted">{t('payments.empty')}</p>{/snippet}
		</DataTable>
	</Section>
{/if}

<style>
	.tiles {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(var(--field-min), 1fr));
		gap: var(--gap-group);
	}
	.muted {
		margin: 0;
		color: var(--color-text-muted);
	}
</style>
