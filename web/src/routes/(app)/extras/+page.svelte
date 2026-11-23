<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { formatMoney, parseMoney } from '#lib/booking/money.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Select from '#lib/ui/Select.svelte';

	type Extra = { id: string; name: string; kind: string; unit: string; price: number; active: boolean };

	const kinds = ['service', 'tour', 'product'];
	const units = ['each', 'per_night', 'per_person', 'per_person_night'];
	const currency = $derived(session.currency);

	let extras = $state<Extra[]>([]);
	let message = $state('');
	let name = $state('');
	let kind = $state('service');
	let unit = $state('each');
	let price = $state('');

	async function load() {
		try {
			extras = await api.get<Extra[]>('/api/extras?all=1');
		} catch (e) {
			message = errorText(e);
		}
	}

	$effect(() => {
		load();
	});

	async function add(event: SubmitEvent) {
		event.preventDefault();
		message = '';
		const cents = parseMoney(price, currency);
		if (!cents || cents < 1) {
			message = t('extras.badPrice');
			return;
		}
		try {
			await api.post('/api/extras', { name, kind, unit, price: cents });
			name = '';
			price = '';
			await load();
		} catch (e) {
			message = errorText(e);
		}
	}

	async function toggle(x: Extra) {
		message = '';
		try {
			await api.patch(`/api/extras/${x.id}`, { active: !x.active });
			await load();
		} catch (e) {
			message = errorText(e);
		}
	}
</script>

<svelte:head><title>{t('extras.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('extras.title')} note={t('extras.note')} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

<form class="card" onsubmit={add}>
	<h2>{t('extras.new')}</h2>
	<Field label={t('extras.name')} bind:value={name} />
	<Select label={t('extras.kind')} options={kinds.map((k) => ({ id: k, label: t(`extras.kind.${k}`) }))} bind:value={kind} />
	<Select label={t('extras.unit')} options={units.map((u) => ({ id: u, label: t(`extras.unit.${u}`) }))} bind:value={unit} />
	<Field label={t('extras.price')} inputmode="decimal" bind:value={price} />
	<div><Button type="submit" disabled={!name.trim() || !price.trim()}>{t('extras.add')}</Button></div>
</form>

{#if extras.length === 0}
	<p class="muted">{t('extras.empty')}</p>
{:else}
	<ul class="list">
		{#each extras as x (x.id)}
			<li class="card" class:off={!x.active}>
				<h3>{x.name}</h3>
				<p class="muted">{t(`extras.kind.${x.kind}`)} · {formatMoney(x.price, currency)} {t(`extras.unit.${x.unit}`)}</p>
				<div><Button variant="secondary" onclick={() => toggle(x)}>{x.active ? t('extras.switchOff') : t('extras.switchOn')}</Button></div>
			</li>
		{/each}
	</ul>
{/if}

<style>
	h2,
	h3 {
		margin: 0;
		font: var(--font-heading);
	}
	p {
		margin: 0;
	}
	.list {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(var(--card-min), 1fr));
		gap: var(--gap-related);
	}
	.card {
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
		padding: var(--space-4);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
		font: var(--font-body);
	}
	.off {
		color: var(--color-text-muted);
	}
	.muted {
		color: var(--color-text-muted);
	}
</style>
