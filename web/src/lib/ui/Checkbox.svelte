<script lang="ts">
	type Props = { label: string; checked?: boolean };
	let { label, checked = $bindable(false) }: Props = $props();
	const id = $props.id();
</script>

<div class="check">
	<input {id} type="checkbox" bind:checked />
	<label for={id}>{label}</label>
</div>

<style>
	.check {
		display: flex;
		align-items: center;
		gap: var(--gap-related);
		min-block-size: var(--control-height);
	}
	input {
		inline-size: var(--size-checkbox);
		block-size: var(--size-checkbox);
		margin: 0;
		accent-color: var(--color-action);
	}
	input:focus-visible {
		outline: var(--border-width-strong) solid var(--color-focus);
		outline-offset: var(--border-width-strong);
	}
	label {
		font: var(--font-body);
		color: var(--color-text);
	}
</style>
