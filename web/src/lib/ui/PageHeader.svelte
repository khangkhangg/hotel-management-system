<script lang="ts">
	import type { Snippet } from 'svelte';

	type Props = { title: string; note?: string; secondary?: Snippet; primary?: Snippet };
	let { title, note, secondary, primary }: Props = $props();
</script>

<header class="page-header">
	<div class="text">
		<h1>{title}</h1>
		{#if note}<div class="note">{note}</div>{/if}
	</div>
	<div class="actions">
		{#if secondary}{@render secondary()}{/if}
		{#if primary}{@render primary()}{/if}
	</div>
</header>

<style>
	.page-header {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--gap-group);
		color: var(--color-text);
	}
	.text {
		flex: 1 1 var(--hovercard-width);
		min-inline-size: 0;
	}
	h1 {
		margin: 0;
		font: var(--font-heading);
	}
	.note {
		font: var(--font-body);
		color: var(--color-text-muted);
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: var(--gap-related);
	}
</style>
