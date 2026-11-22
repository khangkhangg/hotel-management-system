<script lang="ts">
	import { tick, type Snippet } from 'svelte';
	import StatusBadge from './StatusBadge.svelte';

	type Tone = 'success' | 'warning' | 'danger' | 'info' | 'neutral';
	type Props = {
		title: string;
		badge?: string;
		tone?: Tone;
		lines: { label: string; value: string }[];
		note?: string;
		hint?: string;
		trigger: Snippet<[{ 'aria-describedby': string }]>;
	};
	let { title, badge, tone = 'neutral', lines, note, hint, trigger }: Props = $props();

	const id = $props.id();
	const OPEN_DELAY = 300; // the spec's hover delay; a behaviour rule, not a style value
	let open = $state(false);
	let above = $state(false);
	let card = $state<HTMLElement>();
	let timer: ReturnType<typeof setTimeout> | undefined;

	async function show() {
		clearTimeout(timer);
		open = true;
		await tick();
		above = !!card && card.getBoundingClientRect().bottom > window.innerHeight;
	}
	function hide() {
		clearTimeout(timer);
		open = false;
		above = false;
	}
	function hover() {
		clearTimeout(timer);
		timer = setTimeout(show, OPEN_DELAY);
	}
</script>

<span
	class="anchor"
	role="presentation"
	onmouseenter={hover}
	onmouseleave={hide}
	onfocusin={show}
	onfocusout={hide}
	onkeydown={(event) => event.key === 'Escape' && hide()}
>
	{@render trigger({ 'aria-describedby': id })}
	{#if open}
		<div {id} bind:this={card} role="tooltip" class="card" class:above>
			<div class="head">
				<div class="title">{title}</div>
				{#if badge}<StatusBadge {tone}>{badge}</StatusBadge>{/if}
			</div>
			<dl class="lines">
				{#each lines as line (line.label)}
					<div class="line"><dt>{line.label}</dt><dd>{line.value}</dd></div>
				{/each}
			</dl>
			{#if note}<div class="note">{note}</div>{/if}
			{#if hint}<div class="hint">{hint}</div>{/if}
		</div>
	{/if}
</span>

<style>
	.anchor {
		position: relative;
		display: inline-block;
	}
	.card {
		position: absolute;
		inset-inline-start: 0;
		inset-block-start: calc(100% + var(--space-1));
		z-index: var(--layer-popover);
		box-sizing: border-box;
		inline-size: var(--hovercard-width);
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
		padding: var(--space-3);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-lg);
		background: var(--color-surface);
		box-shadow: var(--shadow-float);
		color: var(--color-text);
		font: var(--font-body);
		pointer-events: none;
	}
	.above {
		inset-block-start: auto;
		inset-block-end: calc(100% + var(--space-1));
	}
	.head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--gap-related);
	}
	.title {
		font: var(--font-body-strong);
		min-inline-size: 0;
	}
	.lines {
		display: flex;
		flex-direction: column;
		gap: var(--gap-tight);
		margin: 0;
	}
	.line {
		display: flex;
		justify-content: space-between;
		gap: var(--gap-group);
	}
	dt {
		color: var(--color-text-muted);
	}
	dd {
		margin: 0;
		text-align: end;
		font-variant-numeric: tabular-nums;
	}
	.note {
		padding-block-start: var(--space-2);
		border-block-start: var(--border-width) solid var(--color-divider);
		font: var(--font-body-strong);
		color: var(--color-status-warning-fg);
	}
	.hint {
		font: var(--font-caption);
		color: var(--color-text-muted);
	}
</style>
