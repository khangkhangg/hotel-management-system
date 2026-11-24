<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, errorText } from '#lib/api.ts';
	import { addDays } from '#lib/booking/dates.ts';
	import { formatMoney } from '#lib/booking/money.ts';
	import ReservationPanel from '#lib/booking/ReservationPanel.svelte';
	import type { CalendarData, Stay } from '#lib/booking/types.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import AttentionPanel from '#lib/alerts/AttentionPanel.svelte';
	import DataTable from '#lib/ui/DataTable.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import StatusBadge from '#lib/ui/StatusBadge.svelte';

	const today = session.today;
	let data = $state<CalendarData>({ rooms: [], stays: [] });
	let loaded = $state(false);
	let message = $state('');

	const arrivals = $derived(data.stays.filter((s) => s.from === today && (s.status === 'confirmed' || s.status === 'pending')));
	const departures = $derived(data.stays.filter((s) => s.to === today && s.status === 'checked_in'));
	const inHouse = $derived(data.stays.filter((s) => s.status === 'checked_in' && s.from <= today && s.to > today));
	const panelId = $derived(page.url.searchParams.get('reservation'));
	const statusWord = (v: string) => { const w = v.replace('_', ' '); return w.charAt(0).toLocaleUpperCase('en') + w.slice(1); };
	const roomNumber = (s: Stay) => data.rooms.find((r) => r.id === s.roomId)?.number;

	const columns = [
		{ id: 'guest', label: t('arrivals.guest'), value: (s: Stay) => s.guestName, priority: 'main' as const },
		{ id: 'room', label: t('arrivals.room'), value: (s: Stay) => roomNumber(s) ?? '', priority: 'detail' as const },
		{ id: 'status', label: t('arrivals.status'), value: (s: Stay) => statusWord(s.status), priority: 'status' as const },
		{ id: 'total', label: t('arrivals.total'), value: (s: Stay) => formatMoney(s.total, s.currency), priority: 'extra' as const, align: 'end' as const },
		{ id: 'actions', label: t('staff.col.actions'), value: () => '', priority: 'detail' as const, align: 'end' as const, hideLabel: true }
	];

	async function load() {
		try {
			data = await api.get<CalendarData>(`/api/calendar?from=${addDays(today, -1)}&to=${addDays(today, 1)}`);
			loaded = true;
		} catch (e) {
			message = errorText(e);
		}
	}
	$effect(() => {
		load();
	});

	async function act(s: Stay, action: 'check-in' | 'check-out') {
		message = '';
		try {
			await api.post(`/api/reservations/${s.reservationId}/${action}`);
			await load();
		} catch (e) {
			message = errorText(e);
		}
	}
	const open = (id: string | null) => goto(id ? `?reservation=${id}` : page.url.pathname);
</script>

<svelte:head><title>{t('arrivals.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('arrivals.title')} note={t('arrivals.note', { arrivals: arrivals.length, departures: departures.length, inHouse: inHouse.length })} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

<AttentionPanel onchange={load} />

{#each [{ key: 'arrivals', rows: arrivals }, { key: 'departures', rows: departures }] as group (group.key)}
	<section class="stack-tight" aria-label={t(`arrivals.${group.key}`)}>
		<h2>{t(`arrivals.${group.key}`)}</h2>
		<DataTable label={t(`arrivals.${group.key}`)} rows={group.rows} {columns} rowKey={(s) => s.id} onopen={(s) => open(s.reservationId)}>
			{#snippet cell(s, column)}
				{#if column.id === 'room'}
					{#if roomNumber(s)}{roomNumber(s)}{:else}<StatusBadge tone="warning">{t('arrivals.noRoom')}</StatusBadge>{/if}
				{:else if column.id === 'status'}
					<StatusBadge tone={s.status === 'checked_in' ? 'success' : 'info'}>{statusWord(s.status)}</StatusBadge>
				{:else if column.id === 'actions'}
					<span class="row-actions">
						{#if group.key === 'arrivals'}
							{#if s.roomId}<Button onclick={() => act(s, 'check-in')}>{t('panel.checkIn')}</Button>{:else}<Button variant="secondary" onclick={() => open(s.reservationId)}>{t('panel.assign')}</Button>{/if}
						{:else}
							<Button onclick={() => act(s, 'check-out')}>{t('panel.checkOut')}</Button>
						{/if}
					</span>
				{:else}
					{column.value(s)}
				{/if}
			{/snippet}
			{#snippet empty()}
				{#if loaded}<p class="muted">{t(`arrivals.no.${group.key}`)}</p>{/if}
			{/snippet}
		</DataTable>
	</section>
{/each}

<ReservationPanel id={panelId} onclose={() => open(null)} onchange={load} />

<style>
	h2 {
		margin: 0;
		font: var(--font-heading);
	}
</style>
