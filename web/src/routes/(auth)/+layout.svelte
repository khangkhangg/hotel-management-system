<script lang="ts">
	import type { Snippet } from 'svelte';
	import { t } from '#lib/i18n.ts';

	let { children }: { children: Snippet } = $props();
</script>

<svelte:head><meta name="robots" content="noindex,nofollow" /></svelte:head>

<main class="auth">
	<div class="column">
		<p class="brand">{t('app.name')}</p>
		{@render children()}
	</div>
</main>

<style>
	.auth {
		min-block-size: 100dvh;
		display: grid;
		place-items: center;
		padding: var(--space-8) var(--space-4);
		background: var(--color-page);
	}
	.column {
		inline-size: 100%;
		max-inline-size: var(--form-width);
		display: flex;
		flex-direction: column;
		gap: var(--gap-section);
		padding: var(--space-6);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
	}
	.brand {
		margin: 0;
		font: var(--font-title);
		color: var(--color-text);
	}
</style>
