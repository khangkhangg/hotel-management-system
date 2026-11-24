<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import FormGrid from '#lib/ui/FormGrid.svelte';
	import Section from '#lib/ui/Section.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';

	type Item = { id: string; description: string; location: string; foundOn: string; status: string; ownerName: string; ownerContact: string; note: string };

	// Housekeeping, managers and owners may log and close items. The front desk may only look.
	const canEdit = $derived(['owner', 'manager', 'housekeeping'].includes(session.role ?? ''));

	let items = $state<Item[]>([]);
	let description = $state('');
	let location = $state('');
	let foundOn = $state(new Date().toISOString().slice(0, 10));
	let returning = $state<Record<string, { name: string; contact: string }>>({});
	let loaded = $state(false);
	let message = $state('');
	let ok = $state('');
	let busy = $state(false);

	async function load() {
		try {
			items = await api.get<Item[]>('/api/lost-items');
			returning = Object.fromEntries(items.map((i) => [i.id, { name: i.ownerName, contact: i.ownerContact }]));
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

	const add = (event: SubmitEvent) => {
		event.preventDefault();
		return run(async () => {
			await api.post('/api/lost-items', { description, location, foundOn });
			description = location = '';
		}, t('lost.added'));
	};

	const close = (i: Item, status: 'returned' | 'discarded') =>
		run(() => api.put(`/api/lost-items/${i.id}`, { ...i, status, ownerName: returning[i.id]?.name ?? '', ownerContact: returning[i.id]?.contact ?? '' }), t(status === 'returned' ? 'lost.returned' : 'lost.discarded'));
</script>

<svelte:head><title>{t('lost.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('lost.title')} note={t('lost.note')} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
{#if ok}<p class="notice success" role="status">{ok}</p>{/if}

{#if loaded}
	<Section title={t('lost.items')}>
		{#if items.length === 0}
			<p class="muted">{t('lost.none')}</p>
		{:else}
			<ul class="items">
				{#each items as i (i.id)}
					<li class:off={i.status !== 'stored'}>
						<strong>{i.description}</strong>
						<span class="muted">{i.location ? `${i.location} · ` : ''}{i.foundOn} · {t(`lost.status.${i.status}`)}{i.ownerName ? ` · ${i.ownerName}` : ''}</span>
						{#if i.status === 'stored' && canEdit}
							<Field label={t('lost.ownerName')} bind:value={returning[i.id].name} />
							<Field label={t('lost.ownerContact')} bind:value={returning[i.id].contact} />
							<div class="row">
								<Button variant="secondary" disabled={busy} onclick={() => close(i, 'returned')}>{t('lost.markReturned')}</Button>
								<Button variant="quiet" disabled={busy} onclick={() => close(i, 'discarded')}>{t('lost.markDiscarded')}</Button>
							</div>
						{/if}
					</li>
				{/each}
			</ul>
		{/if}
	</Section>
{/if}

{#if canEdit}
	<Section title={t('lost.new')}>
	<form class="stack" onsubmit={add}>
		<FormGrid>
		<Field label={t('lost.description')} bind:value={description} />
		<Field label={t('lost.location')} bind:value={location} />
		<Field label={t('lost.foundOn')} type="date" bind:value={foundOn} />
		</FormGrid>
		<div class="row"><Button type="submit" disabled={busy}>{t('lost.add')}</Button></div>
	</form>
	</Section>
{/if}

<style>
	p {
		margin: 0;
	}
	.items {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
	}
	.items li {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: var(--space-3);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
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
