<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, errorText } from '#lib/api.ts';
	import { t } from '#lib/i18n.ts';
	import Button from '#lib/ui/Button.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import Field from '#lib/ui/Field.svelte';
	import Select from '#lib/ui/Select.svelte';
	import { addDays } from './dates.ts';
	import { formatMoney } from './money.ts';
	import type { RatePlan, RoomType } from './types.ts';

	type Props = { open: boolean; from: string; onsaved: (reservationId: string) => void };
	let { open = $bindable(false), from, onsaved }: Props = $props();

	type Quote = { nights: number; total: number; currency: string; available: boolean };

	let types = $state<RoomType[]>([]);
	let plans = $state<RatePlan[]>([]);
	let arrival = $state('');
	let departure = $state('');
	let roomType = $state('');
	let ratePlan = $state('');
	let adults = $state('2');
	let children = $state('0');
	let name = $state('');
	let email = $state('');
	let phone = $state('');
	let quote = $state<Quote | null>(null);
	let quoteError = $state('');
	let message = $state('');
	let saving = $state(false);

	onMount(async () => {
		[types, plans] = await Promise.all([api.get<RoomType[]>('/api/room-types'), api.get<RatePlan[]>('/api/rate-plans')]).catch(() => [[], []] as [RoomType[], RatePlan[]]);
	});

	// Each time the dialog opens it starts from the day the person picked.
	$effect(() => {
		if (open) {
			arrival = from;
			departure = addDays(from, 1);
			name = email = phone = message = '';
			quote = null;
		}
	});

	const plansOfType = $derived(plans.filter((p) => p.roomTypeId === roomType));
	$effect(() => {
		if (!roomType && types.length) roomType = types[0].id;
	});
	$effect(() => {
		if (!plansOfType.some((p) => p.id === ratePlan)) ratePlan = plansOfType[0]?.id ?? '';
	});

	// The price and whether the nights are free, shown before anything is saved.
	$effect(() => {
		const query = { arrival, departure, roomType, ratePlan, adults, children };
		quote = null;
		quoteError = '';
		if (!open || !query.roomType || !query.ratePlan || query.departure <= query.arrival || !(+query.adults >= 1)) return;
		const params = new URLSearchParams({ roomTypeId: query.roomType, ratePlanId: query.ratePlan, from: query.arrival, to: query.departure, adults: query.adults, children: query.children || '0' });
		let current = true;
		api.get<Quote>(`/api/quote?${params}`)
			.then((q) => current && (quote = q))
			.catch((e) => current && (quoteError = errorText(e)));
		return () => (current = false);
	});

	async function save(event: SubmitEvent) {
		event.preventDefault();
		saving = true;
		message = '';
		try {
			const res = await api.post<{ id: string }>('/api/reservations', {
				guest: { name, email, phone },
				rooms: [{ roomTypeId: roomType, ratePlanId: ratePlan, from: arrival, to: departure, adults: +adults, children: +children || 0 }]
			});
			open = false;
			onsaved(res.id);
		} catch (e) {
			message = e instanceof ApiError && e.code === 'sold_out' ? t('booking.soldOut') : errorText(e);
		} finally {
			saving = false;
		}
	}
</script>

<Dialog title={t('booking.new')} bind:open>
	<form id="booking-form" class="stack" onsubmit={save}>
		{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
		<div class="pair">
			<Field label={t('booking.arrival')} type="date" bind:value={arrival} />
			<Field label={t('booking.departure')} type="date" bind:value={departure} />
		</div>
		<Select label={t('booking.roomType')} options={types.map((x) => ({ id: x.id, label: x.name }))} bind:value={roomType} />
		<Select label={t('booking.ratePlan')} options={plansOfType.map((x) => ({ id: x.id, label: x.name }))} bind:value={ratePlan} />
		<div class="pair">
			<Field label={t('booking.adults')} inputmode="numeric" bind:value={adults} />
			<Field label={t('booking.children')} inputmode="numeric" bind:value={children} />
		</div>
		<Field label={t('booking.guestName')} autocomplete="off" bind:value={name} />
		<div class="pair">
			<Field label={t('booking.email')} type="email" autocomplete="off" bind:value={email} />
			<Field label={t('booking.phone')} type="tel" autocomplete="off" bind:value={phone} />
		</div>
		<p class="quote" aria-live="polite">
			{#if quote}
				{quote.nights === 1 ? '1 night' : `${quote.nights} nights`}, {formatMoney(quote.total, quote.currency)}.
				{quote.available ? t('booking.available') : t('booking.full')}
			{:else if quoteError}
				{quoteError}
			{:else}
				{t('booking.pickDates')}
			{/if}
		</p>
	</form>
	{#snippet actions()}
		<Button variant="quiet" onclick={() => (open = false)}>{t('common.cancel')}</Button>
		<Button type="submit" form="booking-form" disabled={saving || !quote?.available || name.trim() === ''}>{t('booking.create')}</Button>
	{/snippet}
</Dialog>

<style>
	.pair {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--gap-related);
	}
	.quote {
		margin: 0;
		font: var(--font-control);
		color: var(--color-text);
	}
</style>
