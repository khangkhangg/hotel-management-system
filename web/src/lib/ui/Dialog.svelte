<script lang="ts">
	import type { Snippet } from 'svelte';
	import Button from './Button.svelte';

	type Props = { title: string; open?: boolean; wide?: boolean; onclose?: () => void; children: Snippet; actions?: Snippet };

	let { title, open = $bindable(false), wide = false, onclose, children, actions }: Props = $props();
	let dialog = $state<HTMLDialogElement>();
	const titleId = $props.id();

	// The browser's own modal dialog traps focus, closes on Escape and dims the page behind it.
	$effect(() => {
		if (!dialog) return;
		if (open && !dialog.open) {
			if (typeof dialog.showModal === 'function') dialog.showModal();
			else dialog.setAttribute('open', '');
		}
		if (!open && dialog.open) {
			if (typeof dialog.close === 'function') dialog.close();
			else dialog.removeAttribute('open');
		}
	});

	function close() {
		open = false;
		onclose?.();
	}
</script>

<dialog bind:this={dialog} class:wide aria-labelledby={titleId} onclose={close} oncancel={close}>
	<header>
		<h2 id={titleId}>{title}</h2>
		<Button variant="quiet" icon="close" iconOnly onclick={close}>Close</Button>
	</header>
	<div class="body">{@render children()}</div>
	{#if actions}<footer>{@render actions()}</footer>{/if}
</dialog>

<style>
	dialog {
		box-sizing: border-box;
		inline-size: min(var(--form-width), calc(100% - 2 * var(--space-4)));
		padding: var(--pad-card);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-lg);
		background: var(--color-surface);
		color: var(--color-text);
		box-shadow: var(--shadow-float);
	}
	dialog.wide {
		inline-size: min(var(--drawer-width), calc(100% - 2 * var(--space-4)));
		max-block-size: calc(100dvh - 2 * var(--space-4));
		overflow-y: auto;
	}
	dialog::backdrop {
		background: var(--color-ink);
		opacity: 0.4;
	}
	header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--gap-group);
	}
	h2 {
		margin: 0;
		font: var(--font-heading);
	}
	.body {
		display: flex;
		flex-direction: column;
		gap: var(--gap-group);
		margin-block: var(--space-4);
	}
	footer {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: var(--gap-related);
	}
</style>
