<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, errorText } from '#lib/api.ts';
	import { formatWhen } from '#lib/format.ts';
	import { t } from '#lib/i18n.ts';
	import { roleLabels, type Role } from '#lib/nav.ts';
	import { session } from '#lib/session.svelte.ts';
	import { listen } from '#lib/sse.ts';
	import Button from '#lib/ui/Button.svelte';
	import type { Colleague, Thread, ThreadItem } from './types.ts';

	let { reservationId, version = 0 }: { reservationId: string; version?: number } = $props();

	// What the typed handle stands for: a colleague, or everyone with a role.
	type Pick = { label: string; hint: string; userId?: string; role?: string };
	const roleHandles: Pick[] = [
		{ label: 'frontdesk', hint: 'Everyone at the front desk', role: 'front_desk' },
		{ label: 'housekeeping', hint: 'Everyone in housekeeping', role: 'housekeeping' },
		{ label: 'revenue', hint: 'The revenue team', role: 'revenue' },
		{ label: 'managers', hint: 'The managers', role: 'manager' }
	];

	let thread = $state<Thread>({ pinned: [], items: [] });
	let text = $state('');
	let mentions = $state<{ userId?: string; role?: string }[]>([]);
	let pinned = $state(false);
	let picks = $state<Pick[]>([]);
	let pickIndex = $state(0);
	let message = $state('');
	let warn = $state(false);
	let sending = $state(false);
	let box = $state<HTMLTextAreaElement>();

	const load = async () => (thread = await api.get<Thread>(`/api/reservations/${reservationId}/thread`));

	// The booking changed (a check-in, a payment): its events belong in the thread too.
	$effect(() => {
		version;
		load().catch(() => {});
	});

	onMount(() => {
		load().catch((e) => (message = errorText(e)));
		// Another person's note appears here at once, without a reload.
		const stop = listen(`/api/reservations/${reservationId}/thread/stream`, session.activeProperty ? { 'X-Property-Id': session.activeProperty } : {}, () => {
			load().catch(() => {});
		});
		return stop;
	});

	async function suggest() {
		const caret = box?.selectionStart ?? text.length;
		const m = /(?:^|\s)@(\w*)$/.exec(text.slice(0, caret));
		if (!m) {
			picks = [];
			return;
		}
		const q = m[1].toLocaleLowerCase('en');
		const roles = roleHandles.filter((r) => r.label.startsWith(q));
		const people = await api.get<Colleague[]>(`/api/colleagues?q=${encodeURIComponent(q)}`).catch(() => []);
		picks = [...people.map((p) => ({ label: p.name, hint: roleLabels[p.role as Role] ?? p.role, userId: p.id })), ...roles].slice(0, 6);
		pickIndex = 0;
	}

	function choose(p: Pick) {
		const caret = box?.selectionStart ?? text.length;
		const before = text.slice(0, caret).replace(/@\w*$/, '');
		text = `${before}@${p.label} ${text.slice(caret)}`;
		mentions = [...mentions, p.userId ? { userId: p.userId } : { role: p.role }];
		picks = [];
		box?.focus();
	}

	function keydown(event: KeyboardEvent) {
		if (picks.length === 0) return;
		if (event.key === 'ArrowDown') (event.preventDefault(), (pickIndex = (pickIndex + 1) % picks.length));
		else if (event.key === 'ArrowUp') (event.preventDefault(), (pickIndex = (pickIndex + picks.length - 1) % picks.length));
		else if (event.key === 'Enter') (event.preventDefault(), choose(picks[pickIndex]));
		else if (event.key === 'Escape') picks = [];
	}

	async function send(acknowledged = false) {
		sending = true;
		message = '';
		try {
			// Only handles still in the text count, so deleting a tag before sending removes it.
			await api.post(`/api/reservations/${reservationId}/notes`, { body: text, mentions, pinned, acknowledged });
			text = '';
			mentions = [];
			pinned = false;
			warn = false;
			await load();
		} catch (e) {
			if (e instanceof ApiError && e.code === 'contains_guest_name') warn = true;
			else message = errorText(e);
		} finally {
			sending = false;
		}
	}

	async function act(run: () => Promise<unknown>) {
		message = '';
		try {
			await run();
			await load();
		} catch (e) {
			message = errorText(e);
		}
	}

	const youAreTagged = (it: ThreadItem) => it.mentions?.some((m) => !m.done && (m.name === session.current?.user.name || m.role === session.effectiveRole)) ?? false;
	const canRemove = (it: ThreadItem) => it.mine || session.role === 'owner' || session.role === 'manager';
	const eventWords: Record<string, string> = {
		'reservation.created': 'created the booking',
		'reservation.cancelled': 'cancelled the booking',
		'reservation.checked_in': 'checked the guest in',
		'reservation.checked_out': 'checked the guest out',
		'reservation.no_show': 'marked a no-show',
		'reservation.room_assigned': 'assigned a room',
		'reservation.moved': 'moved the booking to another room type',
		'payment.recorded': 'recorded a payment',
		'alert.accepted': 'applied a suggested fix'
	};
