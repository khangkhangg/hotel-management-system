<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { formatMoney, parseMoney } from '#lib/booking/money.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import { toasts } from '#lib/ui/toast.svelte.ts';
	import DataTable from '#lib/ui/DataTable.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import Field from '#lib/ui/Field.svelte';
	import FormGrid from '#lib/ui/FormGrid.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import SearchBox from '#lib/ui/SearchBox.svelte';
	import Select from '#lib/ui/Select.svelte';
	import SidePanel from '#lib/ui/SidePanel.svelte';
	import StatusBadge from '#lib/ui/StatusBadge.svelte';

	type Code = { id: string; code: string; kind: string; value: number; minNights: number; arriveFrom: string; arriveTo: string; maxUses: number | null; used: number; active: boolean };

	const currency = $derived(session.currency);

	let codes = $state<Code[]>([]);
	let query = $state('');
	let panel = $state(false);
	let removing = $state<Code | null>(null);
	let marked = $state('');
	let code = $state('');
	let kind = $state('percent');
	let value = $state('10');
	let minNights = $state('1');
	let arriveFrom = $state('');
	let arriveTo = $state('');
	let maxUses = $state('');
	let message = $state('');
	let busy = $state(false);

	async function load() {
		try {
			codes = await api.get<Code[]>('/api/promo-codes');
		} catch (e) {
			message = errorText(e);
		}
	}

	$effect(() => {
		load();
	});

	async function run(f: () => Promise<void>, done: string, undo?: () => void) {
		message = '';
		busy = true;
		try {
			await f();
			await load();
			toasts.show(done, undo ? { undo: { label: t('common.undo'), run: undo } } : {});
		} catch (e) {
			message = errorText(e);
		} finally {
			busy = false;
		}
	}

	const example = $derived(kind === 'percent' ? t('promo.exampleP', { pct: Number(value) || 0, price: formatMoney(10000, currency), after: formatMoney(Math.round(10000 * (1 - (Number(value) || 0) / 100)), currency) }) : t('promo.exampleA', { off: formatMoney(parseMoney(value, currency) || 0, currency) }));
	const dirty = $derived(code.trim() !== '' || arriveFrom !== '' || arriveTo !== '' || maxUses !== '');

	function reset() {
		code = arriveFrom = arriveTo = maxUses = '';
		kind = 'percent';
		value = '10';
		minNights = '1';
	}

	const add = (event: SubmitEvent) => {
		event.preventDefault();
		const amount = kind === 'percent' ? Number(value) : parseMoney(value, currency);
		if (!amount) {
			message = t('promo.badValue');
			return;
		}
		const name = code.trim();
		return run(
			async () => {
				await api.post('/api/promo-codes', { code: name, kind, value: amount, minNights: Number(minNights) || 1, arriveFrom, arriveTo, maxUses: maxUses ? Number(maxUses) : null, active: true });
				panel = false;
				reset();
				await load();
				marked = codes.find((c) => c.code.toLocaleUpperCase('en') === name.toLocaleUpperCase('en'))?.id ?? '';
			},
			t('promo.added')
		);
	};

	const toggle = (c: Code) =>
		run(
			() => api.put(`/api/promo-codes/${c.id}`, { ...c, active: !c.active }),
			t('promo.changed'),
			() => run(() => api.put(`/api/promo-codes/${c.id}`, c), t('promo.changed'))
		);
	const remove = (c: Code) => run(async () => { await api.delete(`/api/promo-codes/${c.id}`); removing = null; }, t('promo.removed'));
	const worth = (c: Code) => (c.kind === 'percent' ? t('promo.percentOff', { value: c.value }) : t('promo.amountOff', { value: formatMoney(c.value, currency) }));
	const rules = (c: Code) => [c.minNights > 1 ? t('promo.rule.nights', { n: c.minNights }) : '', c.arriveFrom ? t('promo.rule.from', { date: c.arriveFrom }) : '', c.arriveTo ? t('promo.rule.until', { date: c.arriveTo }) : ''].filter(Boolean).join(', ') || t('promo.rule.any');

	const shown = $derived(codes.filter((c) => c.code.toLocaleLowerCase('en').includes(query.trim().toLocaleLowerCase('en'))));
	const columns = [
		{ id: 'code', label: t('promo.code'), value: (c: Code) => c.code, priority: 'main' as const },
		{ id: 'worth', label: t('promo.col.discount'), value: worth, priority: 'detail' as const },
		{ id: 'rules', label: t('promo.col.rules'), value: rules, priority: 'extra' as const },
		{ id: 'used', label: t('promo.col.used'), value: (c: Code) => t('promo.usedShort', { used: c.used, max: c.maxUses ?? '∞' }), priority: 'extra' as const, align: 'end' as const },
		{ id: 'status', label: t('promo.col.status'), value: (c: Code) => (c.active ? t('promo.on') : t('promo.off')), priority: 'status' as const },
		{ id: 'actions', label: t('promo.col.actions'), value: () => '', priority: 'detail' as const, align: 'end' as const, hideLabel: true }
	];
