<script lang="ts">
	import type { Snippet } from 'svelte';

	type Props = { tone: 'success' | 'warning' | 'danger' | 'info' | 'neutral'; children: Snippet };
	let { tone, children }: Props = $props();
</script>

<span class="badge {tone}"><span class="dot"></span>{@render children()}</span>

<style>
	.badge {
		display: inline-flex;
		align-items: center;
		gap: var(--gap-related);
		block-size: var(--line-height-md);
		box-sizing: border-box;
		padding-inline: var(--space-2) var(--space-3);
		border-radius: var(--radius-full);
		font: var(--font-caption);
		font-weight: var(--font-weight-semibold);
		white-space: nowrap;
	}
	.dot {
		inline-size: var(--dot-size);
		block-size: var(--dot-size);
		border-radius: var(--radius-full);
	}
	.success { background: var(--color-status-success-bg); color: var(--color-status-success-fg); }
	.success .dot { background: var(--color-status-success-dot); }
	.warning { background: var(--color-status-warning-bg); color: var(--color-status-warning-fg); }
	.warning .dot { background: var(--color-status-warning-dot); }
	.danger { background: var(--color-status-danger-bg); color: var(--color-status-danger-fg); }
	.danger .dot { background: var(--color-status-danger-dot); }
	.info { background: var(--color-status-info-bg); color: var(--color-status-info-fg); }
	.info .dot { background: var(--color-status-info-dot); }
	.neutral { background: var(--color-status-neutral-bg); color: var(--color-status-neutral-fg); }
	.neutral .dot { background: var(--color-status-neutral-dot); }
</style>
