<script lang="ts">
	import { api, ApiError } from '#lib/api.ts';
	import Button from '#lib/ui/Button.svelte';

	type Message = { sender: 'guest' | 'staff'; body: string; createdAt: string };
	type Props = { token: string; locale: string; t: (key: string, params?: Record<string, string | number>) => string };
	let { token, locale, t }: Props = $props();

	let messages = $state<Message[]>([]);
	let text = $state('');
	let canReview = $state(false);
	let reviewed = $state(false);
	let rating = $state(5);
	let comment = $state('');
	let error = $state('');
	let busy = $state(false);

	async function load() {
		try {
			messages = await api.get<Message[]>(`/api/manage/${token}/messages`);
			const r = await api.get<{ canReview: boolean; reviewed: boolean }>(`/api/manage/${token}/review`);
			canReview = r.canReview;
			reviewed = r.reviewed;
		} catch {
			// The booking above already says when the link is not valid.
		}
	}

	$effect(() => {
		load();
	});

	async function send(event: SubmitEvent) {
		event.preventDefault();
		if (!text.trim()) return;
		error = '';
		busy = true;
		try {
			await api.post(`/api/manage/${token}/messages`, { body: text });
			text = '';
			await load();
		} catch (e) {
			error = e instanceof ApiError ? t(`guest.error.${e.code}`) : t('guest.error.generic');
		}
		busy = false;
	}

	async function review(event: SubmitEvent) {
		event.preventDefault();
		error = '';
		busy = true;
		try {
			await api.post(`/api/manage/${token}/review`, { rating, comment });
			await load();
		} catch (e) {
			error = e instanceof ApiError ? t(`guest.error.${e.code}`) : t('guest.error.generic');
		}
		busy = false;
	}
</script>

{#if error}<p class="error" role="alert">{error}</p>{/if}

{#if canReview}
	<section class="block stack" aria-label={t('manage.review.title')}>
		<h2>{t('manage.review.title')}</h2>
		<form class="stack" onsubmit={review}>
			<fieldset class="rating">
				<legend>{t('manage.review.rating')}</legend>
				{#each [1, 2, 3, 4, 5] as n (n)}
					<label><input type="radio" name="rating" value={n} bind:group={rating} />{n}</label>
				{/each}
			</fieldset>
			<label class="area"><span>{t('manage.review.comment')}</span><textarea rows="4" maxlength="2000" bind:value={comment}></textarea></label>
			<Button type="submit" disabled={busy}>{t('manage.review.send')}</Button>
		</form>
	</section>
{:else if reviewed}
	<p role="status">{t('manage.review.thanks')}</p>
{/if}

<section class="block stack" aria-label={t('manage.chat.title')}>
	<h2>{t('manage.chat.title')}</h2>
	{#if messages.length === 0}
		<p class="muted">{t('manage.chat.empty')}</p>
	{:else}
		<ul class="thread">
			{#each messages as m, i (i)}
				<li class={m.sender}>
					<strong>{m.sender === 'guest' ? t('manage.chat.you') : t('manage.chat.hotel')}</strong>
					<span class="muted">{new Date(m.createdAt).toLocaleString(locale)}</span>
					<p>{m.body}</p>
				</li>
			{/each}
		</ul>
	{/if}
	<form class="stack" onsubmit={send}>
		<label class="area"><span>{t('manage.chat.label')}</span><textarea rows="3" maxlength="2000" bind:value={text}></textarea></label>
		<Button type="submit" disabled={busy || !text.trim()}>{t('manage.chat.send')}</Button>
	</form>
</section>

<style>
	h2 {
		margin: 0;
		font: var(--font-heading);
	}
	p {
		margin: 0;
	}
	.stack {
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
	}
	.block {
		padding: var(--space-4);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
	}
	.rating {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-3);
		margin: 0;
		padding: 0;
		border: 0;
	}
	.rating legend {
		padding: 0;
		font: var(--font-label);
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
	.thread {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.muted {
		color: var(--color-text-muted);
	}
	.error {
		color: var(--color-status-danger-fg);
	}
</style>
