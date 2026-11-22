<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { t } from '#lib/i18n.ts';
	import Button from '#lib/ui/Button.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';

	let email = $state('');
	let sent = $state(false);
	let message = $state('');
	let busy = $state(false);

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		message = '';
		try {
			await api.post('/api/password-reset', { email });
			sent = true; // the same answer whether or not the account exists
		} catch (e) {
			message = errorText(e);
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>{t('reset.request.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('reset.request.title')} note={sent ? undefined : t('reset.request.help')} />

{#if sent}
	<p class="notice success" role="status">{t('reset.request.sent')}</p>
{:else}
	<form class="stack" onsubmit={submit}>
		{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
		<Field label={t('signIn.email')} type="email" autocomplete="username" bind:value={email} />
		<Button type="submit" block disabled={busy}>{t('reset.request.button')}</Button>
	</form>
{/if}
<Button variant="quiet" block href="/sign-in">{t('back.toSignIn')}</Button>
