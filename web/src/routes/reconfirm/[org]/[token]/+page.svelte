<script lang="ts">
	import { page } from '$app/state';
	import { api, ApiError } from '#lib/api.ts';
	import { pickLocale, translate } from '#lib/guest/locale.ts';
	import Button from '#lib/ui/Button.svelte';

	const locale = pickLocale({ browser: typeof navigator === 'undefined' ? [] : navigator.languages, fallback: 'en' });
	const t = (key: string) => translate(locale, key);

	let phase = $state<'ask' | 'done' | 'invalid'>('ask');
	let busy = $state(false);

	async function confirm() {
		busy = true;
		try {
			await api.post(`/api/reconfirm/${page.params.org}/${page.params.token}`);
			phase = 'done';
		} catch (e) {
			phase = e instanceof ApiError && e.status === 404 ? 'invalid' : 'ask';
		}
		busy = false;
	}
</script>

<svelte:head><title>{t('reconfirm.title')}</title></svelte:head>

<main class="page">
	<h1>{t('reconfirm.title')}</h1>
	{#if phase === 'done'}
		<p role="status">{t('reconfirm.done')}</p>
	{:else if phase === 'invalid'}
		<p role="alert">{t('reconfirm.invalid')}</p>
	{:else}
		<Button disabled={busy} onclick={confirm}>{t('reconfirm.button')}</Button>
	{/if}
</main>

<style>
	.page {
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
		max-inline-size: var(--page-width);
		margin-inline: auto;
		padding: var(--space-6) var(--space-4);
		font: var(--font-body);
	}
	h1 {
		margin: 0;
		font: var(--font-heading);
	}
	p {
		margin: 0;
	}
</style>
