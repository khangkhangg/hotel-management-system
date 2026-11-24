<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { formatDay } from '#lib/booking/dates.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import DataTable from '#lib/ui/DataTable.svelte';
	import Field from '#lib/ui/Field.svelte';
	import FormGrid from '#lib/ui/FormGrid.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import SidePanel from '#lib/ui/SidePanel.svelte';
	import StatusBadge from '#lib/ui/StatusBadge.svelte';

	type Item = { id: string; description: string; location: string; foundOn: string; status: string; ownerName: string; ownerContact: string; note: string };

	// Housekeeping, managers and owners may log and close items. The front desk may only look.
	const canEdit = $derived(['owner', 'manager', 'housekeeping'].includes(session.role ?? ''));

	let items = $state<Item[]>([]);
	let description = $state('');
	let location = $state('');
	let foundOn = $state(new Date().toISOString().slice(0, 10));
	let adding = $state(false);
	let closing = $state<Item | null>(null);
	let ownerName = $state('');
	let ownerContact = $state('');
	let marked = $state('');
	let message = $state('');
	let ok = $state('');
	let busy = $state(false);

	async function load() {
		try {
			items = await api.get<Item[]>('/api/lost-items');
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

	const reset = () => (description = location = '');

	const add = (event: SubmitEvent) => {
		event.preventDefault();
		return run(async () => {
			await api.post('/api/lost-items', { description, location, foundOn });
			adding = false;
			reset();
		}, t('lost.added'));
	};

	const close = (i: Item, status: 'returned' | 'discarded') =>
		run(async () => {
			await api.put(`/api/lost-items/${i.id}`, { ...i, status, ownerName, ownerContact });
			marked = i.id;
			closing = null;
		}, status === 'returned' ? t('lost.returned') : t('lost.discarded'));

	const columns = [
		{ id: 'item', label: t('lost.col.item'), value: (i: Item) => i.description, priority: 'main' as const },
		{ id: 'where', label: t('lost.col.where'), value: (i: Item) => `${i.location ? `${i.location} · ` : ''}${formatDay(i.foundOn)}`, priority: 'detail' as const },
		{ id: 'owner', label: t('lost.col.owner'), value: (i: Item) => i.ownerName, priority: 'extra' as const },
		{ id: 'status', label: t('lost.col.status'), value: (i: Item) => t(`lost.status.${i.status}`), priority: 'status' as const },
		{ id: 'actions', label: t('lost.col.actions'), value: () => '', priority: 'detail' as const, align: 'end' as const, hideLabel: true }
	];
</script>

<svelte:head><title>{t('lost.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('lost.title')} note={t('lost.note')}>
	{#snippet primary()}{#if canEdit}<Button icon="plus" onclick={() => (adding = true)}>{t('lost.new')}</Button>{/if}{/snippet}
</PageHeader>
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
{#if ok}<p class="notice success" role="status">{ok}</p>{/if}

<DataTable label={t('lost.items')} rows={items} {columns} rowKey={(i) => i.id} {marked}>
	{#snippet cell(i, column)}
		{#if column.id === 'status'}
			<StatusBadge tone={i.status === 'stored' ? 'warning' : i.status === 'returned' ? 'success' : 'neutral'}>{column.value(i)}</StatusBadge>
		{:else if column.id === 'actions'}
			{#if i.status === 'stored' && canEdit}
				<Button variant="secondary" disabled={busy} onclick={() => ((closing = i), (ownerName = ''), (ownerContact = ''))}>{t('lost.handOver')}</Button>
			{/if}
		{:else if column.id === 'item'}
			<strong>{i.description}</strong>
		{:else}
			{column.value(i)}
		{/if}
	{/snippet}
	{#snippet empty()}
		<p class="muted">{t('lost.none')}</p>
	{/snippet}
</DataTable>

<SidePanel title={t('lost.new')} bind:open={adding} dirty={description !== '' || location !== ''} onclose={reset}>
	<form id="lost-form" onsubmit={add}>
		<FormGrid>
			<Field label={t('lost.description')} bind:value={description} />
			<Field label={t('lost.location')} bind:value={location} />
			<Field label={t('lost.foundOn')} type="date" bind:value={foundOn} />
		</FormGrid>
	</form>
	{#snippet actions()}
		<Button type="submit" form="lost-form" disabled={busy}>{t('lost.add')}</Button>
	{/snippet}
</SidePanel>

{#if closing}
	{@const item = closing}
	<SidePanel title={t('lost.handOverTitle', { item: item.description })} open={true} dirty={ownerName !== '' || ownerContact !== ''} onclose={() => (closing = null)}>
		<FormGrid>
			<Field label={t('lost.ownerName')} bind:value={ownerName} />
			<Field label={t('lost.ownerContact')} bind:value={ownerContact} />
		</FormGrid>
		{#snippet actions()}
			<Button variant="quiet" disabled={busy} onclick={() => close(item, 'discarded')}>{t('lost.markDiscarded')}</Button>
			<Button disabled={busy} onclick={() => close(item, 'returned')}>{t('lost.markReturned')}</Button>
		{/snippet}
	</SidePanel>
{/if}

<style>
	.muted {
		margin: 0 0 var(--gap-related);
		color: var(--color-text-muted);
	}
</style>
