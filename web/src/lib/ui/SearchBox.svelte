<script lang="ts">
	import { t } from '#lib/i18n.ts';
	import Button from './Button.svelte';
	import Icon from './Icon.svelte';

	type Props = { label: string; value?: string; placeholder?: string; delay?: number; onsearch?: (value: string) => void };
	let { label, value = $bindable(''), placeholder, delay = 250, onsearch }: Props = $props();
	let timer: ReturnType<typeof setTimeout> | undefined;

	// The list updates when the person pauses, not on every key.
	function change(next: string) {
		value = next;
		clearTimeout(timer);
		timer = setTimeout(() => onsearch?.(next), delay);
	}
</script>

<div class="search" role="search">
	<span class="glyph"><Icon name="search" /></span>
	<input type="search" aria-label={label} placeholder={placeholder ?? label} {value} oninput={(event) => change(event.currentTarget.value)} />
	{#if value}
		<span class="clear"><Button variant="quiet" icon="close" iconOnly onclick={() => change('')}>{t('search.clear')}</Button></span>
	{/if}
</div>

<style>
	.search {
		position: relative;
		display: flex;
		align-items: center;
		max-inline-size: var(--form-width);
		inline-size: 100%;
	}
	.glyph {
		position: absolute;
		inset-inline-start: var(--space-3);
		display: flex;
		color: var(--color-text-muted);
		pointer-events: none;
	}
	input {
		box-sizing: border-box;
		inline-size: 100%;
		min-block-size: var(--control-height);
		padding-inline: calc(var(--space-3) * 2 + var(--icon-size));
		border: var(--border-width) solid var(--color-border-input);
		border-radius: var(--radius-control);
		background: var(--color-surface);
		color: var(--color-text);
		font: var(--font-input);
	}
	input::-webkit-search-cancel-button {
		display: none;
	}
	input:focus-visible {
		outline: var(--border-width-strong) solid var(--color-focus);
		outline-offset: var(--border-width-strong);
	}
	.clear {
		position: absolute;
		inset-inline-end: 0;
		display: flex;
	}
</style>
