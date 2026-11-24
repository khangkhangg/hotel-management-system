<script lang="ts">
	import { t } from '#lib/i18n.ts';
	import Button from './Button.svelte';
	import Icon from './Icon.svelte';

	type Chip = { id: string; label: string };
	let { chips, onremove, onclear }: { chips: Chip[]; onremove: (id: string) => void; onclear: () => void } = $props();
</script>

{#if chips.length}
	<ul class="chips">
		{#each chips as chip (chip.id)}
			<li class="chip">
				<span>{chip.label}</span>
				<button type="button" aria-label={t('filters.remove', { label: chip.label })} onclick={() => onremove(chip.id)}><Icon name="close" /></button>
			</li>
		{/each}
		{#if chips.length >= 2}<li><Button variant="quiet" onclick={onclear}>{t('filters.clearAll')}</Button></li>{/if}
	</ul>
{/if}

<style>
	.chips {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--gap-related);
	}
	.chip {
		display: inline-flex;
		align-items: center;
		gap: var(--gap-tight);
		min-block-size: var(--control-height-compact);
		padding-inline: var(--space-3) var(--space-1);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-badge);
		background: var(--color-surface-muted);
		color: var(--color-text);
		font: var(--font-control);
	}
	.chip button {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		inline-size: var(--control-height-compact);
		block-size: var(--control-height-compact);
		border: 0;
		border-radius: var(--radius-badge);
		background: transparent;
		color: var(--color-text-subtle);
		cursor: pointer;
	}
	@media (hover: hover) {
		.chip button:hover {
			background: var(--color-button-quiet-bg-hover);
		}
	}
	.chip button:focus-visible {
		outline: var(--border-width-strong) solid var(--color-focus);
	}
</style>
