<script lang="ts">
	type Props = {
		label: string;
		type?: 'text' | 'date' | 'email' | 'tel' | 'time' | 'number' | 'password' | 'search';
		value?: string;
		placeholder?: string;
		autocomplete?: AutoFill;
		inputmode?: 'numeric' | 'decimal' | 'text' | 'email' | 'tel' | 'search';
		error?: string;
	};
	let { label, type = 'text', value = $bindable(''), placeholder, autocomplete, inputmode, error }: Props = $props();
	const id = $props.id();
</script>

<div class="field">
	<label for={id}>{label}</label>
	<input
		{id}
		{type}
		{value}
		{placeholder}
		{autocomplete}
		{inputmode}
		aria-invalid={error ? 'true' : undefined}
		aria-describedby={error ? `${id}-error` : undefined}
		oninput={(event) => (value = event.currentTarget.value)}
	/>
	{#if error}<p id="{id}-error" class="error">{error}</p>{/if}
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
	input {
		box-sizing: border-box;
		inline-size: 100%;
		block-size: var(--control-height);
		padding-inline: var(--space-3);
		border: var(--border-width) solid var(--color-border-input);
		border-radius: var(--radius-md);
		background: var(--color-surface);
		color: var(--color-text);
		font: var(--font-input);
	}
	input[aria-invalid='true'] {
		border-color: var(--color-status-danger-dot);
	}
	input:focus-visible {
		outline: var(--border-width-strong) solid var(--color-focus);
		outline-offset: var(--border-width-strong);
	}
	.error {
		margin: 0;
		font: var(--font-caption);
		color: var(--color-status-danger-fg);
	}
</style>
