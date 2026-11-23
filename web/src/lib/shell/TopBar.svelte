<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '#lib/api.ts';
	import { roleLabels, roles, type Role } from '#lib/nav.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
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
		<div class="view">
			<span class="viewlabel">{t('topbar.viewAs')}</span>
			<div class="viewselect"><Select label={t('topbar.viewAs')} hideLabel options={viewOptions} value={session.viewingAs ?? ''} onchange={(id) => session.setViewAs(id as Role | '')} /></div>
		</div>
	{/if}

	<div class="end">
		<NotificationBell />
		<span class="me">{session.current?.user.name ?? ''}</span>
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
	.view {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		color: var(--color-text-muted);
		font: var(--font-label);
	}
	.viewlabel {
		display: none;
	}
	.viewselect {
		inline-size: var(--card-min);
		max-inline-size: 40vw;
	}
	@media (min-width: 640px) {
		.viewlabel {
			display: inline;
		}
	}
	.me {
		display: none;
		color: var(--color-text-muted);
		font: var(--font-label);
	}
	@media (min-width: 640px) {
		.me {
			display: inline;
		}
	}
	.end {
		display: flex;
		align-items: center;
		gap: var(--gap-tight);
	}
	</style>
