<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Select from '#lib/ui/Select.svelte';

	type Template = { id: string; name: string; freeCancelUntilDays: number | null; lateCancelPenalty: string; lateCancelPercent: number | null; mealPlan: string };
	type RoomType = { id: string; name: string };

	const penalties = ['none', 'first_night', 'percent', 'full'];

	let templates = $state<Template[]>([]);
	let roomTypes = $state<RoomType[]>([]);
	let roomType = $state('');
	let message = $state('');
	let ok = $state('');
	let name = $state('');
	let days = $state('');
	let penalty = $state('none');
	let percent = $state('');
	let meal = $state('none');

	async function load() {
		try {
			templates = await api.get<Template[]>('/api/rate-plan-templates');
			roomTypes = await api.get<RoomType[]>('/api/room-types');
			roomType = roomTypes[0]?.id ?? '';
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
			await api.post('/api/rate-plan-templates', {
				name,
				freeCancelUntilDays: days.trim() === '' ? null : Number(days),
				lateCancelPenalty: penalty,
				lateCancelPercent: penalty === 'percent' ? Number(percent) : null,
				mealPlan: meal
			});
			name = days = percent = '';
			await load();
		} catch (e) {
			message = errorText(e);
		}
	}

	async function link(x: Template) {
		message = ok = '';
		try {
			await api.post(`/api/rate-plan-templates/${x.id}/link`, { roomTypeId: roomType });
			ok = t('chain.linked', { name: x.name });
		} catch (e) {
			message = errorText(e);
		}
	}

	const rule = (x: Template) =>
		[
			x.freeCancelUntilDays === null ? t('chain.noDeadline') : t('chain.freeUntil', { days: x.freeCancelUntilDays }),
			t(`chain.penalty.${x.lateCancelPenalty}`, { percent: x.lateCancelPercent ?? 0 }),
			x.mealPlan === 'none' ? t('chain.noMeal') : x.mealPlan
		].join(' · ');
</script>

<svelte:head><title>{t('chain.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('chain.title')} note={t('chain.note')} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
{#if ok}<p class="notice success" role="status">{ok}</p>{/if}

{#if session.effectiveRole === 'owner'}
	<form class="card" onsubmit={create}>
		<h2>{t('chain.new')}</h2>
		<Field label={t('chain.name')} bind:value={name} />
		<Field label={t('chain.days')} inputmode="numeric" bind:value={days} />
		<Select label={t('chain.latePenalty')} options={penalties.map((p) => ({ id: p, label: t(`chain.penalty.${p}`, { percent: '' }) }))} bind:value={penalty} />
		{#if penalty === 'percent'}<Field label={t('chain.percent')} inputmode="numeric" bind:value={percent} />{/if}
		<Field label={t('chain.meal')} bind:value={meal} />
		<div><Button type="submit" disabled={!name.trim()}>{t('chain.add')}</Button></div>
	</form>
{/if}

{#if templates.length === 0}
	<p class="muted">{t('chain.empty')}</p>
{:else}
	<Select label={t('chain.roomType')} options={roomTypes.map((r) => ({ id: r.id, label: r.name }))} bind:value={roomType} />
	<ul class="list">
		{#each templates as x (x.id)}
			<li class="card">
				<h3>{x.name}</h3>
				<p class="muted">{rule(x)}</p>
				<div><Button variant="secondary" disabled={!roomType} onclick={() => link(x)}>{t('chain.link')}</Button></div>
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
	.muted {
		color: var(--color-text-muted);
	}
</style>
