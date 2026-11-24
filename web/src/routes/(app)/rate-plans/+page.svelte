<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import type { RatePlan, RoomType } from '#lib/booking/types.ts';
	import { t } from '#lib/i18n.ts';
	import Button from '#lib/ui/Button.svelte';
	import DataTable from '#lib/ui/DataTable.svelte';
	import SidePanel from '#lib/ui/SidePanel.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Select from '#lib/ui/Select.svelte';

	let types = $state<RoomType[]>([]);
	let plans = $state<RatePlan[]>([]);
	let loaded = $state(false);
	let message = $state('');
	let open = $state(false);
	let roomType = $state('');
	let name = $state('');
	let freeDays = $state('2');
	let penalty = $state('first_night');

	const penalties = [
		{ id: 'none', label: 'No penalty' },
		{ id: 'first_night', label: 'First night' },
		{ id: 'full', label: 'Full stay' }
	];

	const columns = [
		{ id: 'name', label: t('rate-plans.name'), value: (p: RatePlan) => p.name, priority: 'main' as const },
		{ id: 'room', label: t('booking.roomType'), value: (p: RatePlan) => types.find((x) => x.id === p.roomTypeId)?.name ?? '', priority: 'detail' as const },
		{ id: 'cancel', label: t('rate-plans.cancellation'), value: (p: RatePlan) => (p.lateCancelPenalty === 'none' ? t('rate-plans.freeAlways') : t('rate-plans.penalty', { penalty: p.lateCancelPenalty.replace('_', ' ') })), priority: 'extra' as const },
		{ id: 'actions', label: t('staff.col.actions'), value: () => '', priority: 'detail' as const, align: 'end' as const, hideLabel: true }
	];

	async function load() {
		try {
			[types, plans] = await Promise.all([api.get<RoomType[]>('/api/room-types'), api.get<RatePlan[]>('/api/rate-plans')]);
			roomType = roomType || types[0]?.id || '';
			loaded = true;
		} catch (e) {
			message = errorText(e);
		}
	}
	$effect(() => {
		load();
	});

	async function save(event: SubmitEvent) {
		event.preventDefault();
		message = '';
		try {
			await api.post('/api/rate-plans', { roomTypeId: roomType, name, freeCancelUntilDays: freeDays === '' ? null : +freeDays, lateCancelPenalty: penalty });
			open = false;
			name = '';
			await load();
		} catch (e) {
			message = errorText(e);
		}
	}
</script>

<svelte:head><title>{t('rate-plans.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('rate-plans.title')} note={t('rate-plans.note')}>
	{#snippet primary()}<Button icon="plus" disabled={types.length === 0} onclick={() => (open = true)}>{t('rate-plans.new')}</Button>{/snippet}
</PageHeader>
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

{#if loaded}
	<DataTable label={t('rate-plans.title')} rows={plans} {columns} rowKey={(p) => p.id}>
		{#snippet cell(p, column)}
			{#if column.id === 'name'}
				<strong>{p.name}</strong>
			{:else if column.id === 'actions'}
				<Button variant="quiet" href={`/rates?plan=${p.id}`}>{t('rate-plans.setRates')}</Button>
			{:else}
				{column.value(p)}
			{/if}
		{/snippet}
		{#snippet empty()}<p class="muted">{t('rate-plans.empty')}</p>{/snippet}
	</DataTable>
{/if}

<SidePanel title={t('rate-plans.new')} bind:open dirty={name.trim() !== ''}>
	<form id="plan-form" class="stack" onsubmit={save}>
		<Select label={t('booking.roomType')} options={types.map((x) => ({ id: x.id, label: x.name }))} bind:value={roomType} />
		<Field label={t('rate-plans.name')} bind:value={name} />
		<Field label={t('rate-plans.freeDays')} inputmode="numeric" bind:value={freeDays} />
		<Select label={t('rate-plans.latePenalty')} options={penalties} bind:value={penalty} />
	</form>
	{#snippet actions()}
		<Button variant="quiet" onclick={() => (open = false)}>{t('common.cancel')}</Button>
		<Button type="submit" form="plan-form" disabled={name.trim() === ''}>{t('rate-plans.create')}</Button>
	{/snippet}
</SidePanel>
