<script lang="ts">
	type Option = { id: string; label: string };
	type Props = { options: Option[]; value?: string; onchange?: (id: string) => void };
	let { options, value = $bindable(), onchange }: Props = $props();

	const index = $derived(Math.max(0, options.findIndex((o) => o.id === value)));

	function pick(id: string) {
		value = id;
		onchange?.(id);
	}
</script>

<div class="segmented" style="--count: {options.length}; --index: {index}">
	<span class="thumb"></span>
	{#each options as option (option.id)}
		<button type="button" aria-pressed={option.id === value} onclick={() => pick(option.id)}>{option.label}</button>
	{/each}
</div>

<style>
	.segmented {
		position: relative;
		display: inline-grid;
		grid-template-columns: repeat(var(--count), minmax(0, 1fr));
		box-sizing: border-box;
		block-size: var(--control-height);
		padding: var(--space-1);
		border-radius: var(--radius-md);
		background: var(--color-surface-muted);
	}
	.thumb {
		position: absolute;
		inset-block: var(--space-1);
		inset-inline-start: var(--space-1);
		inline-size: calc((100% - 2 * var(--space-1)) / var(--count));
		border-radius: var(--radius-sm);
		background: var(--color-surface);
		box-shadow: var(--shadow-thumb);
		transform: translateX(calc(var(--index) * 100%));
		transition: transform var(--duration-screen) var(--ease-standard);
	}
	button {
		position: relative;
		block-size: var(--control-height-compact);
		padding-inline: var(--pad-control-x);
		border: 0;
		border-radius: var(--radius-sm);
		background: transparent;
		color: var(--color-text-muted);
		font: var(--font-label);
		font-size: var(--font-size-sm);
		line-height: var(--line-height-sm);
		white-space: nowrap;
		cursor: pointer;
		transition: color var(--duration-fast) var(--ease-out);
	}
	button[aria-pressed='true'] {
		color: var(--color-text);
	}
	button:focus-visible {
		outline: var(--border-width-strong) solid var(--color-focus);
		outline-offset: calc(-1 * var(--border-width-strong));
	}
</style>
