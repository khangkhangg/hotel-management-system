<script lang="ts">
	import { page } from '$app/state';
	import type { NavItem } from '#lib/nav.ts';
	import { t } from '#lib/i18n.ts';
	import Button from '#lib/ui/Button.svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import StatusBadge from '#lib/ui/StatusBadge.svelte';

	type Props = { items: NavItem[]; expanded: boolean; overlay: boolean; hotel: string; role: string; viewing: boolean; ontoggle: () => void };
	let { items, expanded, overlay, hotel, role, viewing, ontoggle }: Props = $props();

	const current = (href: string) => (href === '/' ? page.url.pathname === '/' : page.url.pathname === href || page.url.pathname.startsWith(href + '/'));
</script>

{#if overlay && expanded}
	<button class="backdrop" type="button" aria-label={t('nav.close')} onclick={ontoggle}></button>
{/if}

<aside class="sidebar" class:expanded class:overlay>
	<div class="head">
		{#if expanded}
			<div class="brand">
				<span class="hotel">{hotel}</span>
				<StatusBadge tone={viewing ? 'warning' : 'info'}>{role}</StatusBadge>
			</div>
		{/if}
		<Button variant="quiet" icon="panel" iconOnly onclick={ontoggle}>{expanded ? t('nav.collapse') : t('nav.expand')}</Button>
	</div>
	<nav aria-label={t('nav.main')}>
		<ul>
			{#each items as item (item.id)}
				<li>
					<a href={item.href} aria-current={current(item.href) ? 'page' : undefined} title={expanded ? undefined : item.label} onclick={() => overlay && expanded && ontoggle()}>
						<Icon name={item.icon} />
						<span class="label">{item.label}</span>
					</a>
				</li>
			{/each}
		</ul>
	</nav>
</aside>

<style>
	.sidebar {
		display: none;
		flex-direction: column;
		gap: var(--gap-group);
		box-sizing: border-box;
		inline-size: var(--rail-width);
		flex: none;
		padding: var(--space-3);
		background: var(--color-surface);
		border-inline-end: var(--border-width) solid var(--color-border);
		transition: inline-size var(--duration-slide) var(--ease-standard);
		overflow: hidden;
	}
	@media (min-width: 640px) {
		.sidebar {
			display: flex;
			position: sticky;
			inset-block-start: 0;
			block-size: 100dvh;
		}
	}
	.sidebar.expanded {
		inline-size: var(--sidebar-width);
	}
	.sidebar.overlay.expanded {
		position: fixed;
		inset-block: 0;
		inset-inline-start: 0;
		z-index: var(--layer-panel);
		box-shadow: var(--shadow-float);
	}
	.backdrop {
		position: fixed;
		inset: 0;
		z-index: var(--layer-panel);
		border: 0;
		background: var(--color-ink);
		opacity: 0.3;
	}
	.head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--gap-related);
		min-block-size: var(--control-height);
	}
	.brand {
		flex: 1 1 auto;
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: var(--space-1);
		min-inline-size: 0;
		padding-block: var(--space-2);
	}
	.hotel {
		max-inline-size: 100%;
		font: var(--font-heading);
		color: var(--color-text);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	a {
		display: flex;
		align-items: center;
		gap: var(--gap-related);
		min-block-size: var(--control-height);
		padding-inline: var(--space-3);
		border-radius: var(--radius-md);
		color: var(--color-text-subtle);
		font: var(--font-control);
		text-decoration: none;
		white-space: nowrap;
	}
	@media (hover: hover) {
		a:hover {
			background: var(--color-button-quiet-bg-hover);
			color: var(--color-text);
		}
	}
	a[aria-current='page'] {
		background: var(--color-action-wash);
		color: var(--color-action-ink);
	}
	a:focus-visible {
		outline: var(--border-width-strong) solid var(--color-focus);
		outline-offset: var(--border-width-strong);
	}
	.sidebar:not(.expanded) .label {
		position: absolute;
		inline-size: var(--size-hidden);
		block-size: var(--size-hidden);
		overflow: hidden;
		clip-path: inset(50%);
	}
</style>
