<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, errorText } from '#lib/api.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import BookingDialog from '#lib/booking/BookingDialog.svelte';
	import CalendarGrid from '#lib/booking/CalendarGrid.svelte';
	import ReservationPanel from '#lib/booking/ReservationPanel.svelte';
	import { addDays, formatDay, rangeDays } from '#lib/booking/dates.ts';
	import type { CalendarData, Stay } from '#lib/booking/types.ts';
	import Button from '#lib/ui/Button.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';

	const DAYS = 14;
	const today = new Date().toISOString().slice(0, 10);

	let start = $state(today);
	let data = $state<CalendarData>({ rooms: [], stays: [] });
	let unread = $state<string[]>([]);
	let message = $state('');
	let loaded = $state(false);
	let newOpen = $state(false);
	let newFrom = $state(today);

	const days = $derived(rangeDays(start, DAYS));
	const panelId = $derived(page.url.searchParams.get('reservation'));
	const unassigned = $derived(data.stays.filter((s) => s.roomId === null && (s.status === 'confirmed' || s.status === 'pending')));
	const canBook = $derived(session.effectiveRole === 'front_desk' || session.effectiveRole === 'manager' || session.effectiveRole === 'owner');

	async function load() {
		try {
			[data] = await Promise.all([api.get<CalendarData>(`/api/calendar?from=${start}&to=${addDays(start, DAYS)}`)]);
			unread = (await api.get<{ reservationIds: string[] }>('/api/notes/unread').catch(() => ({ reservationIds: [] }))).reservationIds;
			loaded = true;
		} catch (e) {
			message = errorText(e);
		}
	}

	$effect(() => {
		start; // reload when the range moves
		load();
	});

	const open = (id: string | null) => goto(id ? `?reservation=${id}` : page.url.pathname);
	function newBooking(from: string) {
		newFrom = from;
		newOpen = true;
	}
</script>

<svelte:head><title>{t('calendar.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('calendar.title')} note={t('calendar.note', { from: formatDay(days[0]), to: formatDay(days[DAYS - 1]) })}>
	{#snippet secondary()}
		<Button variant="quiet" icon="left" iconOnly onclick={() => (start = addDays(start, -7))}>{t('calendar.earlier')}</Button>
		<Button variant="secondary" onclick={() => (start = today)}>{t('calendar.today')}</Button>
		<Button variant="quiet" icon="right" iconOnly onclick={() => (start = addDays(start, 7))}>{t('calendar.later')}</Button>
	{/snippet}
	{#snippet primary()}
		{#if canBook}<Button icon="plus" onclick={() => newBooking(today)}>{t('booking.new')}</Button>{/if}
	{/snippet}
</PageHeader>

{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

{#if unassigned.length}
	<section class="tray" aria-label={t('calendar.unassigned')}>
		<h2>{t('calendar.unassigned')}</h2>
		<ul>
			{#each unassigned as s (s.id)}
				<li><Button variant="secondary" onclick={() => open(s.reservationId)}>{s.guestName}, {formatDay(s.from)} to {formatDay(s.to)}</Button></li>
			{/each}
		</ul>
	</section>
{/if}

{#if loaded && data.rooms.length === 0}
	<p class="muted">{t('calendar.empty')}</p>
{:else}
	<CalendarGrid {days} rooms={data.rooms} stays={data.stays} {today} {unread} onpick={(s: Stay) => open(s.reservationId)} onday={newBooking} />
{/if}

<ReservationPanel id={panelId} onclose={() => open(null)} onchange={load} />
<BookingDialog bind:open={newOpen} from={newFrom} onsaved={(id) => (load(), open(id))} />

<style>
	.tray {
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
	}
	h2 {
		margin: 0;
		font: var(--font-heading);
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-wrap: wrap;
		gap: var(--gap-related);
	}
</style>
