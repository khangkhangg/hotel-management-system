<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon, { type IconName } from './Icon.svelte';

	type Props = {
		variant?: 'primary' | 'secondary' | 'quiet';
		icon?: IconName;
		iconOnly?: boolean;
		block?: boolean;
		disabled?: boolean;
		href?: string;
		onclick?: (event: MouseEvent) => void;
		children: Snippet;
	};
	let { variant = 'primary', icon, iconOnly = false, block = false, disabled = false, href, onclick, children }: Props = $props();
</script>

{#snippet inner()}
	{#if icon}<Icon name={icon} />{/if}
	<span class:visually-hidden={iconOnly}>{@render children()}</span>
{/snippet}

{#if href}
	<a class="button {variant}" class:block class:icon-only={iconOnly} {href}>{@render inner()}</a>
{:else}
	<button type="button" class="button {variant}" class:block class:icon-only={iconOnly} {disabled} {onclick}>{@render inner()}</button>
{/if}

<style>
	.button {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: var(--gap-related);
		box-sizing: border-box;
		min-inline-size: var(--control-height);
		block-size: var(--control-height);
		padding-inline: var(--pad-control-x);
		border: var(--border-width) solid transparent;
		border-radius: var(--radius-md);
		font: var(--font-control);
		white-space: nowrap;
		text-decoration: none;
		cursor: pointer;
		transition:
			background-color var(--duration-fast) var(--ease-out),
			border-color var(--duration-fast) var(--ease-out);
	}
	.block {
		inline-size: 100%;
	}
	.icon-only {
		padding-inline: 0;
	}
	.visually-hidden {
		position: absolute;
		inline-size: var(--size-hidden);
		block-size: var(--size-hidden);
		overflow: hidden;
		clip-path: inset(50%);
		white-space: nowrap;
	}

	.primary {
		background: var(--color-action);
		border-color: var(--color-action);
		color: var(--color-action-text);
	}
	.secondary {
		background: var(--color-button-secondary-bg);
		border-color: var(--color-border-button);
		color: var(--color-text);
	}
	.quiet {
		background: transparent;
		color: var(--color-text);
	}
	@media (hover: hover) {
		.primary:hover:not(:disabled) {
			background: var(--color-action-hover);
			border-color: var(--color-action-hover);
		}
		.secondary:hover:not(:disabled) {
			background: var(--color-button-secondary-bg-hover);
			border-color: var(--color-border-button-hover);
		}
		.quiet:hover:not(:disabled) {
			background: var(--color-button-quiet-bg-hover);
		}
	}
	.button:focus-visible {
		outline: var(--border-width-strong) solid var(--color-focus);
		outline-offset: var(--border-width-strong);
	}
	.button:disabled {
		background: var(--color-disabled-bg);
		border-color: var(--color-disabled-bg);
		color: var(--color-disabled-text);
		cursor: not-allowed;
	}
</style>
