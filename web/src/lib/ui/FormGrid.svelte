<script lang="ts">
	import type { Snippet } from 'svelte';

	// Fields in columns on a wide screen, so short inputs stay short. A field that needs the full row puts class="wide" on its wrapper.
	let { children }: { children: Snippet } = $props();
</script>

<div class="grid">{@render children()}</div>

<style>
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(var(--field-min), 1fr));
		align-items: end;
		gap: var(--gap-related);
	}
	.grid > :global(.wide) {
		grid-column: 1 / -1;
	}
</style>
