<script lang="ts">
	import { page } from '$app/state';
	import { api, ApiError } from '#lib/api.ts';
	import { formatMoney } from '#lib/booking/money.ts';
	import { pickLocale, translate } from '#lib/guest/locale.ts';
	import Button from '#lib/ui/Button.svelte';
	import Field from '#lib/ui/Field.svelte';

	type View = {
		code: string;
		status: string;
		from: string;
		to: string;
		currency: string;
		paid: number;
		cancellable: boolean;
		refund: number;
		penalty: number;
		hotel: string;
		address: string;
	};

	const slug = page.params.slug;
	const token = page.url.searchParams.get('token');
	const locale = pickLocale({ browser: navigator.languages, fallback: 'en' });
	const t = (key: string, params: Record<string, string | number> = {}) => translate(locale, key, params);

	let view = $state<View | null>(null);
	let invalid = $state(false);
	let code = $state('');
	let email = $state('');
	let sent = $state(false);
	let message = $state('');
	let confirming = $state(false);
	let busy = $state(false);

	const money = (n: number) => formatMoney(n, view?.currency ?? 'USD');

	$effect(() => {
		if (!token) return;
		api
			.get<View>(`/api/manage/${token}`)
			.then((v) => (view = v))
			.catch(() => (invalid = true));
	});

	async function requestLink(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		message = '';
		try {
			await api.post(`/api/public/${slug}/manage-link`, { code, email });
			sent = true;
		} catch (e) {
			message = e instanceof ApiError && e.code === 'slow_down' ? t('guest.error.slow_down') : t('guest.error.generic');
		}
		busy = false;
	}

	async function resend() {
		await api.post(`/api/manage/${token}/resend`);
		message = t('manage.resent');
	}

	async function cancel() {
		busy = true;
		try {
			const r = await api.post<{ refund: number; refundPending: boolean }>(`/api/manage/${token}/cancel`);
			message = r.refundPending ? t('manage.cancelledPending') : r.refund > 0 ? t('manage.cancelledRefund', { amount: money(r.refund) }) : t('manage.cancelled');
			if (view) view = { ...view, status: 'cancelled', cancellable: false };
			confirming = false;
		} catch {
			message = t('guest.error.generic');
		}
		busy = false;
	}
</script>

<svelte:head><title>{t('guest.manage')}</title></svelte:head>

<main class="page">
	<h1>{t('guest.manage')}</h1>
	{#if message}<p role="status">{message}</p>{/if}

	{#if !token}
		{#if sent}
			<p role="status">{t('manage.sent')}</p>
		{:else}
			<form class="stack" onsubmit={requestLink}>
				<p class="muted">{t('manage.intro')}</p>
				<Field label={t('guest.done.code')} bind:value={code} />
				<Field label={t('guest.email')} type="email" autocomplete="email" bind:value={email} />
				<Button type="submit" disabled={busy || !code.trim() || !email.trim()}>{t('manage.sendLink')}</Button>
			</form>
		{/if}
	{:else if invalid}
		<p role="alert">{t('manage.invalid')}</p>
		<Button href={`/book/${slug}/manage`}>{t('manage.newLink')}</Button>
	{:else if view}
		<section class="card stack">
			<h2>{view.hotel}</h2>
			<p class="muted">{view.address}</p>
			<p>{t('guest.done.code')}: <strong>{view.code}</strong></p>
			<p>{view.from} → {view.to}</p>
			<p>{t('manage.status')}: <strong>{t(`manage.state.${view.status}`)}</strong></p>
			<p>{t('manage.paid')}: <strong>{money(view.paid)}</strong></p>
			<div class="row">
				<Button variant="secondary" onclick={resend}>{t('manage.resend')}</Button>
				{#if view.cancellable && !confirming}<Button variant="quiet" onclick={() => (confirming = true)}>{t('manage.cancel')}</Button>{/if}
			</div>
			{#if confirming}
				<div class="stack" role="group" aria-label={t('manage.cancel')}>
					<p>
						{#if view.penalty > 0}{t('manage.penalty', { amount: money(view.penalty) })}{:else if view.refund > 0}{t('manage.refundWill', { amount: money(view.refund) })}{:else}{t('manage.nothingToRefund')}{/if}
					</p>
					<div class="row">
						<Button onclick={cancel} disabled={busy}>{t('manage.confirmCancel')}</Button>
						<Button variant="quiet" onclick={() => (confirming = false)}>{t('manage.keep')}</Button>
					</div>
				</div>
			{/if}
			<p class="muted">{t('manage.change')}</p>
		</section>
	{/if}
</main>

<style>
	h1,
	h2 {
		margin: 0;
		font: var(--font-heading);
	}
	p {
		margin: 0;
	}
	.page {
		max-inline-size: var(--page-width);
		margin-inline: auto;
		padding: var(--space-4);
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
		font: var(--font-body);
	}
	.stack {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}
	.card {
		padding: var(--space-4);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
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
