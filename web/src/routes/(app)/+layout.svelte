<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount, type Snippet } from 'svelte';
	import { MediaQuery } from 'svelte/reactivity';
	import { api, ApiError, onUnauthorized, errorText } from '#lib/api.ts';
	import { drafts, returnTo } from '#lib/draft.ts';
	import { t } from '#lib/i18n.ts';
	import { allowed, homeFor, itemsFor, roleLabels } from '#lib/nav.ts';
	import { session } from '#lib/session.svelte.ts';
	import BottomBar from '#lib/shell/BottomBar.svelte';
	import Sidebar from '#lib/shell/Sidebar.svelte';
	import TopBar from '#lib/shell/TopBar.svelte';
	import Button from '#lib/ui/Button.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';

	let { children }: { children: Snippet } = $props();

	let ready = $state(false);
	let loadError = $state('');
	let collapsed = $state(false);
	let overlayOpen = $state(false);
	const desktop = new MediaQuery('(min-width: 1024px)');

	const role = $derived(session.effectiveRole);
	const items = $derived(role ? itemsFor(role) : []);
	const expanded = $derived(desktop.current ? !collapsed : overlayOpen);
	const path = $derived(page.url.pathname);
	const denied = $derived(ready && role !== null && !allowed(role, path) && path !== '/');

	onMount(() => {
		// A request that finds the session gone keeps where the person was and what they typed, then asks them to sign in again.
		onUnauthorized(() => {
			returnTo.set(location.pathname + location.search);
			drafts.save(location.pathname);
			session.clear();
			goto('/sign-in');
		});
		session
			.load()
			.then(() => {
				collapsed = readSidebar() === 'collapsed';
				ready = true;
			})
			.catch((e) => {
				if (!(e instanceof ApiError && e.status === 401)) loadError = errorText(e); // a 401 was handled above
			});
		return () => onUnauthorized(null);
	});

	// The overview is for owners and managers; everyone else lands on the first item of their menu.
	$effect(() => {
		if (ready && role && path === '/' && !allowed(role, '/')) goto(homeFor(role), { replaceState: true });
	});

	async function signOut() {
		await api.post('/api/sign-out').catch(() => {});
		session.clear();
		await goto('/sign-in');
	}

	const sidebarKey = () => `sidebar:${session.current?.user.id}`;
	function readSidebar() {
		try {
			return localStorage.getItem(sidebarKey());
		} catch {
			return null;
		}
	}
	function toggle() {
		if (!desktop.current) {
			overlayOpen = !overlayOpen;
			return;
		}
		collapsed = !collapsed;
		try {
			localStorage.setItem(sidebarKey(), collapsed ? 'collapsed' : 'expanded');
		} catch {
			/* storage blocked: the choice lasts until the page closes */
		}
	}
</script>

{#if loadError}
	<main class="failed">
		<PageHeader title={loadError} />
		<div class="actions">
			<Button onclick={signOut}>{t('topbar.signOut')}</Button>
			<Button variant="secondary" onclick={() => location.reload()}>{t('common.retry')}</Button>
		</div>
	</main>
{:else if ready && role}
	<div class="shell">
		<Sidebar {items} {expanded} overlay={!desktop.current} hotel={session.current?.organization.name ?? ''} ontoggle={toggle} />
		<div class="main">
			<TopBar />
			{#if session.viewingAs}
				<div class="banner" role="status">
					<span>{t('viewAs.banner', { role: roleLabels[session.viewingAs] })}</span>
					<Button variant="secondary" onclick={() => session.setViewAs('')}>{t('viewAs.back')}</Button>
				</div>
			{/if}
			<main id="main">
				{#key path}
					<div class="screen">
						{#if denied}
							<PageHeader title={t('access.denied.title')} note={t('access.denied.help')} />
						{:else}
							{@render children()}
						{/if}
					</div>
				{/key}
			</main>
		</div>
		<BottomBar {items} />
	</div>
{/if}

<style>
	.failed {
		display: flex;
		flex-direction: column;
		gap: var(--gap-group);
		max-inline-size: var(--form-width);
		margin: var(--space-8) auto;
		padding-inline: var(--space-4);
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: var(--gap-related);
	}
	.shell {
		display: flex;
		min-block-size: 100dvh;
		background: var(--color-page);
	}
	.main {
		flex: 1 1 0;
		min-inline-size: 0;
	}
	@media not all and (min-width: 640px) {
		.main {
			padding-block-end: var(--topbar-height);
		}
	}
	.banner {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--gap-related);
		padding: var(--space-2) var(--space-4);
		background: var(--color-status-warning-bg);
		color: var(--color-status-warning-fg);
		font: var(--font-control);
	}
	main {
		box-sizing: border-box;
		max-inline-size: var(--content-max-width);
		margin-inline: auto;
		padding: var(--space-4);
	}
	@media (min-width: 640px) {
		main {
			padding: var(--space-6);
		}
	}
	.screen {
		display: flex;
		flex-direction: column;
		gap: var(--gap-section);
		animation: arrive var(--duration-screen) var(--ease-standard);
	}
	@keyframes arrive {
		from {
			opacity: 0;
			transform: translateY(var(--space-2));
		}
		to {
			opacity: 1;
			transform: none;
		}
	}
</style>
