<script lang="ts">
	import Icon from './Icon.svelte';

	type Option = { id: string; label: string };
	type Props = { label: string; options: Option[]; value?: string; onchange?: (id: string) => void; hideLabel?: boolean };

	let { label, options, value = $bindable(), onchange, hideLabel = false }: Props = $props();
	const id = $props.id();
</script>

<div class="field">
	<label for={id} class:visually-hidden={hideLabel}>{label}</label>
	<div class="control">
		<select {id} bind:value onchange={() => onchange?.(value ?? '')}>
			{#each options as option (option.id)}
				<option value={option.id}>{option.label}</option>
			{/each}
		</select>
		<span class="chevron" aria-hidden="true"><Icon name="down" /></span>
	</div>
</div>

<style>
	.field {
		display: flex;
		flex-direction: column;
		gap: var(--gap-tight);
	}
	label {
		font: var(--font-label);
		color: var(--color-text-subtle);
	}
	.visually-hidden {
		position: absolute;
		inline-size: var(--size-hidden);
		block-size: var(--size-hidden);
		overflow: hidden;
		clip-path: inset(50%);
		white-space: nowrap;
	}
	.control {
		position: relative;
		display: flex;
	}
	select {
		appearance: none;
		box-sizing: border-box;
		inline-size: 100%;
		block-size: var(--control-height);
		padding-inline: var(--space-3) calc(var(--space-3) + var(--icon-size));
		border: var(--border-width) solid var(--color-border-input);
		border-radius: var(--radius-md);
		background: var(--color-surface);
		color: var(--color-text);
		font: var(--font-input);
		cursor: pointer;
	}
	select:focus-visible {
		outline: var(--border-width-strong) solid var(--color-focus);
		outline-offset: var(--border-width-strong);
	}
	.chevron {
		position: absolute;
		inset-block: 0;
		inset-inline-end: var(--space-3);
		display: flex;
		align-items: center;
		color: var(--color-text-muted);
		pointer-events: none;
	}
</style>
