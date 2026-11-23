<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { t } from '#lib/i18n.ts';
	import Button from '#lib/ui/Button.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';

	type Thread = { reservationId: string; code: string; guest: string; last: string; lastSender: string; lastAt: string; unread: number };
	type Message = { sender: 'guest' | 'staff'; body: string; createdAt: string };

	let threads = $state<Thread[]>([]);
	let open = $state<Thread | null>(null);
	let messages = $state<Message[]>([]);
	let text = $state('');
	let loaded = $state(false);
	let message = $state('');
	let ok = $state('');
	let busy = $state(false);

	async function load() {
		try {
			threads = await api.get<Thread[]>('/api/messages');
			loaded = true;
		} catch (e) {
			message = errorText(e);
		}
	}

	$effect(() => {
		load();
	});

	async function select(th: Thread) {
		open = th;
		message = ok = '';
		try {
			messages = await api.get<Message[]>(`/api/reservations/${th.reservationId}/messages`);
			await load();
		} catch (e) {
			message = errorText(e);
		}
	}

	async function send(event: SubmitEvent) {
		event.preventDefault();
		if (!open || !text.trim()) return;
		busy = true;
		message = ok = '';
		try {
			await api.post(`/api/reservations/${open.reservationId}/messages`, { body: text });
			text = '';
			messages = await api.get<Message[]>(`/api/reservations/${open.reservationId}/messages`);
			ok = t('messages.sent');
		} catch (e) {
			message = errorText(e);
		}
		busy = false;
	}
</script>

<svelte:head><title>{t('messages.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('messages.title')} note={t('messages.note')} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
{#if ok}<p class="notice success" role="status">{ok}</p>{/if}

{#if loaded}
	{#if threads.length === 0}
		<p class="muted">{t('messages.none')}</p>
	{:else}
		<ul class="threads" aria-label={t('messages.title')}>
			{#each threads as th (th.reservationId)}
				<li>
					<button type="button" class:on={open?.reservationId === th.reservationId} aria-pressed={open?.reservationId === th.reservationId} onclick={() => select(th)}>
						<strong>{th.guest} · {th.code}</strong>
						<span class="muted">{th.last}</span>
						{#if th.unread > 0}<span class="badge">{t('messages.unread', { count: th.unread })}</span>{/if}
					</button>
				</li>
			{/each}
		</ul>
	{/if}

	{#if open}
		<section class="card" aria-label={`${open.guest} · ${open.code}`}>
			<h2>{open.guest} · {open.code}</h2>
			<ul class="thread">
				{#each messages as m, i (i)}
					<li class={m.sender}>
						<strong>{m.sender === 'guest' ? t('messages.guest') : t('messages.hotel')}</strong>
						<span class="muted">{new Date(m.createdAt).toLocaleString()}</span>
						<p>{m.body}</p>
					</li>
				{/each}
			</ul>
			<form class="stack" onsubmit={send}>
				<label class="area"><span>{t('messages.reply')}</span><textarea rows="3" maxlength="2000" bind:value={text}></textarea></label>
				<div class="row"><Button type="submit" disabled={busy || !text.trim()}>{t('messages.send')}</Button></div>
			</form>
		</section>
	{/if}
{/if}

<style>
	h2 {
		margin: 0;
		font: var(--font-heading);
	}
	p {
		margin: 0;
	}
	.threads,
	.thread {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
		max-inline-size: var(--page-width);
	}
	.threads button {
		inline-size: 100%;
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: var(--space-1);
		padding: var(--space-3);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
		color: var(--color-text);
		font: var(--font-body);
		text-align: start;
		cursor: pointer;
	}
	.threads button.on {
		border-color: var(--color-focus);
	}
	.badge {
		font: var(--font-label);
		color: var(--color-status-warning-fg);
	}
	.card,
	.stack {
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
	}
	.card {
		max-inline-size: var(--page-width);
		padding: var(--space-4);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
		font: var(--font-body);
	}
	.area {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		font: var(--font-label);
	}
	textarea {
		padding: var(--space-2) var(--space-3);
		border: var(--border-width) solid var(--color-border-input);
		border-radius: var(--radius-md);
		background: var(--color-surface);
		color: var(--color-text);
		font: var(--font-body);
	}
	.row {
		display: flex;
		gap: var(--gap-related);
	}
	.muted {
		color: var(--color-text-muted);
	}
</style>
