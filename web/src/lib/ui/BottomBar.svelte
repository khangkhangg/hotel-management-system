<script lang="ts">
	import { t } from '#lib/i18n.ts';
	import Icon, { type IconName } from './Icon.svelte';

	type Item = { id: string; label: string; href: string; icon: IconName };
	let { items, active, onmore }: { items: Item[]; active?: string; onmore: () => void } = $props();

	// Four slots: four screens when they fit, otherwise three and More.
	const overflow = $derived(items.length > 4);
	const shown = $derived(overflow ? items.slice(0, 3) : items);
</script>

<nav aria-label={t('bottombar.label')}>
	<ul>
		{#each shown as item (item.id)}
			<li>
				<a href={item.href} aria-current={active === item.id ? 'page' : undefined}><Icon name={item.icon} /><span>{item.label}</span></a>
			</li>
		{/each}
		{#if overflow}
			<li>
				<button type="button" onclick={onmore}><Icon name="list" /><span>{t('bottombar.more')}</span></button>
			</li>
		{/if}
	</ul>
</nav>

<style>
	nav {
		display: none;
		position: fixed;
		inset-inline: 0;
		inset-block-end: 0;
		z-index: var(--layer-header);
		background: var(--color-surface);
		border-block-start: var(--border-width) solid var(--color-border);
		padding-block-end: env(safe-area-inset-bottom, 0);
	}
	@media (width < 640px) {
		nav {
			display: block;
		}
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-auto-flow: column;
		grid-auto-columns: 1fr;
	}
	a,
	button {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: var(--gap-tight);
		inline-size: 100%;
		min-block-size: var(--bottom-bar-height);
		border: 0;
		background: transparent;
		color: var(--color-text-muted);
		font: var(--font-caption);
		text-decoration: none;
		cursor: pointer;
	}
	a[aria-current='page'] {
		color: var(--color-action-ink);
		font-weight: var(--font-weight-semibold);
	}
	a:focus-visible,
	button:focus-visible {
		outline: var(--border-width-strong) solid var(--color-focus);
		outline-offset: calc(var(--border-width-strong) * -1);
	}
</style>
