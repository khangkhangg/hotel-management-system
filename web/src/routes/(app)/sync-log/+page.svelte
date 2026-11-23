<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { formatWhen } from '#lib/format.ts';
	import { t } from '#lib/i18n.ts';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import StatusBadge from '#lib/ui/StatusBadge.svelte';

	type Line = { direction: string; status: string; message: string; at: string };

	let lines = $state<Line[]>([]);
	let loaded = $state(false);
	let message = $state('');

	$effect(() => {
		api
			.get<Line[]>('/api/channel/log')
			.then((l) => (lines = l))
			.catch((e) => (message = errorText(e)))
			.finally(() => (loaded = true));
	});
</script>

<svelte:head><title>{t('log.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('log.title')} note={t('log.note')} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

{#if loaded && lines.length === 0}
	<p class="muted">{t('log.empty')}</p>
{:else}
	<ul class="list">
		{#each lines as l, i (i)}
			<li>
				<StatusBadge tone={l.status === 'ok' ? 'success' : 'danger'}>{t(`log.status.${l.status}`)}</StatusBadge>
				<span class="muted">{t(`log.direction.${l.direction}`)} · {formatWhen(l.at)}</span>
				<p>{l.message}</p>
			</li>
		{/each}
	</ul>
{/if}

<style>
	p {
		margin: 0;
	}
	.list {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
		max-inline-size: var(--page-width);
	}
	li {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
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
