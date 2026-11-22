<script lang="ts">
	import { page } from '$app/state';
	import { api, ApiError, errorText } from '#lib/api.ts';
	import { t } from '#lib/i18n.ts';
	import Button from '#lib/ui/Button.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';

	let password = $state('');
	let done = $state(false);
	let linkGone = $state(false);
	let message = $state('');
	let busy = $state(false);

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		message = '';
		try {
			await api.post(`/api/password-reset/${page.params.token}`, { password });
			done = true;
		} catch (e) {
			linkGone = e instanceof ApiError && e.code === 'link_invalid';
			message = errorText(e);
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>{t('reset.use.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('reset.use.title')} />

{#if done}
	<p class="notice success" role="status">{t('reset.use.done')}</p>
	<Button block href="/sign-in">{t('signIn.button')}</Button>
{:else}
	<form class="stack" onsubmit={submit}>
		{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
		<Field label={t('reset.use.password')} type="password" autocomplete="new-password" bind:value={password} />
		<p class="muted">{t('invite.passwordHelp')}</p>
		<Button type="submit" block disabled={busy}>{t('reset.use.button')}</Button>
	</form>
	{#if linkGone}<Button variant="quiet" block href="/reset">{t('reset.request.title')}</Button>{/if}
{/if}
