<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { formatMoney, parseMoney } from '#lib/booking/money.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import Checkbox from '#lib/ui/Checkbox.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';

	type Suggestion = { date: string; ratePlanId: string; ratePlanName: string; roomType: string; current: number; price: number; occupancy: number; competitor: number | null; reasons: string[] };
	type Limit = { roomTypeId: string; floor: number | null; ceiling: number | null };
	type RoomType = { id: string; name: string };
	type Competitor = { id: string; date: string; name: string; price: number };

	const currency = $derived(session.currency);
	const iso = (d: Date) => d.toISOString().slice(0, 10);
	const today = iso(new Date());
	const later = iso(new Date(Date.now() + 30 * 86400000));

	let suggestions = $state<Suggestion[]>([]);
	let picked = $state<Record<string, boolean>>({});
	let roomTypes = $state<RoomType[]>([]);
	let limits = $state<Record<string, { floor: string; ceiling: string }>>({});
	let competitors = $state<Competitor[]>([]);
	let cDate = $state('');
	let cName = $state('');
	let cPrice = $state('');
	let message = $state('');
	let ok = $state('');
	let busy = $state(false);
	let loaded = $state(false);

	const key = (s: Suggestion) => `${s.ratePlanId}|${s.date}`;
	const major = (n: number | null) => (n === null ? '' : String(n / (['VND', 'JPY', 'KRW', 'CLP', 'IDR'].includes(currency) ? 1 : 100)));

	async function load() {
		try {
			const [s, r, l, c] = await Promise.all([
				api.get<Suggestion[]>('/api/price-suggestions?days=30'),
				api.get<RoomType[]>('/api/room-types'),
				api.get<Limit[]>('/api/price-limits'),
				api.get<Competitor[]>(`/api/competitor-rates?from=${today}&to=${later}`)
			]);
			suggestions = s;
			picked = Object.fromEntries(s.map((x) => [key(x), true]));
			roomTypes = r;
			limits = Object.fromEntries(r.map((rt) => [rt.id, { floor: major(l.find((x) => x.roomTypeId === rt.id)?.floor ?? null), ceiling: major(l.find((x) => x.roomTypeId === rt.id)?.ceiling ?? null) }]));
			competitors = c;
			loaded = true;
		} catch (e) {
			message = errorText(e);
		}
	}

	$effect(() => {
		load();
	});

	async function run(f: () => Promise<void>, done: string) {
		message = ok = '';
		busy = true;
		try {
			await f();
			await load();
			ok = done;
		} catch (e) {
			message = errorText(e);
		} finally {
			busy = false;
		}
	}

	const apply = () =>
		run(
			() => api.post('/api/price-suggestions/apply', { changes: suggestions.filter((s) => picked[key(s)]).map((s) => ({ ratePlanId: s.ratePlanId, date: s.date, price: s.price })) }),
			t('pricing.applied')
		);

	const saveLimit = (rt: RoomType) => {
		const l = limits[rt.id];
		const floor = l.floor.trim() ? parseMoney(l.floor, currency) : null;
		const ceiling = l.ceiling.trim() ? parseMoney(l.ceiling, currency) : null;
		if ((l.floor.trim() && !floor) || (l.ceiling.trim() && !ceiling)) {
			message = t('pricing.badAmount');
			return;
		}
		return run(() => api.put('/api/price-limits', { roomTypeId: rt.id, floor, ceiling }), t('pricing.limitSaved', { name: rt.name }));
	};

	const addCompetitor = (event: SubmitEvent) => {
		event.preventDefault();
		const price = parseMoney(cPrice, currency);
		if (!price) {
			message = t('pricing.badAmount');
			return;
		}
		return run(async () => {
			await api.post('/api/competitor-rates', { rates: [{ date: cDate, name: cName, price }] });
			cPrice = '';
		}, t('pricing.competitorSaved'));
	};

	const removeCompetitor = (c: Competitor) => run(() => api.delete(`/api/competitor-rates/${c.id}`), t('pricing.competitorRemoved'));
</script>

<svelte:head><title>{t('pricing.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('pricing.title')} note={t('pricing.note')} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
{#if ok}<p class="notice success" role="status">{ok}</p>{/if}

{#if loaded}
	<section class="card">
		<h2>{t('pricing.suggestions')}</h2>
		{#if suggestions.length === 0}
			<p class="muted">{t('pricing.noSuggestions')}</p>
		{:else}
			<ul class="rows">
				{#each suggestions as s (key(s))}
					<li>
						<Checkbox label={`${s.date} · ${s.roomType}`} bind:checked={picked[key(s)]} />
						<p><strong>{formatMoney(s.current, currency)}</strong> → <strong>{formatMoney(s.price, currency)}</strong> <span class="muted">{t('pricing.occupancy', { percent: Math.round(s.occupancy * 100) })}</span></p>
						<ul class="reasons">
							{#each s.reasons as r (r)}<li>{r}</li>{/each}
						</ul>
					</li>
				{/each}
			</ul>
			<div class="row"><Button disabled={busy} onclick={apply}>{t('pricing.apply')}</Button></div>
			<p class="muted">{t('pricing.applyNote')}</p>
		{/if}
	</section>

	<section class="card">
		<h2>{t('pricing.limits')}</h2>
		<p class="muted">{t('pricing.limitsNote', { currency })}</p>
		{#each roomTypes as rt (rt.id)}
			<div class="limit">
				<strong>{rt.name}</strong>
				<Field label={t('pricing.floor')} inputmode="decimal" bind:value={limits[rt.id].floor} />
				<Field label={t('pricing.ceiling')} inputmode="decimal" bind:value={limits[rt.id].ceiling} />
				<div class="row"><Button variant="secondary" disabled={busy} onclick={() => saveLimit(rt)}>{t('common.save')}</Button></div>
			</div>
		{/each}
	</section>

	<form class="card" onsubmit={addCompetitor}>
		<h2>{t('pricing.competitors')}</h2>
		<p class="muted">{t('pricing.competitorsNote')}</p>
		<Field label={t('pricing.cDate')} type="date" bind:value={cDate} />
		<Field label={t('pricing.cName')} bind:value={cName} />
		<Field label={t('pricing.cPrice', { currency })} inputmode="decimal" bind:value={cPrice} />
		<div class="row"><Button type="submit" disabled={busy}>{t('pricing.addCompetitor')}</Button></div>
		{#if competitors.length}
			<ul class="rows">
				{#each competitors as c (c.id)}
					<li class="inline">
						<span>{c.date} · {c.name} · {formatMoney(c.price, currency)}</span>
						<Button variant="quiet" disabled={busy} onclick={() => removeCompetitor(c)}>{t('yield.remove')}</Button>
					</li>
				{/each}
			</ul>
		{/if}
	</form>
{/if}

<style>
	h2 {
		margin: 0;
		font: var(--font-heading);
	}
	p {
		margin: 0;
	}
	.card {
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
		max-inline-size: var(--page-width);
		padding: var(--space-4);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
		font: var(--font-body);
	}
	.rows {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
	}
	.rows > li {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		padding: var(--space-3);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
	}
	.rows > li.inline {
		flex-direction: row;
		flex-wrap: wrap;
		justify-content: space-between;
		align-items: center;
	}
	.reasons {
		margin: 0;
		padding-inline-start: var(--space-6);
	}
	.limit {
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: var(--gap-related);
	}
	.muted {
		color: var(--color-text-muted);
	}
</style>
