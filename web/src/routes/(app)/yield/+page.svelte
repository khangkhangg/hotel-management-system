<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { t } from '#lib/i18n.ts';
	import Button from '#lib/ui/Button.svelte';
	import FormGrid from '#lib/ui/FormGrid.svelte';
	import Section from '#lib/ui/Section.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Select from '#lib/ui/Select.svelte';
	import { formatMoney } from '#lib/booking/money.ts';
	import { session } from '#lib/session.svelte.ts';

	type Rule = { id: string; kind: string; roomTypeId: string | null; from: string; to: string; value: number; percent: number; active: boolean };
	type RoomType = { id: string; name: string };
	type Parity = { date: string; roomType: string; direct: number; channel: number; status: string };

	const kinds = ['auto_close', 'sell_limit', 'availability_cap', 'rate_adjust'];

	let rules = $state<Rule[]>([]);
	let parity = $state<Parity[]>([]);
	type Preview = { date: string; roomType: string; availableBefore: number; availableAfter: number; priceBefore: number; priceAfter: number };
	let preview = $state<Preview[] | null>(null);
	let roomTypes = $state<RoomType[]>([]);
	let kind = $state('availability_cap');
	let roomTypeId = $state('');
	let value = $state('1');
	let percent = $state('10');
	let from = $state('');
	let to = $state('');
	let message = $state('');
	let ok = $state('');
	let busy = $state(false);

	async function load() {
		try {
			[rules, roomTypes, parity] = await Promise.all([api.get<Rule[]>('/api/yield-rules'), api.get<RoomType[]>('/api/room-types'), api.get<Parity[]>('/api/channel/parity').catch(() => [])]);
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

	const draft = () => ({ kind, roomTypeId: roomTypeId || null, from, to, value: Number(value), percent: kind === 'rate_adjust' ? Number(percent) : 0, active: true });

	const add = (event: SubmitEvent) => {
		event.preventDefault();
		preview = null;
		return run(() => api.post('/api/yield-rules', draft()), t('yield.added'));
	};

	async function showPreview() {
		message = ok = '';
		busy = true;
		try {
			preview = await api.post<Preview[]>('/api/channel/yield-preview', draft());
		} catch (e) {
			message = errorText(e);
		} finally {
			busy = false;
		}
	}

	const toggle = (r: Rule) => run(() => api.put(`/api/yield-rules/${r.id}`, { ...r, active: !r.active }), t('yield.changed'));
	const remove = (r: Rule) => run(() => api.delete(`/api/yield-rules/${r.id}`), t('yield.removed'));

	const where = (r: Rule) => (r.roomTypeId ? (roomTypes.find((x) => x.id === r.roomTypeId)?.name ?? '') : t('yield.allRooms'));
	const when = (r: Rule) => (r.from || r.to ? t('yield.window', { from: r.from || '…', to: r.to || '…' }) : t('yield.everyNight'));
</script>

<svelte:head><title>{t('yield.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('yield.title')} note={t('yield.note')} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
{#if ok}<p class="notice success" role="status">{ok}</p>{/if}

<Section title={t('yield.rules')}>
	{#if rules.length === 0}
		<p class="muted">{t('yield.none')}</p>
	{:else}
		<ul class="rules">
			{#each rules as r (r.id)}
				<li class:off={!r.active}>
					<div>
						<strong>{t(`yield.d.${r.kind}`, { value: r.value, percent: r.percent })}</strong>
						<span class="muted">{where(r)} · {when(r)}{r.active ? '' : ` · ${t('yield.off')}`}</span>
					</div>
					<div class="row">
						<Button variant="secondary" disabled={busy} onclick={() => toggle(r)}>{r.active ? t('yield.switchOff') : t('yield.switchOn')}</Button>
						<Button variant="quiet" disabled={busy} onclick={() => remove(r)}>{t('yield.remove')}</Button>
					</div>
				</li>
			{/each}
		</ul>
	{/if}
</Section>

<Section title={t('yield.new')}>
<form class="stack" onsubmit={add}>
	<Select label={t('yield.kind')} options={kinds.map((k) => ({ id: k, label: t(`yield.kind.${k}`) }))} value={kind} onchange={(id) => (kind = id)} />
	<p class="muted">{t(`yield.help.${kind}`)}</p>
	<FormGrid>
	<Select label={t('yield.roomType')} options={[{ id: '', label: t('yield.allRooms') }, ...roomTypes.map((r) => ({ id: r.id, label: r.name }))]} value={roomTypeId} onchange={(id) => (roomTypeId = id)} />
	<Field label={t(`yield.value.${kind}`)} inputmode="numeric" bind:value />
	{#if kind === 'rate_adjust'}<Field label={t('yield.percent')} inputmode="numeric" bind:value={percent} />{/if}
	<Field label={t('yield.from')} type="date" bind:value={from} />
	<Field label={t('yield.to')} type="date" bind:value={to} />
	</FormGrid>
	<div class="row">
		<Button type="submit" disabled={busy}>{t('yield.add')}</Button>
		<Button variant="secondary" disabled={busy} onclick={showPreview}>{t('yield.preview')}</Button>
	</div>
	{#if preview}
		{#if preview.length === 0}
			<p class="muted" role="status">{t('yield.previewNone')}</p>
		{:else}
			<p role="status">{t('yield.previewCount', { count: preview.length })}</p>
			<ul class="plain">
				{#each preview.slice(0, 8) as p (p.date + p.roomType)}
					<li>{t('yield.previewRow', { date: p.date, roomType: p.roomType, before: p.availableBefore, after: p.availableAfter, priceBefore: formatMoney(p.priceBefore, session.currency), priceAfter: formatMoney(p.priceAfter, session.currency) })}</li>
				{/each}
			</ul>
		{/if}
	{/if}
</form>
</Section>

<Section title={t('yield.parity')}>
	{#if parity.length === 0}
		<p class="muted">{t('yield.parityNone')}</p>
	{:else}
		<p class="muted">{t('yield.parityNote')}</p>
		<ul class="rules">
			{#each parity.slice(0, 30) as p (p.date + p.roomType)}
				<li>
					<div>
						<strong>{p.date} · {p.roomType}</strong>
						<span class="muted">{t('yield.parityRow', { direct: formatMoney(p.direct, session.currency), channel: formatMoney(p.channel, session.currency) })}</span>
					</div>
					<span class:off={p.status === 'channel_higher'}>{t(`yield.parity.${p.status}`)}</span>
				</li>
			{/each}
		</ul>
	{/if}
</Section>

<style>
	p {
		margin: 0;
	}
	.rules {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
	}
	.rules li {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		align-items: center;
		gap: var(--gap-related);
		padding: var(--space-3);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
	}
	.rules li > div:first-child {
		display: flex;
		flex-direction: column;
	}
	.plain {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.off strong {
		color: var(--color-text-muted);
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
