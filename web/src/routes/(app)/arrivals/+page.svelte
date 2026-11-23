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
	const roomNumber = (s: Stay) => data.rooms.find((r) => r.id === s.roomId)?.number;

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
		{#if loaded && group.rows.length === 0}
			<p class="muted">{t(`arrivals.no.${group.key}`)}</p>
		{:else if group.rows.length}
			<!-- svelte-ignore a11y_no_noninteractive_tabindex -- a scrolling table must be reachable by keyboard -->
			<div class="table-scroll" role="region" aria-label={t('arrivals.title')} tabindex="0">
				<table class="data-table">
					<thead>
						<tr>
							<th scope="col">{t('arrivals.guest')}</th>
							<th scope="col">{t('arrivals.room')}</th>
							<th scope="col">{t('arrivals.status')}</th>
							<th scope="col" class="num">{t('arrivals.total')}</th>
							<th scope="col"><span class="visually-hidden">{t('staff.col.actions')}</span></th>
						</tr>
					</thead>
					<tbody>
						{#each group.rows as s (s.id)}
							<tr>
								<th scope="row">{s.guestName} <small class="muted">{s.code}</small></th>
								<td>{roomNumber(s) ?? t('arrivals.noRoom')}</td>
								<td><StatusBadge tone={s.status === 'checked_in' ? 'success' : 'info'}>{s.status.replace('_', ' ')}</StatusBadge></td>
								<td class="num">{formatMoney(s.total, s.currency)}</td>
								<td class="actions">
									<span class="row-actions">
										{#if group.key === 'arrivals'}
											{#if s.roomId}<Button onclick={() => act(s, 'check-in')}>{t('panel.checkIn')}</Button>{:else}<Button variant="secondary" onclick={() => open(s.reservationId)}>{t('panel.assign')}</Button>{/if}
										{:else}
											<Button onclick={() => act(s, 'check-out')}>{t('panel.checkOut')}</Button>
										{/if}
										<Button variant="quiet" onclick={() => open(s.reservationId)}>{t('arrivals.open')}</Button>
									</span>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>
{/each}

<ReservationPanel id={panelId} onclose={() => open(null)} onchange={load} />

<style>
	h2 {
		margin: 0;
		font: var(--font-heading);
	}
	small {
		font: var(--font-caption);
	}
</style>
