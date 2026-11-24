<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { page } from '$app/state';
	import { addDays, formatDay, rangeDays, weekday } from '#lib/booking/dates.ts';
	import { formatMoney, parseMoney } from '#lib/booking/money.ts';
	import { foldRuns } from '#lib/booking/runs.ts';
	import type { RatePlan, RoomType } from '#lib/booking/types.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import Checkbox from '#lib/ui/Checkbox.svelte';
	import SidePanel from '#lib/ui/SidePanel.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Select from '#lib/ui/Select.svelte';

	type Rate = { date: string; price: number; minNights: number; closed: boolean; closedToArrival: boolean; closedToDeparture: boolean; suspicious: boolean };

	const today = session.today;
	const DAYS = 30;
	let types = $state<RoomType[]>([]);
	let plans = $state<RatePlan[]>([]);
	let plan = $state('');
	let rates = $state<Rate[]>([]);
	let message = $state('');
	let ok = $state('');
	let open = $state(false);
	let from = $state(today);
	let to = $state(addDays(today, 7));
	let price = $state('');
	let minNights = $state('1');
	let closed = $state(false);
	let closedToArrival = $state(false);
	let closedToDeparture = $state(false);
	const currency = $derived(session.currency);

	const days = rangeDays(today, DAYS);
	const byDate = $derived(Object.fromEntries(rates.map((r) => [r.date, r])));
	const rulesOf = (r: Rate) => [r.minNights, r.closed, r.closedToArrival, r.closedToDeparture, r.suspicious].join();
	const runs = $derived(foldRuns(days, (d) => byDate[d] ?? null, (a, b) => (a && b ? a.price === b.price && rulesOf(a) === rulesOf(b) : a === b)));

	$effect(() => {
		Promise.all([api.get<RoomType[]>('/api/room-types'), api.get<RatePlan[]>('/api/rate-plans')])
			.then(([a, b]) => {
				types = a;
				plans = b;
				plan = plan || (b.some((x) => x.id === page.url.searchParams.get('plan')) ? (page.url.searchParams.get('plan') ?? '') : b[0]?.id) || '';
			})
			.catch((e) => (message = errorText(e)));
	});
	$effect(() => {
		if (plan) load();
	});

	async function load() {
		try {
			rates = await api.get<Rate[]>(`/api/rates?ratePlanId=${plan}&from=${today}&to=${addDays(today, DAYS)}`);
		} catch (e) {
			message = errorText(e);
		}
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		message = ok = '';
		const cents = parseMoney(price, currency);
		if (cents === null) {
			message = t('panel.badAmount');
			return;
		}
		try {
			const r = await api.put<{ suspicious: string[] }>('/api/rates', {
				ratePlanId: plan,
				from,
				to,
				price: cents,
				minNights: +minNights || 1,
				closed,
				closedToArrival,
				closedToDeparture
			});
			open = false;
			ok = r.suspicious.length ? t('rates.suspicious', { dates: r.suspicious.map(formatDay).join(', ') }) : t('rates.saved');
			await load();
		} catch (e) {
			message = errorText(e);
		}
	}
	const planLabel = (p: RatePlan) => `${types.find((x) => x.id === p.roomTypeId)?.name ?? ''}, ${p.name}`;
</script>

<svelte:head><title>{t('rates.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('rates.title')} note={t('rates.note')}>
	{#snippet primary()}<Button icon="tag" disabled={!plan} onclick={() => (open = true)}>{t('rates.set')}</Button>{/snippet}
</PageHeader>

{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
{#if ok}<p class="notice success" role="status">{ok}</p>{/if}

{#if plans.length === 0}
	<p class="muted">{t('rates.noPlans')} <a href="/rate-plans">{t('rate-plans.title')}</a></p>
{:else}
	<div class="pick"><Select label={t('booking.ratePlan')} options={plans.map((p) => ({ id: p.id, label: planLabel(p) }))} bind:value={plan} /></div>
	<!-- svelte-ignore a11y_no_noninteractive_tabindex -- a scrolling table must be reachable by keyboard -->
	<div class="table-scroll" role="region" aria-label={t('rates.title')} tabindex="0">
		<table class="data-table">
			<thead>
				<tr>
					<th scope="col">{t('rates.date')}</th>
					<th scope="col" class="num">{t('rates.price')}</th>
					<th scope="col">{t('rates.rules')}</th>
				</tr>
			</thead>
			<tbody>
				{#each runs as run (run.from)}
					{@const r = run.value}
					<tr>
						<th scope="row">{run.n === 1 ? `${weekday(run.from)} ${formatDay(run.from)}` : t('rates.range', { from: formatDay(run.from), to: formatDay(run.to), n: run.n })}</th>
						<td class="num">{r ? formatMoney(r.price, currency) : '–'}</td>
						<td>
							{#if !r}<span class="muted">{t('rates.none')}</span>
							{:else}
								<span class="muted">
									{r.minNights > 1 ? t('rates.minNights', { n: r.minNights }) : ''}
									{r.closed ? t('rates.closed') : ''}
									{r.closedToArrival ? t('rates.cta') : ''}
									{r.closedToDeparture ? t('rates.ctd') : ''}
									{r.suspicious ? t('rates.check') : ''}
								</span>
							{/if}
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}

<SidePanel title={t('rates.set')} bind:open>
	<form id="rate-form" class="stack" onsubmit={save}>
		<div class="pair">
			<Field label={t('rates.from')} type="date" bind:value={from} />
			<Field label={t('rates.to')} type="date" bind:value={to} />
		</div>
		<Field label={t('rates.pricePerNight')} inputmode="decimal" bind:value={price} />
		<Field label={t('rates.minimum')} inputmode="numeric" bind:value={minNights} />
		<Checkbox label={t('rates.closedLabel')} bind:checked={closed} />
		<Checkbox label={t('rates.ctaLabel')} bind:checked={closedToArrival} />
		<Checkbox label={t('rates.ctdLabel')} bind:checked={closedToDeparture} />
	</form>
	{#snippet actions()}
		<Button variant="quiet" onclick={() => (open = false)}>{t('common.cancel')}</Button>
		<Button type="submit" form="rate-form" disabled={price.trim() === ''}>{t('rates.save')}</Button>
	{/snippet}
</SidePanel>

<style>
	.pick {
		max-inline-size: var(--form-width);
	}
	.pair {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--gap-related);
	}
</style>
