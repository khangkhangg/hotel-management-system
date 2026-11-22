<script lang="ts">
	type Props = { label: string; value: string; unit?: string; note?: string; tone?: 'plain' | 'up' | 'down' };
	let { label, value, unit = '', note = '', tone = 'plain' }: Props = $props();
</script>

<div class="tile">
	<div class="label" data-line>{label}</div>
	<div class="value" data-line>{value}<span class="unit">{unit}</span></div>
	{#if note}<div class="note {tone}" data-line>{note}</div>{/if}
</div>

<style>
	.tile {
		box-sizing: border-box;
		block-size: 100%;
		display: flex;
		flex-direction: column;
		gap: var(--gap-tight);
		padding: var(--pad-card);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-lg);
		background: var(--color-surface);
		color: var(--color-text);
	}
	.label {
		font: var(--font-body);
		color: var(--color-text-muted);
	}
	.value {
		font: var(--font-display);
		font-variant-numeric: tabular-nums;
	}
	.unit {
		color: var(--color-text-muted);
	}
	.note {
		font: var(--font-label);
		color: var(--color-text-muted);
	}
	.up { color: var(--color-trend-up); }
	.down { color: var(--color-trend-down); }
</style>
