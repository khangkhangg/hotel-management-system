<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { formatDay } from '#lib/booking/dates.ts';
	import type { Room } from '#lib/booking/types.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Select from '#lib/ui/Select.svelte';
	import StatusBadge from '#lib/ui/StatusBadge.svelte';

	type Ticket = { id: string; roomId: string; roomNumber: string; floor: number; description: string; status: string; blocksFrom: string | null; blocksTo: string | null; resolution: string };

	let tickets = $state<Ticket[]>([]);
	let rooms = $state<Room[]>([]);
	let loaded = $state(false);
	let message = $state('');
	let ok = $state('');
	let open = $state(false);
	let room = $state('');
	let description = $state('');
	let blocksFrom = $state('');
	let blocksTo = $state('');
	let closing = $state<Ticket | null>(null);
	let closeOpen = $state(false);
	let resolution = $state('');

	const canWrite = $derived(session.effectiveRole === 'owner' || session.effectiveRole === 'manager' || session.effectiveRole === 'housekeeping');

	async function load() {
		try {
			[tickets, rooms] = await Promise.all([api.get<Ticket[]>('/api/maintenance-tickets'), api.get<Room[]>('/api/rooms')]);
			room = room || rooms[0]?.id || '';
			loaded = true;
		} catch (e) {
			message = errorText(e);
		}
	}
	$effect(() => {
		load();
	});

	async function create(event: SubmitEvent) {
		event.preventDefault();
		message = ok = '';
		try {
			const r = await api.post<{ clashes: string[] }>('/api/maintenance-tickets', {
				roomId: room,
				description,
				...(blocksFrom && blocksTo ? { blocksFrom, blocksTo } : {})
			});
			ok = r.clashes.length ? t('issues.clash', { n: r.clashes.length }) : t('issues.opened');
			open = false;
			description = blocksFrom = blocksTo = '';
			await load();
		} catch (e) {
			message = errorText(e);
		}
	}

	async function close(event: SubmitEvent) {
		event.preventDefault();
		message = '';
		try {
			await api.patch(`/api/maintenance-tickets/${closing!.id}`, { status: 'done', resolution });
			closeOpen = false;
			resolution = '';
			await load();
		} catch (e) {
			message = errorText(e);
		}
	}
</script>

<svelte:head><title>{t('issues.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('issues.title')} note={t('issues.note')}>
	{#snippet primary()}{#if canWrite}<Button icon="plus" disabled={rooms.length === 0} onclick={() => (open = true)}>{t('issues.new')}</Button>{/if}{/snippet}
</PageHeader>
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
{#if ok}<p class="notice success" role="status">{ok}</p>{/if}

{#if loaded && tickets.length === 0}
	<p class="muted">{t('issues.empty')}</p>
{:else if tickets.length}
	<!-- svelte-ignore a11y_no_noninteractive_tabindex -- a scrolling table must be reachable by keyboard -->
	<div class="table-scroll" role="region" aria-label={t('issues.title')} tabindex="0">
		<table class="data-table">
			<thead>
				<tr>
					<th scope="col">{t('rooms.number')}</th>
					<th scope="col">{t('issues.problem')}</th>
					<th scope="col">{t('arrivals.status')}</th>
					<th scope="col">{t('issues.blocks')}</th>
					<th scope="col"><span class="visually-hidden">{t('staff.col.actions')}</span></th>
				</tr>
			</thead>
			<tbody>
				{#each tickets as ticket (ticket.id)}
					<tr>
						<th scope="row">{ticket.roomNumber}</th>
						<td class="wrap">{ticket.description}{ticket.resolution ? ` — ${ticket.resolution}` : ''}</td>
						<td><StatusBadge tone={ticket.status === 'done' ? 'success' : 'warning'}>{ticket.status.replace('_', ' ')}</StatusBadge></td>
						<td>{ticket.blocksFrom && ticket.blocksTo ? `${formatDay(ticket.blocksFrom)} to ${formatDay(ticket.blocksTo)}` : ''}</td>
						<td class="actions">
							{#if canWrite && ticket.status !== 'done'}<Button variant="secondary" onclick={() => ((closing = ticket), (closeOpen = true))}>{t('issues.close')}</Button>{/if}
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}

<Dialog title={t('issues.new')} bind:open>
	<form id="ticket-form" class="stack" onsubmit={create}>
		<Select label={t('rooms.number')} options={rooms.map((r) => ({ id: r.id, label: `${r.number} (${r.roomTypeName})` }))} bind:value={room} />
		<Field label={t('issues.problem')} bind:value={description} />
		<p class="muted">{t('issues.blockHelp')}</p>
		<div class="pair">
			<Field label={t('issues.blockFrom')} type="date" bind:value={blocksFrom} />
			<Field label={t('issues.blockTo')} type="date" bind:value={blocksTo} />
		</div>
	</form>
	{#snippet actions()}
		<Button variant="quiet" onclick={() => (open = false)}>{t('common.cancel')}</Button>
		<Button type="submit" form="ticket-form" disabled={description.trim() === ''}>{t('issues.create')}</Button>
	{/snippet}
</Dialog>

<Dialog title={t('issues.closeTitle', { room: closing?.roomNumber ?? '' })} bind:open={closeOpen}>
	<form id="close-form" class="stack" onsubmit={close}>
		<Field label={t('issues.resolution')} bind:value={resolution} />
	</form>
	{#snippet actions()}
		<Button variant="quiet" onclick={() => (closeOpen = false)}>{t('common.cancel')}</Button>
		<Button type="submit" form="close-form">{t('issues.close')}</Button>
	{/snippet}
</Dialog>

<style>
	.wrap {
		white-space: normal;
		min-inline-size: var(--form-width);
	}
	.pair {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--gap-related);
	}
	p {
		margin: 0;
	}
</style>
