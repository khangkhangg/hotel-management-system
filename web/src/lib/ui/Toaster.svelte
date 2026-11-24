<script lang="ts">
	import Button from './Button.svelte';
	import { toasts } from './toast.svelte.ts';
</script>

<div class="toaster" aria-live="polite">
	{#each toasts.items as toast (toast.id)}
		<div class="toast" role="status">
			<span>{toast.message}</span>
			{#if toast.undo}
				{@const undo = toast.undo}
				<Button
					variant="quiet"
					onclick={() => {
						undo.run();
						toasts.dismiss(toast.id);
					}}>{undo.label}</Button
				>
			{/if}
		</div>
	{/each}
</div>

<style>
	.toaster {
		position: fixed;
		inset-inline-end: var(--space-4);
		inset-block-end: var(--space-4);
		z-index: var(--layer-toast);
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
		max-inline-size: min(var(--form-width), calc(100vw - var(--space-4) * 2));
		pointer-events: none;
	}
	.toast {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--gap-group);
		padding: var(--space-3) var(--space-4);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-control);
		background: var(--color-ink);
		color: var(--color-text-inverse);
		font: var(--font-body-strong);
		box-shadow: var(--shadow-float);
		pointer-events: auto;
		animation: rise var(--duration-fast) var(--ease-standard);
	}
	@media (width < 640px) {
		.toaster {
			inset-inline: var(--space-4);
			inset-block-end: calc(var(--bottom-bar-height) + var(--space-4));
			max-inline-size: none;
		}
	}
	@keyframes rise {
		from {
			opacity: 0;
		}
		to {
			opacity: 1;
		}
	}
</style>
