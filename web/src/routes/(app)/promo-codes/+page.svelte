<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { formatMoney, parseMoney } from '#lib/booking/money.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import FormGrid from '#lib/ui/FormGrid.svelte';
	import Section from '#lib/ui/Section.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Select from '#lib/ui/Select.svelte';

	type Code = { id: string; code: string; kind: string; value: number; minNights: number; arriveFrom: string; arriveTo: string; maxUses: number | null; used: number; active: boolean };

	const currency = $derived(session.currency);

	let codes = $state<Code[]>([]);
	let code = $state('');
	let kind = $state('percent');
	let value = $state('10');
	let minNights = $state('1');
	let arriveFrom = $state('');
	let arriveTo = $state('');
	let maxUses = $state('');
	let message = $state('');
	let ok = $state('');
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

	const add = (event: SubmitEvent) => {
		event.preventDefault();
		const amount = kind === 'percent' ? Number(value) : parseMoney(value, currency);
		if (!amount) {
			message = t('promo.badValue');
			return;
		}
		return run(
			() =>
				api.post('/api/promo-codes', {
					code,
					kind,
					value: amount,
					minNights: Number(minNights) || 1,
					arriveFrom,
					arriveTo,
					maxUses: maxUses ? Number(maxUses) : null,
					active: true
				}),
			t('promo.added')
		);
	};

	const toggle = (c: Code) => run(() => api.put(`/api/promo-codes/${c.id}`, { ...c, active: !c.active }), t('promo.changed'));
	const remove = (c: Code) => run(() => api.delete(`/api/promo-codes/${c.id}`), t('promo.removed'));
	const worth = (c: Code) => (c.kind === 'percent' ? t('promo.percentOff', { value: c.value }) : t('promo.amountOff', { value: formatMoney(c.value, currency) }));
</script>

<svelte:head><title>{t('promo.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('promo.title')} note={t('promo.note')} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
{#if ok}<p class="notice success" role="status">{ok}</p>{/if}

<Section title={t('promo.codes')}>
	{#if codes.length === 0}
		<p class="muted">{t('promo.none')}</p>
	{:else}
		<ul class="codes">
			{#each codes as c (c.id)}
				<li class:off={!c.active}>
					<div>
						<strong>{c.code}</strong>
						<span class="muted">{worth(c)} · {t('promo.used', { used: c.used, max: c.maxUses ?? '∞' })}{c.active ? '' : ` · ${t('promo.off')}`}</span>
					</div>
					<div class="row">
						<Button variant="secondary" disabled={busy} onclick={() => toggle(c)}>{c.active ? t('promo.switchOff') : t('promo.switchOn')}</Button>
						<Button variant="quiet" disabled={busy} onclick={() => remove(c)}>{t('promo.remove')}</Button>
					</div>
				</li>
			{/each}
		</ul>
	{/if}
</Section>

<Section title={t('promo.new')}>
<form class="stack" onsubmit={add}>
	<FormGrid>
	<Field label={t('promo.code')} placeholder="SPRING10" bind:value={code} />
	<Select label={t('promo.kind')} options={[{ id: 'percent', label: t('promo.kind.percent') }, { id: 'amount', label: t('promo.kind.amount') }]} value={kind} onchange={(id) => (kind = id)} />
	<Field label={kind === 'percent' ? t('promo.valuePercent') : t('promo.valueAmount', { currency })} inputmode="decimal" bind:value />
	<Field label={t('promo.minNights')} inputmode="numeric" bind:value={minNights} />
	<Field label={t('promo.arriveFrom')} type="date" bind:value={arriveFrom} />
	<Field label={t('promo.arriveTo')} type="date" bind:value={arriveTo} />
	<Field label={t('promo.maxUses')} inputmode="numeric" bind:value={maxUses} />
	</FormGrid>
	<div class="row"><Button type="submit" disabled={busy}>{t('promo.add')}</Button></div>
</form>
</Section>

<style>
	p {
		margin: 0;
	}
	.codes {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
	}
	.codes li {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		align-items: center;
		gap: var(--gap-related);
		padding: var(--space-3);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
	}
	.codes li > div:first-child {
		display: flex;
		flex-direction: column;
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
