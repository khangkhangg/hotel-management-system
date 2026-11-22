<script lang="ts">
	import { onMount } from 'svelte';
	import { api, errorText } from '#lib/api.ts';
	import { actionOptions, describeChange, describeEntry, type Entry } from '#lib/activity.ts';
	import { formatWhen } from '#lib/format.ts';
	import { t } from '#lib/i18n.ts';
	import { roleLabels, type Role } from '#lib/nav.ts';
	import Button from '#lib/ui/Button.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Select from '#lib/ui/Select.svelte';

	type Page = { items: Entry[]; nextCursor: string | null };

	let entries = $state<Entry[]>([]);
	let cursor = $state<string | null>(null);
	let action = $state('');
	let loaded = $state(false);
	let message = $state('');

	async function load(more: boolean) {
		message = '';
		const params = new URLSearchParams({ limit: '25' });
		if (action) params.set('action', action);
		if (more && cursor) params.set('cursor', cursor);
		try {
			const page = await api.get<Page>(`/api/activity?${params}`);
			entries = more ? [...entries, ...page.items] : page.items;
			cursor = page.nextCursor;
			loaded = true;
		} catch (e) {
			message = errorText(e);
		}
	}

	onMount(() => {
		load(false);
	});

	const who = (e: Entry) => {
		const role = roleLabels[e.actor.role as Role] ?? e.actor.role;
		return e.viewAs ? `${role}, viewing as ${roleLabels[e.viewAs as Role] ?? e.viewAs}` : role;
	};
</script>

<svelte:head><title>{t('activity.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('activity.title')} note={t('activity.note')} />

<div class="filters">
	<Select label={t('activity.filter')} options={actionOptions} bind:value={action} onchange={() => load(false)} />
</div>

{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

{#if loaded && entries.length === 0}
	<p class="muted">{t('activity.empty')}</p>
{:else}
	<ol class="list">
		{#each entries as entry (entry.id)}
			<li>
				<p class="line">
					<span class="actor">{entry.actor.name || t('activity.system')}</span>
					{describeEntry(entry)}
				</p>
				<p class="meta">
					<span>{who(entry)}</span>
					<time datetime={entry.at}>{formatWhen(entry.at)}</time>
					{#if entry.ip}<span>{entry.ip}</span>{/if}
				</p>
				{#if describeChange(entry)}<p class="change">{describeChange(entry)}</p>{/if}
				{#if entry.reason}<p class="change">{entry.reason}</p>{/if}
			</li>
		{/each}
	</ol>
	{#if cursor}<div><Button variant="secondary" onclick={() => load(true)}>{t('activity.more')}</Button></div>{/if}
{/if}

<style>
	.filters {
		max-inline-size: var(--form-width);
	}
	.list {
		list-style: none;
		margin: 0;
		padding: 0;
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-lg);
		background: var(--color-surface);
	}
	li {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		padding: var(--space-3) var(--space-4);
		border-block-end: var(--border-width) solid var(--color-divider);
	}
	li:last-child {
		border-block-end: 0;
	}
	p {
		margin: 0;
	}
	.line {
		font: var(--font-body);
		color: var(--color-text);
	}
	.actor {
		font: var(--font-control);
	}
	.meta {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1) var(--gap-group);
		font: var(--font-caption);
		color: var(--color-text-muted);
	}
	.change {
		font: var(--font-caption);
		color: var(--color-text-subtle);
	}
</style>
