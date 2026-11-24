<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import DataTable from '#lib/ui/DataTable.svelte';
	import Field from '#lib/ui/Field.svelte';
	import FormGrid from '#lib/ui/FormGrid.svelte';
	import SidePanel from '#lib/ui/SidePanel.svelte';
	import { toasts } from '#lib/ui/toast.svelte.ts';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Select from '#lib/ui/Select.svelte';

	type Template = { id: string; name: string; freeCancelUntilDays: number | null; lateCancelPenalty: string; lateCancelPercent: number | null; mealPlan: string };
	type RoomType = { id: string; name: string };

	const penalties = ['none', 'first_night', 'percent', 'full'];

	let templates = $state<Template[]>([]);
	let roomTypes = $state<RoomType[]>([]);
	let roomType = $state('');
	let message = $state('');
	let open = $state(false);
	let name = $state('');
	let days = $state('');
	let penalty = $state('none');
	let percent = $state('');
	let meal = $state('none');

	let loaded = $state(false);
	const columns = [
		{ id: 'name', label: t('chain.name'), value: (x: Template) => x.name, priority: 'main' as const },
		{ id: 'rule', label: t('chain.col.rule'), value: (x: Template) => rule(x), priority: 'detail' as const },
		{ id: 'actions', label: t('staff.col.actions'), value: () => '', priority: 'detail' as const, align: 'end' as const, hideLabel: true }
	];

	async function load() {
		try {
			templates = await api.get<Template[]>('/api/rate-plan-templates');
			roomTypes = await api.get<RoomType[]>('/api/room-types');
			roomType = roomTypes[0]?.id ?? '';
			loaded = true;
		} catch (e) {
			message = errorText(e);
		}
	}

	$effect(() => {
		load();
	});

	async function create(event: SubmitEvent) {
		event.preventDefault();
		message = '';
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
			open = false;
		} catch (e) {
			message = errorText(e);
		}
	}

	async function link(x: Template) {
		message = '';
		try {
			await api.post(`/api/rate-plan-templates/${x.id}/link`, { roomTypeId: roomType });
			toasts.show(t('chain.linked', { name: x.name }));
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

<PageHeader title={t('chain.title')} note={t('chain.note')}>
	{#snippet primary()}{#if session.effectiveRole === 'owner'}<Button icon="plus" onclick={() => (open = true)}>{t('chain.new')}</Button>{/if}{/snippet}
</PageHeader>
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

{#if loaded}
	<Select label={t('chain.roomType')} options={roomTypes.map((r) => ({ id: r.id, label: r.name }))} value={roomType} onchange={(id) => (roomType = id)} />
	<DataTable label={t('chain.title')} rows={templates} {columns} rowKey={(x) => x.id}>
		{#snippet cell(x, column)}
			{#if column.id === 'name'}
				<strong>{x.name}</strong>
			{:else if column.id === 'actions'}
				<Button variant="secondary" disabled={!roomType} onclick={() => link(x)}>{t('chain.link')}</Button>
			{:else}
				{column.value(x)}
			{/if}
		{/snippet}
		{#snippet empty()}<p class="muted">{t('chain.empty')}</p>{/snippet}
	</DataTable>
{/if}

<SidePanel title={t('chain.new')} bind:open dirty={name.trim() !== '' || days !== ''}>
	<form id="chain-form" onsubmit={create}>
		<FormGrid>
			<Field label={t('chain.name')} bind:value={name} />
			<Field label={t('chain.days')} inputmode="numeric" bind:value={days} />
			<Select label={t('chain.latePenalty')} options={penalties.map((p) => ({ id: p, label: t(`chain.penalty.${p}`, { percent: ' ' }) }))} bind:value={penalty} />
			{#if penalty === 'percent'}<Field label={t('chain.percent')} inputmode="numeric" bind:value={percent} />{/if}
			<Field label={t('chain.meal')} bind:value={meal} />
		</FormGrid>
	</form>
	{#snippet actions()}
		<Button type="submit" form="chain-form" disabled={!name.trim()}>{t('chain.add')}</Button>
	{/snippet}
</SidePanel>

<style>
	.muted {
		margin: 0;
		color: var(--color-text-muted);
	}
</style>