</script>

<section class="thread" aria-label={t('thread.title')}>
	<h3>{t('thread.title')}</h3>

	{#if thread.pinned.length}
		<ul class="pinned" aria-label={t('thread.pinned')}>
			{#each thread.pinned as it (it.id)}
				<li><span class="pin">{t('thread.pin')}</span> {it.body}</li>
			{/each}
		</ul>
	{/if}

	{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

	<ol class="items">
		{#each thread.items as it (it.type + (it.id ?? it.at + it.action))}
			{#if it.type === 'event'}
				<li class="event"><span>{it.author} {eventWords[it.action ?? ''] ?? it.action}</span> <time datetime={it.at}>{formatWhen(it.at)}</time></li>
			{:else}
				<li class="note" class:removed={it.removed}>
					<p class="head">
						<span class="who">{it.author}</span>
						{#if it.authorRole}<span class="role">{roleLabels[it.authorRole as Role] ?? it.authorRole}</span>{/if}
						<time datetime={it.at}>{formatWhen(it.at)}</time>
						{#if it.edited}<span class="role">{t('thread.edited')}</span>{/if}
					</p>
					{#if it.removed}
						<p class="body muted">{t('thread.removed', { name: it.removedBy ?? '' })}</p>
					{:else}
						<p class="body">{it.body}</p>
						{#if it.mentions?.length}
							<p class="tags">
								{#each it.mentions as m}
									<span class="tag" class:done={m.done}>@{m.name || m.role}{m.done ? ' ✓' : ''}</span>
								{/each}
							</p>
						{/if}
						<p class="row">
							{#if youAreTagged(it)}<Button variant="secondary" onclick={() => act(() => api.post(`/api/notes/${it.id}/done`))}>{t('thread.done')}</Button>{/if}
							{#if it.mine || session.role === 'manager' || session.role === 'owner'}
								<Button variant="quiet" onclick={() => act(() => api.patch(`/api/notes/${it.id}`, { pinned: !it.pinned }))}>{it.pinned ? t('thread.unpin') : t('thread.pin')}</Button>
							{/if}
							{#if canRemove(it)}<Button variant="quiet" onclick={() => act(() => api.delete(`/api/notes/${it.id}`))}>{t('thread.remove')}</Button>{/if}
						</p>
					{/if}
				</li>
			{/if}
		{/each}
	</ol>

	<form
		class="composer"
		onsubmit={(e) => {
			e.preventDefault();
			send();
		}}
	>
		<label for="note-{reservationId}" class="label">{t('thread.write')}</label>
		<div class="box">
			<textarea id="note-{reservationId}" bind:this={box} bind:value={text} oninput={suggest} onkeydown={keydown} rows="3" maxlength="2000" placeholder={t('thread.placeholder')}></textarea>
			{#if picks.length}
				<ul class="picker" role="listbox" aria-label={t('thread.tag')}>
					{#each picks as p, i (p.label + (p.userId ?? p.role))}
						<li role="option" aria-selected={i === pickIndex}>
							<button type="button" class:active={i === pickIndex} onclick={() => choose(p)}><span>{p.label}</span><small>{p.hint}</small></button>
						</li>
					{/each}
				</ul>
			{/if}
		</div>
		{#if warn}
			<p class="notice danger" role="alert">{t('error.contains_guest_name')}</p>
			<div class="row"><Button variant="secondary" onclick={() => send(true)}>{t('thread.sendAnyway')}</Button></div>
		{/if}
		<div class="row">
			<label class="pinbox"><input type="checkbox" bind:checked={pinned} /> {t('thread.pinNote')}</label>
			<Button type="submit" disabled={sending || text.trim() === ''}>{t('thread.send')}</Button>
		</div>
	</form>
</section>

<style>
	.thread {
		display: flex;
		flex-direction: column;
		gap: var(--gap-group);
	}
	h3 {
		margin: 0;
		font: var(--font-heading);
	}
	.pinned {
		list-style: none;
		margin: 0;
		padding: var(--space-3);
		border-radius: var(--radius-md);
		background: var(--color-status-warning-bg);
		color: var(--color-status-warning-fg);
		font: var(--font-body);
	}
	.pin {
		font: var(--font-label);
	}
	.items {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
	}
	.event {
		display: flex;
		justify-content: space-between;
		gap: var(--gap-related);
		font: var(--font-caption);
		color: var(--color-text-muted);
	}
	.note {
		padding: var(--space-3);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
	}
	p {
		margin: 0;
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1) var(--gap-related);
		align-items: baseline;
		font: var(--font-caption);
		color: var(--color-text-muted);
	}
	.who {
		font: var(--font-control);
		color: var(--color-text);
	}
	.body {
		margin-block-start: var(--space-1);
		font: var(--font-body);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.tags,
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--gap-tight);
		margin-block-start: var(--space-2);
	}
	.tag {
		padding-inline: var(--space-2);
		border-radius: var(--radius-full);
		background: var(--color-action-wash);
		color: var(--color-action-ink);
		font: var(--font-caption);
	}
	.tag.done {
		background: var(--color-status-success-bg);
		color: var(--color-status-success-fg);
	}
	.composer {
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
	}
	.label {
		font: var(--font-label);
		color: var(--color-text-subtle);
	}
	.box {
		position: relative;
	}
	textarea {
		box-sizing: border-box;
		inline-size: 100%;
		padding: var(--space-3);
		border: var(--border-width) solid var(--color-border-input);
		border-radius: var(--radius-md);
		background: var(--color-surface);
		color: var(--color-text);
		font: var(--font-input);
		resize: vertical;
	}
	textarea:focus-visible {
		outline: var(--border-width-strong) solid var(--color-focus);
		outline-offset: var(--border-width-strong);
	}
	.picker {
		position: absolute;
		inset-inline: 0;
		inset-block-start: 100%;
		z-index: var(--layer-popover);
		list-style: none;
		margin: var(--space-1) 0 0;
		padding: var(--space-1);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
		box-shadow: var(--shadow-float);
	}
	.picker button {
		display: flex;
		justify-content: space-between;
		gap: var(--gap-related);
		inline-size: 100%;
		padding: var(--space-2) var(--space-3);
		border: 0;
		border-radius: var(--radius-sm);
		background: transparent;
		color: var(--color-text);
		font: var(--font-body);
		text-align: start;
		cursor: pointer;
	}
	.picker button.active {
		background: var(--color-action-wash);
	}
	.picker small {
		color: var(--color-text-muted);
	}
	.pinbox {
		display: flex;
		align-items: center;
		gap: var(--gap-tight);
		margin-inline-end: auto;
		font: var(--font-body);
		color: var(--color-text-subtle);
	}
	.muted {
		color: var(--color-text-muted);
	}
</style>
