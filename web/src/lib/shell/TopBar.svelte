<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '#lib/api.ts';
	import { roleLabels, roles } from '#lib/nav.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import Segmented from '#lib/ui/Segmented.svelte';
	import Select from '#lib/ui/Select.svelte';
	import NotificationBell from './NotificationBell.svelte';

	const viewOptions = [{ id: '', label: t('viewAs.owner') }, ...roles.filter((r) => r !== 'owner').map((r) => ({ id: r, label: roleLabels[r] }))];
	const properties = $derived(session.properties.map((p) => ({ id: p.id, label: p.name })));

	async function signOut() {
		await api.post('/api/sign-out').catch(() => {});
		session.clear();
		await goto('/sign-in');
	}
</script>

<header class="topbar">
	<div class="where">
		{#if properties.length > 1}
			<Select label={t('topbar.property')} hideLabel options={properties} value={session.activeProperty ?? ''} onchange={(id) => session.selectProperty(id)} />
		{:else}
			<span class="name">{session.propertyName}</span>
		{/if}
	</div>

	{#if session.role === 'owner'}
		<div class="view" role="group" aria-label={t('topbar.viewAs')}>
			<div class="wide"><Segmented options={viewOptions} value={session.viewingAs ?? ''} onchange={(id) => session.setViewAs(id as '')} /></div>
			<div class="narrow"><Select label={t('topbar.viewAs')} hideLabel options={viewOptions} value={session.viewingAs ?? ''} onchange={(id) => session.setViewAs(id as '')} /></div>
		</div>
	{/if}

	<div class="end">
		<NotificationBell />
		<Button variant="quiet" icon="logout" iconOnly onclick={signOut}>{t('topbar.signOut')}</Button>
	</div>
</header>

<style>
	.topbar {
		position: sticky;
		inset-block-start: 0;
		z-index: var(--layer-header);
		display: flex;
		align-items: center;
		gap: var(--gap-group);
		min-block-size: var(--topbar-height);
		padding-inline: var(--space-4);
		background: var(--color-surface);
		border-block-end: var(--border-width) solid var(--color-border);
	}
	.where {
		flex: 1 1 0;
		min-inline-size: 0;
	}
	.name {
		display: block;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font: var(--font-heading);
		color: var(--color-text);
	}
	.end {
		display: flex;
		align-items: center;
		gap: var(--gap-tight);
	}
	.wide {
		display: none;
	}
	@media (min-width: 640px) {
		.wide {
			display: block;
		}
		.narrow {
			display: none;
		}
	}
</style>
