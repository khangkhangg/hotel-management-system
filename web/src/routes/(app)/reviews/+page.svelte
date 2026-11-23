<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { t } from '#lib/i18n.ts';
	import Button from '#lib/ui/Button.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';

	type Review = { id: string; rating: number; comment: string; name: string; reply: string; createdAt: string; status: string };

	let reviews = $state<Review[]>([]);
	let replies = $state<Record<string, string>>({});
	let loaded = $state(false);
	let message = $state('');
	let ok = $state('');
	let busy = $state(false);

	async function load() {
		try {
			reviews = await api.get<Review[]>('/api/reviews');
			replies = Object.fromEntries(reviews.map((r) => [r.id, r.reply]));
			loaded = true;
		} catch (e) {
			message = errorText(e);
		}
	}

	$effect(() => {
		load();
	});

	async function run(f: () => Promise<void>) {
		message = ok = '';
		busy = true;
		try {
			await f();
			await load();
			ok = t('reviews.saved');
		} catch (e) {
			message = errorText(e);
		} finally {
			busy = false;
		}
	}

	const toggle = (r: Review) => run(() => api.post(`/api/reviews/${r.id}/${r.status === 'published' ? 'hide' : 'show'}`));
	const reply = (r: Review) => run(() => api.post(`/api/reviews/${r.id}/reply`, { text: replies[r.id] ?? '' }));
</script>

<svelte:head><title>{t('reviews.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('reviews.title')} note={t('reviews.note')} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
{#if ok}<p class="notice success" role="status">{ok}</p>{/if}

{#if loaded}
	{#if reviews.length === 0}
		<p class="muted">{t('reviews.none')}</p>
	{:else}
		<ul class="items">
			{#each reviews as r (r.id)}
				<li class="card" class:off={r.status === 'hidden'}>
					<h2>{r.name} · {t('reviews.rating', { rating: r.rating })}{r.status === 'hidden' ? ` · ${t('reviews.hidden')}` : ''}</h2>
					<p class="muted">{new Date(r.createdAt).toLocaleDateString()}</p>
					{#if r.comment}<p>{r.comment}</p>{/if}
					<Field label={t('reviews.replyLabel')} bind:value={replies[r.id]} />
					<div class="row">
						<Button variant="secondary" disabled={busy} onclick={() => reply(r)}>{t('reviews.reply')}</Button>
						<Button variant="quiet" disabled={busy} onclick={() => toggle(r)}>{r.status === 'published' ? t('reviews.hide') : t('reviews.show')}</Button>
					</div>
				</li>
			{/each}
		</ul>
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
	.items {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
	}
	.card {
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
		max-inline-size: var(--page-width);
		padding: var(--space-4);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
		font: var(--font-body);
	}
	.off h2 {
		color: var(--color-text-muted);
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: var(--gap-related);
	}
	.muted {
		color: var(--color-text-muted);
	}
</style>
