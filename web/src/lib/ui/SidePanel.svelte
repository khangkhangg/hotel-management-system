<script lang="ts">
	import type { Snippet } from 'svelte';
	import { t } from '#lib/i18n.ts';
	import Button from './Button.svelte';

	type Props = { title: string; wide?: boolean; open?: boolean; dirty?: boolean; onclose?: () => void; children: Snippet; actions?: Snippet };
	let { title, wide = false, open = $bindable(false), dirty = false, onclose, children, actions }: Props = $props();
	let dialog = $state<HTMLDialogElement>();
	let asking = $state(false);
	const titleId = $props.id();

	// The browser's modal dialog traps focus and closes on Escape, like Dialog.
	$effect(() => {
		if (!dialog) return;
		if (open && !dialog.open) {
			if (typeof dialog.showModal === 'function') dialog.showModal();
			else dialog.setAttribute('open', '');
			// The cursor starts in the first field, not on Close.
			queueMicrotask(() => dialog?.querySelector<HTMLElement>('.body input, .body select, .body textarea')?.focus());
		} else if (!open && dialog.open) {
			if (typeof dialog.close === 'function') dialog.close();
			else dialog.removeAttribute('open');
		}
	});

	function finish() {
		if (!open) return;
		asking = false;
		open = false;
		onclose?.();
	}

	// Unsaved changes are never thrown away without asking.
	function request() {
		if (dirty) asking = true;
		else finish();
	}
</script>

<dialog
	class:wide
	bind:this={dialog}
	aria-labelledby={titleId}
	onclose={finish}
	oncancel={(event) => {
		event.preventDefault();
		request();
	}}
>
	<header>
		<h2 id={titleId}>{title}</h2>
		<Button variant="quiet" icon="close" iconOnly onclick={request}>{t('common.close')}</Button>
	</header>
	{#if asking}
		<div class="ask" role="alert">
			<p>{t('panel.discardPrompt')}</p>
			<Button onclick={() => (asking = false)}>{t('common.keepEditing')}</Button>
			<Button variant="secondary" onclick={finish}>{t('common.discard')}</Button>
		</div>
	{/if}
	<div class="body">{@render children()}</div>
	{#if actions}<footer>{@render actions()}</footer>{/if}
</dialog>

<style>
	dialog {
		box-sizing: border-box;
		inset-block: 0;
		inset-inline-start: auto;
		inset-inline-end: 0;
		margin: 0;
		display: none;
		flex-direction: column;
		block-size: 100dvh;
		max-block-size: 100dvh;
		max-inline-size: none;
		inline-size: min(var(--side-panel-width), 100%);
		padding: 0;
		border: var(--border-width) solid var(--color-border);
		border-start-start-radius: var(--radius-panel);
		border-end-start-radius: var(--radius-panel);
		background: var(--color-surface);
		color: var(--color-text);
		box-shadow: var(--shadow-float);
	}
	dialog.wide {
		inline-size: min(var(--drawer-width), 100%);
	}
	dialog[open] {
		display: flex;
	}
	dialog::backdrop {
		background: var(--color-ink);
		opacity: 0.4;
	}
	@media (max-width: 640px) {
		dialog {
			inline-size: 100%;
			border: 0;
			border-radius: 0;
		}
	}
	header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--gap-group);
		padding: var(--pad-card);
		border-block-end: var(--border-width) solid var(--color-divider);
	}
	h2 {
		margin: 0;
		font: var(--font-heading);
	}
	.ask {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--gap-related);
		padding: var(--pad-card);
		background: var(--color-status-warning-bg);
		color: var(--color-status-warning-fg);
	}
	.ask p {
		margin: 0;
		flex: 1 1 100%;
		font: var(--font-body-strong);
	}
	.body {
		flex: 1 1 auto;
		display: flex;
		flex-direction: column;
		gap: var(--gap-group);
		padding: var(--pad-card);
		overflow-y: auto;
	}
	footer {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: var(--gap-related);
		padding: var(--pad-card);
		border-block-start: var(--border-width) solid var(--color-divider);
		background: var(--color-surface);
	}
</style>
