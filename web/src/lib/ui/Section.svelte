<script lang="ts">
	import type { Snippet } from 'svelte';

	// One block of a screen: a heading, a line that says what it is for, and the content. Every screen builds from these,
	// so spacing, borders and headings are the same everywhere.
	type Props = { title: string; note?: string; actions?: Snippet; children: Snippet };
	let { title, note, actions, children }: Props = $props();
</script>

<section class="section">
	<header>
		<div>
			<h2>{title}</h2>
			{#if note}<p class="note">{note}</p>{/if}
		</div>
		{#if actions}<div class="actions">{@render actions()}</div>{/if}
	</header>
	{@render children()}
</section>

<style>
	.section {
		display: flex;
		flex-direction: column;
		gap: var(--gap-group);
		padding: var(--space-4);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
		font: var(--font-body);
	}
	header {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		justify-content: space-between;
		gap: var(--gap-related);
	}
	h2 {
		margin: 0;
		font: var(--font-heading);
	}
	.note {
		margin: var(--space-1) 0 0;
		color: var(--color-text-muted);
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: var(--gap-related);
	}
</style>
