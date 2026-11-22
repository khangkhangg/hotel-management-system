<script lang="ts">
	import type { Snippet } from 'svelte';

	type Props = { padded?: boolean; label?: string; children: Snippet };
	let { padded = true, label, children }: Props = $props();
</script>

<section class="card" class:padded aria-label={label}>{@render children()}</section>

<style>
	.card {
		box-sizing: border-box;
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-lg);
		background: var(--color-surface);
		color: var(--color-text);
	}
	.padded {
		padding: var(--pad-card);
	}
</style>
