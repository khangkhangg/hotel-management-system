<script lang="ts">
	import { page } from '$app/state';
	import type { NavItem } from '#lib/nav.ts';
	import { t } from '#lib/i18n.ts';
	import Icon from '#lib/ui/Icon.svelte';

	let { items }: { items: NavItem[] } = $props();

	const current = (href: string) => (href === '/' ? page.url.pathname === '/' : page.url.pathname === href || page.url.pathname.startsWith(href + '/'));
</script>

<nav class="bottom" aria-label={t('nav.bottom')}>
	<ul style="--count: {items.length}">
		{#each items as item (item.id)}
			<li>
				<a href={item.href} aria-current={current(item.href) ? 'page' : undefined}>
					<Icon name={item.icon} />
					<span>{item.label}</span>
				</a>
			</li>
		{/each}
	</ul>
</nav>

<style>
	.bottom {
		position: fixed;
		inset-inline: 0;
		inset-block-end: 0;
		z-index: var(--layer-sticky);
		background: var(--color-surface);
		border-block-start: var(--border-width) solid var(--color-border);
		box-shadow: var(--shadow-bar);
		padding-block-end: env(safe-area-inset-bottom);
	}
	@media (min-width: 640px) {
		.bottom {
			display: none;
		}
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(var(--count), minmax(0, 1fr));
	}
	a {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: var(--space-1);
		min-block-size: var(--bottom-bar-height);
		padding-inline: var(--space-1);
		color: var(--color-text-subtle);
		font: var(--font-caption);
		text-decoration: none;
		text-align: center;
	}
	a[aria-current='page'] {
		color: var(--color-action-ink);
		background: var(--color-action-wash);
	}
	a:focus-visible {
		outline: var(--border-width-strong) solid var(--color-focus);
		outline-offset: calc(-1 * var(--border-width-strong));
	}
</style>
