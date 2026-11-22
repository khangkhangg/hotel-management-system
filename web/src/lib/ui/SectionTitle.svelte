<script lang="ts">
	type Props = { title: string; note?: string; level?: 2 | 3 };
	let { title, note, level = 2 }: Props = $props();
</script>

<div class="section-title">
	<svelte:element this={`h${level}`} class="title">{title}</svelte:element>
	{#if note}<div class="note">{note}</div>{/if}
</div>

<style>
	.title {
		margin: 0;
		font: var(--font-title);
		color: var(--color-text);
	}
	.note {
		font: var(--font-caption);
		color: var(--color-text-muted);
	}
</style>
