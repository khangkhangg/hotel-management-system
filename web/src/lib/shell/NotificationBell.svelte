<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import { formatWhen } from '#lib/format.ts';
	import { t } from '#lib/i18n.ts';
	import Button from '#lib/ui/Button.svelte';
	import Icon from '#lib/ui/Icon.svelte';

	type Item = { id: string; level: string; title: string; body: string; actionURL: string; count: number; openedAt: string | null; resolvedAt: string | null };
	type Listing = { unread: number; items: Item[] };

	let listing = $state<Listing>({ unread: 0, items: [] });
	let open = $state(false);
	let root = $state<HTMLElement>();

	const refresh = async () => (listing = await api.get<Listing>('/api/notifications'));

	onMount(() => {
		refresh().catch(() => {});
		// New notifications arrive on a server-sent stream, so the count changes without a reload.
		const stream = new EventSource('/api/notifications/stream');
		stream.addEventListener('notification', (event) => (listing = JSON.parse((event as MessageEvent).data)));
		return () => stream.close();
	});

	async function openItem(item: Item) {
		open = false;
		if (!item.openedAt) await api.post(`/api/notifications/${item.id}/open`).catch(() => {});
		await refresh().catch(() => {});
		if (item.actionURL) await goto(item.actionURL);
	}

	async function openAll() {
		await api.post('/api/notifications/open-all');
		await refresh();
	}

	function outside(event: PointerEvent) {
		if (open && root && !root.contains(event.target as Node)) open = false;
	}
	function escape(event: KeyboardEvent) {
		if (open && event.key === 'Escape') open = false;
	}
</script>

<svelte:window onpointerdown={outside} onkeydown={escape} />

<div class="bell" bind:this={root}>
	<button
		type="button"
		class="trigger"
		aria-expanded={open}
		aria-label={listing.unread > 0 ? t('bell.labelUnread', { count: listing.unread }) : t('bell.label')}
		onclick={() => (open = !open)}
	>
		<Icon name="bell" />
		{#if listing.unread > 0}<span class="count" aria-hidden="true">{listing.unread}</span>{/if}
	</button>

	{#if open}
		<section class="panel" aria-label={t('bell.label')}>
			<header>
				<h2>{t('bell.label')}</h2>
				{#if listing.unread > 0}<Button variant="quiet" onclick={openAll}>{t('bell.markAll')}</Button>{/if}
			</header>
			{#if listing.items.length === 0}
				<p class="muted">{t('bell.empty')}</p>
			{:else}
				<ul>
					{#each listing.items as item (item.id)}
						<li>
							<button type="button" class="item" class:unread={!item.openedAt && !item.resolvedAt} onclick={() => openItem(item)}>
								<span class="title">{item.title}{#if item.count > 1} <span class="times">× {item.count}</span>{/if}</span>
								{#if item.body}<span class="body">{item.body}</span>{/if}
							</button>
						</li>
					{/each}
				</ul>
			{/if}
		</section>
	{/if}
</div>

<style>
	.bell {
		position: relative;
	}
	.trigger {
		position: relative;
		display: grid;
		place-items: center;
		inline-size: var(--control-height);
		block-size: var(--control-height);
		border: 0;
		border-radius: var(--radius-md);
		background: transparent;
		color: var(--color-text);
		cursor: pointer;
	}
	@media (hover: hover) {
		.trigger:hover {
			background: var(--color-button-quiet-bg-hover);
		}
	}
	.trigger:focus-visible,
	.item:focus-visible {
		outline: var(--border-width-strong) solid var(--color-focus);
		outline-offset: var(--border-width-strong);
	}
	.count {
		position: absolute;
		inset-block-start: var(--space-1);
		inset-inline-end: var(--space-1);
		min-inline-size: var(--space-4);
		padding-inline: var(--space-1);
		border-radius: var(--radius-full);
		background: var(--color-status-danger-dot);
		color: var(--color-text-inverse);
		font: var(--font-caption);
		line-height: var(--space-4);
		text-align: center;
	}
	.panel {
		position: absolute;
		inset-block-start: calc(100% + var(--space-2));
		inset-inline-end: 0;
		z-index: var(--layer-popover);
		inline-size: min(var(--panel-width), calc(100vw - 2 * var(--space-4)));
		max-block-size: 70dvh;
		overflow-y: auto;
		padding: var(--pad-card);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-lg);
		background: var(--color-surface);
		box-shadow: var(--shadow-float);
	}
	header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--gap-related);
	}
	h2 {
		margin: 0;
		font: var(--font-heading);
	}
	ul {
		list-style: none;
		margin: var(--space-3) 0 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.item {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		inline-size: 100%;
		padding: var(--space-3);
		border: 0;
		border-radius: var(--radius-md);
		background: transparent;
		color: var(--color-text-muted);
		font: var(--font-body);
		text-align: start;
		cursor: pointer;
	}
	@media (hover: hover) {
		.item:hover {
			background: var(--color-button-quiet-bg-hover);
		}
	}
	.item.unread {
		background: var(--color-action-wash);
		color: var(--color-text);
	}
	.title {
		font: var(--font-control);
		color: var(--color-text);
	}
	.times {
		color: var(--color-text-muted);
	}
	.muted {
		margin: var(--space-3) 0 0;
		color: var(--color-text-muted);
		font: var(--font-body);
	}
</style>
