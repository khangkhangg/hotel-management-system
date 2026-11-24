<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { t } from '#lib/i18n.ts';
	import { formatMoney } from '#lib/booking/money.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import DataTable from '#lib/ui/DataTable.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import Field from '#lib/ui/Field.svelte';
	import FormGrid from '#lib/ui/FormGrid.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Section from '#lib/ui/Section.svelte';
	import Select from '#lib/ui/Select.svelte';
	import SidePanel from '#lib/ui/SidePanel.svelte';
	import StatusBadge from '#lib/ui/StatusBadge.svelte';
	import { toasts } from '#lib/ui/toast.svelte.ts';

	type Rule = { id: string; kind: string; roomTypeId: string | null; from: string; to: string; value: number; percent: number; active: boolean };
	type RoomType = { id: string; name: string };
	type Parity = { date: string; roomType: string; direct: number; channel: number; status: string };
	type Preview = { date: string; roomType: string; availableBefore: number; availableAfter: number; priceBefore: number; priceAfter: number };

	const kinds = ['auto_close', 'sell_limit', 'availability_cap', 'rate_adjust'];

	let rules = $state<Rule[]>([]);
	let parity = $state<Parity[]>([]);
	let preview = $state<Preview[] | null>(null);
	let roomTypes = $state<RoomType[]>([]);
	let panel = $state(false);
	let removing = $state<Rule | null>(null);
	let marked = $state('');
	let kind = $state('availability_cap');
	let roomTypeId = $state('');
	let value = $state('1');
	let percent = $state('10');
	let from = $state('');
	let to = $state('');
	let message = $state('');
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
		message = '';
		busy = true;
		try {
			await f();
			await load();
			toasts.show(done);
		} catch (e) {
			message = errorText(e);
		} finally {
			busy = false;
		}
	}

	const draft = () => ({ kind, roomTypeId: roomTypeId || null, from, to, value: Number(value), percent: kind === 'rate_adjust' ? Number(percent) : 0, active: true });
	const dirty = $derived(from !== '' || to !== '' || roomTypeId !== '' || value !== '1' || kind !== 'availability_cap');

	function reset() {
		kind = 'availability_cap';
		roomTypeId = from = to = '';
		value = '1';
		percent = '10';
		preview = null;
	}

	const add = (event: SubmitEvent) => {
		event.preventDefault();
		const before = new Set(rules.map((r) => r.id));
		return run(async () => {
			await api.post('/api/yield-rules', draft());
			panel = false;
			reset();
			await load();
			marked = rules.find((r) => !before.has(r.id))?.id ?? '';
		}, t('yield.added'));
	};

	async function showPreview() {
		message = '';
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
	const remove = (r: Rule) =>
		run(async () => {
			await api.delete(`/api/yield-rules/${r.id}`);
			removing = null;
		}, t('yield.removed'));
	const where = (r: Rule) => (r.roomTypeId ? (roomTypes.find((x) => x.id === r.roomTypeId)?.name ?? '') : t('yield.allRooms'));
	const when = (r: Rule) => (r.from || r.to ? t('yield.window', { from: r.from || '...', to: r.to || '...' }) : t('yield.everyNight'));

	const ruleColumns = [
		{ id: 'rule', label: t('yield.kind'), value: (r: Rule) => t(`yield.d.${r.kind}`, { value: r.value, percent: r.percent }), priority: 'main' as const },
		{ id: 'where', label: t('yield.roomType'), value: where, priority: 'detail' as const },
		{ id: 'when', label: t('yield.col.when'), value: when, priority: 'extra' as const },
		{ id: 'status', label: t('yield.col.status'), value: (r: Rule) => (r.active ? t('yield.on') : t('yield.off')), priority: 'status' as const },
		{ id: 'actions', label: t('yield.col.actions'), value: () => '', priority: 'detail' as const, align: 'end' as const, hideLabel: true }
	];
	const parityColumns = [
		{ id: 'night', label: t('yield.col.night'), value: (p: Parity) => `${p.date} · ${p.roomType}`, priority: 'main' as const },
		{ id: 'direct', label: t('yield.col.direct'), value: (p: Parity) => formatMoney(p.direct, session.currency), priority: 'detail' as const, align: 'end' as const },
		{ id: 'channel', label: t('yield.col.channel'), value: (p: Parity) => formatMoney(p.channel, session.currency), priority: 'detail' as const, align: 'end' as const },
		{ id: 'status', label: t('yield.col.status'), value: (p: Parity) => t(`yield.parity.${p.status}`), priority: 'status' as const }
	];
</script>

<svelte:head><title>{t('yield.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('yield.title')} note={t('yield.note')}>
	{#snippet primary()}<Button icon="plus" onclick={() => (panel = true)}>{t('yield.new')}</Button>{/snippet}
</PageHeader>
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

<Section title={t('yield.rules')}>
	<DataTable label={t('yield.rules')} rows={rules} columns={ruleColumns} rowKey={(r) => r.id} {marked}>
		{#snippet cell(r, column)}
			{#if column.id === 'status'}
				<StatusBadge tone={r.active ? 'success' : 'neutral'}>{column.value(r)}</StatusBadge>
			{:else if column.id === 'actions'}
				<span class="row">
					<Button variant="secondary" disabled={busy} onclick={() => toggle(r)}>{r.active ? t('yield.switchOff') : t('yield.switchOn')}<span class="visually-hidden"> {column.value(r)}</span></Button>
					<Button variant="quiet" disabled={busy} onclick={() => (removing = r)}>{t('yield.remove')}<span class="visually-hidden"> {t(`yield.d.${r.kind}`, { value: r.value, percent: r.percent })}</span></Button>
				</span>
			{:else if column.id === 'rule'}
				<strong>{column.value(r)}</strong>
			{:else}
				{column.value(r)}
			{/if}
		{/snippet}
		{#snippet empty()}<p class="muted">{t('yield.none')}</p>{/snippet}
	</DataTable>
</Section>

<Section title={t('yield.parity')}>
	<p class="muted">{parity.length === 0 ? t('yield.parityNone') : t('yield.parityNote')}</p>
	<DataTable label={t('yield.parity')} rows={parity.slice(0, 30)} columns={parityColumns} rowKey={(p) => p.date + p.roomType}>
		{#snippet cell(p, column)}
			{#if column.id === 'status'}
				<StatusBadge tone={p.status === 'channel_higher' ? 'neutral' : 'warning'}>{column.value(p)}</StatusBadge>
			{:else}
				{column.value(p)}
			{/if}
		{/snippet}
	</DataTable>
</Section>

{#if removing}
	{@const target = removing}
	<Dialog title={t('yield.removeTitle')} open={true} onclose={() => (removing = null)}>
		<p>{t('yield.removeBody', { rule: t(`yield.d.${target.kind}`, { value: target.value, percent: target.percent }) })}</p>
		{#snippet actions()}
			<Button variant="secondary" disabled={busy} onclick={() => remove(target)}>{t('yield.removeConfirm')}</Button>
			<Button onclick={() => (removing = null)}>{t('yield.keep')}</Button>
		{/snippet}
	</Dialog>
{/if}

<SidePanel title={t('yield.new')} bind:open={panel} {dirty} onclose={reset}>
	<form id="yield-form" onsubmit={add}>
		<Select label={t('yield.kind')} options={kinds.map((k) => ({ id: k, label: t(`yield.kind.${k}`) }))} value={kind} onchange={(id) => (kind = id)} />
		<p class="muted">{t(`yield.help.${kind}`)}</p>
		<FormGrid>
			<Select label={t('yield.roomType')} options={[{ id: '', label: t('yield.allRooms') }, ...roomTypes.map((r) => ({ id: r.id, label: r.name }))]} value={roomTypeId} onchange={(id) => (roomTypeId = id)} />
			<Field label={t(`yield.value.${kind}`)} inputmode="numeric" bind:value />
			{#if kind === 'rate_adjust'}<Field label={t('yield.percent')} inputmode="numeric" bind:value={percent} />{/if}
			<Field label={t('yield.from')} type="date" bind:value={from} />
			<Field label={t('yield.to')} type="date" bind:value={to} />
		</FormGrid>
	</form>
	<div>
		<Button variant="secondary" disabled={busy} onclick={showPreview}>{t('yield.preview')}</Button>
	</div>
	{#if preview}
		{#if preview.length === 0}
			<p class="muted" role="status">{t('yield.previewNone')}</p>
		{:else}
			<div role="status">
				<p class="muted">{t('yield.previewCount', { count: preview.length })}</p>
				{#each preview.slice(0, 8) as p (p.date + p.roomType)}
					<p>{t('yield.previewRow', { date: p.date, roomType: p.roomType, before: p.availableBefore, after: p.availableAfter, priceBefore: formatMoney(p.priceBefore, session.currency), priceAfter: formatMoney(p.priceAfter, session.currency) })}</p>
				{/each}
			</div>
		{/if}
	{/if}
	{#snippet actions()}
		<Button type="submit" form="yield-form" disabled={busy}>{t('yield.add')}</Button>
	{/snippet}
</SidePanel>

<style>
	p {
		margin: 0;
	}
	.row {
		display: inline-flex;
		flex-wrap: wrap;
		gap: var(--gap-related);
	}
	.muted {
		color: var(--color-text-muted);
	}
</style>