</script>

<svelte:head><title>{t('promo.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('promo.title')} note={t('promo.note')}>
	{#snippet primary()}<Button icon="plus" onclick={() => (panel = true)}>{t('promo.new')}</Button>{/snippet}
</PageHeader>
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

{#if codes.length > 6}<SearchBox label={t('promo.search')} bind:value={query} />{/if}

<DataTable label={t('promo.codes')} rows={shown} {columns} rowKey={(c) => c.id} {marked}>
	{#snippet cell(c, column)}
		{#if column.id === 'status'}
			<StatusBadge tone={c.active ? 'success' : 'neutral'}>{column.value(c)}</StatusBadge>
		{:else if column.id === 'actions'}
			<span class="row">
				<Button variant="secondary" disabled={busy} onclick={() => toggle(c)}>{c.active ? t('promo.switchOff') : t('promo.switchOn')}<span class="visually-hidden"> {c.code}</span></Button>
				<Button variant="quiet" disabled={busy} onclick={() => (removing = c)}>{t('promo.remove')}<span class="visually-hidden"> {c.code}</span></Button>
			</span>
		{:else if column.id === 'code'}
			<strong>{c.code}</strong>
		{:else}
			{column.value(c)}
		{/if}
	{/snippet}
	{#snippet empty()}
		<p class="muted">{t('promo.none')}</p>
	{/snippet}
</DataTable>

{#if removing}
	{@const target = removing}
	<Dialog title={t('promo.removeTitle', { code: target.code })} open={true} onclose={() => (removing = null)}>
		<p>{t('promo.removeBody')}</p>
		{#snippet actions()}
			<Button variant="secondary" disabled={busy} onclick={() => remove(target)}>{t('promo.removeConfirm')}</Button>
			<Button onclick={() => (removing = null)}>{t('promo.keep')}</Button>
		{/snippet}
	</Dialog>
{/if}

<SidePanel title={t('promo.new')} bind:open={panel} {dirty} onclose={reset}>
	<form id="promo-form" onsubmit={add}>
		<FormGrid>
			<Field label={t('promo.code')} placeholder="SPRING10" bind:value={code} />
			<Select label={t('promo.kind')} options={[{ id: 'percent', label: t('promo.kind.percent') }, { id: 'amount', label: t('promo.kind.amount') }]} value={kind} onchange={(id) => (kind = id)} />
			<Field label={kind === 'percent' ? t('promo.valuePercent') : t('promo.valueAmount', { currency })} inputmode="decimal" bind:value />
			<p class="hint">{example}</p>
			<Field label={t('promo.minNights')} inputmode="numeric" bind:value={minNights} />
			<Field label={t('promo.arriveFrom')} type="date" bind:value={arriveFrom} />
			<Field label={t('promo.arriveTo')} type="date" bind:value={arriveTo} />
			<Field label={t('promo.maxUses')} inputmode="numeric" bind:value={maxUses} />
		</FormGrid>
	</form>
	{#snippet actions()}
		<Button type="submit" form="promo-form" disabled={busy}>{t('promo.add')}</Button>
	{/snippet}
</SidePanel>

<style>
	.row {
		display: inline-flex;
		flex-wrap: wrap;
		gap: var(--gap-related);
	}
	.hint {
		grid-column: 1 / -1;
		margin: 0;
		color: var(--color-text-muted);
		font: var(--font-caption);
	}
	.muted {
		margin: 0 0 var(--gap-related);
		color: var(--color-text-muted);
	}
</style>
