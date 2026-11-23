<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import AttentionPanel from '#lib/alerts/AttentionPanel.svelte';
	import { addDays } from '#lib/booking/dates.ts';
	import type { CalendarData } from '#lib/booking/types.ts';
	import { t } from '#lib/i18n.ts';
	import { moreFor } from '#lib/nav.ts';
	import { session } from '#lib/session.svelte.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import StatTile from '#lib/ui/StatTile.svelte';

	const today = session.today;
	let data = $state<CalendarData | null>(null);
	let message = $state('');

	$effect(() => {
		api.get<CalendarData>(`/api/calendar?from=${addDays(today, -1)}&to=${addDays(today, 1)}`)
			.then((d) => (data = d))
			.catch((e) => (message = errorText(e)));
	});

	const arriving = $derived(data?.stays.filter((s) => s.from === today && (s.status === 'confirmed' || s.status === 'pending')).length ?? 0);
	const leaving = $derived(data?.stays.filter((s) => s.to === today && s.status === 'checked_in').length ?? 0);
	const inHouse = $derived(data?.stays.filter((s) => s.status === 'checked_in' && s.from <= today && s.to > today).length ?? 0);
	const dirty = $derived(data?.rooms.filter((r) => r.status === 'dirty').length ?? 0);
</script>

<svelte:head><title>{t('overview.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('overview.title')} note={t('overview.note')} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

<AttentionPanel />

{#if data}
	<div class="tiles">
		<StatTile label={t('overview.arriving')} value={String(arriving)} note={t('overview.today')} />
		<StatTile label={t('overview.leaving')} value={String(leaving)} note={t('overview.today')} />
		<StatTile label={t('overview.inHouse')} value={String(inHouse)} note={t('overview.tonight')} />
		<StatTile label={t('overview.dirty')} value={String(dirty)} note={t('overview.ofRooms', { n: data.rooms.length })} />
	</div>
{/if}

<nav class="more" aria-label={t('overview.more')}>
	<h2>{t('overview.more')}</h2>
	<ul>
		{#each moreFor(session.effectiveRole ?? 'manager') as item (item.id)}
			<li><a href={item.href}><Icon name={item.icon} />{item.label}</a></li>
		{/each}
	</ul>
</nav>

<style>
	.more {
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
	}
	.more h2 {
		margin: 0;
		font: var(--font-heading);
	}
	.more ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-wrap: wrap;
		gap: var(--gap-related);
	}
	.more a {
		display: inline-flex;
		align-items: center;
		gap: var(--gap-related);
		min-block-size: var(--control-height);
		padding-inline: var(--space-4);
		border: var(--border-width) solid var(--color-border-button);
		border-radius: var(--radius-md);
		background: var(--color-button-secondary-bg);
		color: var(--color-text);
		font: var(--font-control);
		text-decoration: none;
	}
	.more a:focus-visible {
		outline: var(--border-width-strong) solid var(--color-focus);
		outline-offset: var(--border-width-strong);
	}

	.tiles {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(var(--field-min), 1fr));
		gap: var(--gap-group);
	}
</style>
