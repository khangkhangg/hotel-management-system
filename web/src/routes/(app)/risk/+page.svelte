<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { t } from '#lib/i18n.ts';
	import Button from '#lib/ui/Button.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';

	type Item = { reservationId: string; code: string; guest: string; score: number; level: string; reasons: { code: string; text: string }[]; status: string; createdAt: string };

	let items = $state<Item[]>([]);
	let loaded = $state(false);
	let message = $state('');
	let ok = $state('');
	let busy = $state(false);

	async function load() {
		try {
			items = await api.get<Item[]>('/api/risk');
			loaded = true;
		} catch (e) {
			message = errorText(e);
		}
	}

	$effect(() => {
		load();
	});

	async function decide(i: Item, verb: 'approve' | 'decline') {
		message = ok = '';
		busy = true;
		try {
			await api.post(`/api/risk/${i.reservationId}/${verb}`);
			await load();
			ok = t(`risk.${verb}d`, { code: i.code });
		} catch (e) {
			message = errorText(e);
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>{t('risk.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('risk.title')} note={t('risk.note')} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
{#if ok}<p class="notice success" role="status">{ok}</p>{/if}

{#if loaded}
	{#if items.length === 0}
		<p class="muted">{t('risk.none')}</p>
	{:else}
		<ul class="items">
			{#each items as i (i.reservationId)}
				<li class="card">
					<h2>{t('risk.booking', { code: i.code, guest: i.guest })}</h2>
					<p class="muted">{t('risk.score', { score: i.score })}</p>
					<ul class="reasons">
						{#each i.reasons as r (r.code)}<li>{r.text}</li>{/each}
					</ul>
					<div class="row">
						<Button disabled={busy} onclick={() => decide(i, 'approve')}>{t('risk.approve')}</Button>
						<Button variant="secondary" disabled={busy} onclick={() => decide(i, 'decline')}>{t('risk.decline')}</Button>
					</div>
					<p class="muted">{t('risk.declineNote')}</p>
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
	.reasons {
		margin: 0;
		padding-inline-start: var(--space-6);
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
