<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { formatDay } from './dates.ts';
	import { formatMoney, parseMoney } from './money.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import Field from '#lib/ui/Field.svelte';
	import Select from '#lib/ui/Select.svelte';
	import StatusBadge from '#lib/ui/StatusBadge.svelte';
	import CancelRisk from './CancelRisk.svelte';
	import Thread from './Thread.svelte';
	import type { Folio, Reservation, Room } from './types.ts';

	type Props = { id: string | null; onclose: () => void; onchange?: () => void };
	let { id, onclose, onchange }: Props = $props();

	const tones = { pending: 'warning', confirmed: 'info', checked_in: 'success', checked_out: 'neutral', cancelled: 'neutral', no_show: 'danger' } as const;
	const statusWords = { pending: 'Pending', confirmed: 'Confirmed', checked_in: 'Checked in', checked_out: 'Checked out', cancelled: 'Cancelled', no_show: 'No-show' } as const;
	const methods = [
		{ id: 'cash', label: 'Cash' },
		{ id: 'card_terminal', label: 'Card terminal' },
		{ id: 'bank_transfer', label: 'Bank transfer' },
		{ id: 'other', label: 'Other' }
	];

	let open = $state(false);
	let res = $state<Reservation | null>(null);
	let folio = $state<Folio | null>(null);
	let rooms = $state<Room[]>([]);
	let message = $state('');
	let ok = $state('');
	let pick = $state<Record<string, string>>({});
	let method = $state('cash');
	let amount = $state('');
	let note = $state('');
	let reason = $state('');
	let version = $state(0);

	$effect(() => {
		open = id !== null;
		if (id) load(id);
		else (res = null), (folio = null);
	});

	async function load(reservation: string) {
		message = '';
		try {
			res = await api.get<Reservation>(`/api/reservations/${reservation}`);
			folio = await api.get<Folio>(`/api/reservations/${reservation}/folio`);
			rooms = await api.get<Room[]>('/api/rooms');
			extras = await api.get<Extra[]>('/api/extras').catch(() => []);
			extraId = extras[0]?.id ?? '';
			version++;
		} catch (e) {
			message = errorText(e);
		}
	}

	async function run(path: string, body?: unknown, done = '') {
		message = ok = '';
		try {
			await api.post(`/api/reservations/${id}/${path}`, body);
			ok = done;
			await load(id!);
			onchange?.();
		} catch (e) {
			message = errorText(e);
		}
	}

	type Extra = { id: string; name: string; unit: string; price: number };
	let extras = $state<Extra[]>([]);
	let extraId = $state('');
	let extraQty = $state('1');

	async function sellExtra(event: SubmitEvent) {
		event.preventDefault();
		message = ok = '';
		try {
			await api.post(`/api/reservations/${id}/extras`, { extraId, quantity: Number(extraQty) });
			ok = t('panel.extraAdded');
			extraQty = '1';
			await load(id!);
			onchange?.();
		} catch (e) {
			message = errorText(e);
		}
	}

	async function takePayment(event: SubmitEvent) {
		event.preventDefault();
		message = ok = '';
		const cents = parseMoney(amount, res?.currency ?? 'USD');
		if (cents === null) {
			message = t('panel.badAmount');
			return;
		}
		try {
			await api.post(`/api/folios/${folio!.folioId}/payments`, { method, amount: cents, note });
			amount = note = '';
			ok = t('panel.paymentTaken');
			await load(id!);
			onchange?.();
		} catch (e) {
			message = errorText(e);
		}
	}

	async function refund(paymentId: string, cents: number) {
		message = ok = '';
		try {
			const r = await api.post<{ status: string }>(`/api/payments/${paymentId}/refunds`, { amount: cents, reason });
			ok = r.status === 'done' ? t('panel.refunded') : t('panel.refundWaits');
			reason = '';
			await load(id!);
		} catch (e) {
			message = errorText(e);
		}
	}

	const typeRooms = (typeId: string) => rooms.filter((r) => r.roomTypeId === typeId && r.status !== 'out_of_order').map((r) => ({ id: r.id, label: `${r.number} (${r.status})` }));
	const money = (n: number) => formatMoney(n, res?.currency ?? 'USD');
	const canMoney = $derived(session.effectiveRole !== 'revenue');
</script>

<Dialog title={res ? `${res.guest.name} · ${res.code}` : t('panel.loading')} wide bind:open onclose={onclose}>
	{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
	{#if ok}<p class="notice success" role="status">{ok}</p>{/if}

	{#if res}
		<section class="block" aria-label={t('panel.booking')}>
			<p class="line">
				<StatusBadge tone={tones[res.status]}>{statusWords[res.status]}</StatusBadge>
				<span class="muted">{t('panel.source', { source: res.source })}</span>
			</p>
			{#if res.guest.email || res.guest.phone}
				<p class="muted">{res.guest.email ?? ''} {res.guest.phone ?? ''}</p>
			{/if}
			{#each res.rooms as room (room.id)}
				<div class="stay">
					<p class="head">{room.roomTypeName}, {formatDay(room.from)} to {formatDay(room.to)} ({room.nights.length === 1 ? '1 night' : `${room.nights.length} nights`}), {room.adults} adults{room.children ? `, ${room.children} children` : ''}</p>
					{#if room.roomNumber}
						<p class="muted">{t('panel.room', { number: room.roomNumber })}</p>
					{:else if res.status === 'confirmed' || res.status === 'pending'}
						<div class="assign">
							<Select label={t('panel.chooseRoom')} options={[{ id: '', label: t('panel.noRoomYet') }, ...typeRooms(room.roomTypeId)]} bind:value={pick[room.id]} />
							<Button variant="secondary" disabled={!pick[room.id]} onclick={() => run('assign', { roomId: pick[room.id], reservationRoomId: room.id }, t('panel.assigned'))}>{t('panel.assign')}</Button>
						</div>
					{/if}
				</div>
			{/each}
			{#if (res.status === 'confirmed' || res.status === 'pending') && id}<CancelRisk {id} />{/if}
			<p class="actions">
				{#if res.status === 'confirmed'}
					<Button onclick={() => run('check-in', undefined, t('panel.checkedIn'))}>{t('panel.checkIn')}</Button>
					<Button variant="secondary" onclick={() => run('no-show', undefined, t('panel.noShown'))}>{t('panel.noShow')}</Button>
				{/if}
				{#if res.status === 'pending'}<Button onclick={() => run('confirm-payment', undefined, t('panel.paymentConfirmed'))}>{t('panel.confirmPayment')}</Button>{/if}
				{#if res.status === 'checked_in'}<Button onclick={() => run('check-out', undefined, t('panel.checkedOut'))}>{t('panel.checkOut')}</Button>{/if}
				{#if res.status === 'confirmed' || res.status === 'pending'}<Button variant="quiet" onclick={() => run('cancel', undefined, t('panel.cancelled'))}>{t('panel.cancel')}</Button>{/if}
			</p>
		</section>

		{#if folio && canMoney}
			<section class="block" aria-label={t('panel.folio')}>
				<h3>{t('panel.folio')}</h3>
				<table>
					<tbody>
						{#each folio.lines as line (line.id)}
							<tr><td>{line.description}{line.quantity > 1 ? ` × ${line.quantity}` : ''}</td><td class="num">{money(line.amount)}</td></tr>
						{/each}
					</tbody>
					<tfoot>
						<tr><th scope="row">{t('panel.total')}</th><td class="num">{money(folio.total)}</td></tr>
						<tr><th scope="row">{t('panel.paid')}</th><td class="num">{money(folio.paid)}</td></tr>
						{#if folio.refunded}<tr><th scope="row">{t('panel.refundedTotal')}</th><td class="num">{money(folio.refunded)}</td></tr>{/if}
						<tr class="due"><th scope="row">{t('panel.balance')}</th><td class="num">{money(folio.balance)}</td></tr>
					</tfoot>
				</table>

				{#each folio.payments as p (p.id)}
					<div class="pay">
						<span>{methods.find((m) => m.id === p.method)?.label} {money(p.amount)}{p.note ? `, ${p.note}` : ''}</span>
						<StatusBadge tone={p.status === 'paid' ? 'success' : 'neutral'}>{p.status.replace('_', ' ')}</StatusBadge>
						{#each p.refunds as r (r.id)}<small class="muted">{t('panel.refundLine', { amount: money(r.amount), status: r.status })}</small>{/each}
					</div>
				{/each}

				{#if extras.length}
					<form class="take" onsubmit={sellExtra}>
						<Select label={t('panel.extra')} options={extras.map((x) => ({ id: x.id, label: `${x.name} · ${money(x.price)}` }))} bind:value={extraId} />
						<Field label={t('panel.extraQty')} inputmode="numeric" bind:value={extraQty} />
						<Button type="submit" variant="secondary" disabled={!Number(extraQty)}>{t('panel.addExtra')}</Button>
					</form>
				{/if}
				<form class="take" onsubmit={takePayment}>
					<Select label={t('panel.method')} options={methods} bind:value={method} />
					<Field label={t('panel.amount')} inputmode="decimal" bind:value={amount} />
					<Field label={t('panel.note')} bind:value={note} />
					<Button type="submit" disabled={amount.trim() === ''}>{t('panel.takePayment')}</Button>
				</form>
				{#if folio.payments.length}
					<details>
						<summary>{t('panel.refund')}</summary>
						<Field label={t('panel.refundReason')} bind:value={reason} />
						<p class="actions">
							{#each folio.payments as p (p.id)}
								<Button variant="secondary" disabled={reason.trim() === ''} onclick={() => refund(p.id, p.amount - p.refunds.filter((r) => r.status !== 'rejected').reduce((a, r) => a + r.amount, 0))}>{t('panel.refundRest', { amount: money(p.amount) })}</Button>
							{/each}
						</p>
					</details>
				{/if}
			</section>
		{/if}

		<Thread reservationId={res.id} {version} />
	{/if}
</Dialog>

<style>
	.block {
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
	}
	h3 {
		margin: 0;
		font: var(--font-heading);
	}
	p {
		margin: 0;
	}
	.line,
	.actions {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--gap-related);
	}
	.muted {
		color: var(--color-text-muted);
		font: var(--font-body);
	}
	.head {
		font: var(--font-control);
	}
	.stay {
		padding: var(--space-3);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		display: flex;
		flex-direction: column;
		gap: var(--gap-tight);
	}
	.assign {
		display: grid;
		grid-template-columns: 1fr auto;
		align-items: end;
		gap: var(--gap-related);
	}
	table {
		inline-size: 100%;
		border-collapse: collapse;
		font: var(--font-body);
	}
	td,
	th {
		padding: var(--space-1) 0;
		text-align: start;
		font-weight: var(--font-weight-regular);
	}
	.num {
		text-align: end;
		font-variant-numeric: tabular-nums;
	}
	tfoot tr:first-child > * {
		border-block-start: var(--border-width) solid var(--color-divider);
	}
	.due > * {
		font: var(--font-control);
	}
	.pay {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--gap-related);
		font: var(--font-body);
	}
	.take {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(var(--field-min), 1fr));
		align-items: end;
		gap: var(--gap-related);
	}
	summary {
		cursor: pointer;
		font: var(--font-control);
	}
</style>
