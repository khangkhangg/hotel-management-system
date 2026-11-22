<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, errorText } from '#lib/api.ts';
	import { t, has } from '#lib/i18n.ts';
	import { destination, pendingEmail, type NextStep } from '#lib/signin-steps.ts';
	import Button from '#lib/ui/Button.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';

	const fromGoogle = page.url.searchParams.get('error') ?? '';
	let email = $state('');
	let password = $state('');
	let message = $state(has(`error.${fromGoogle}`) ? t(`error.${fromGoogle}`) : '');
	let busy = $state(false);

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		message = '';
		try {
			const step = await api.post<NextStep>('/api/sign-in', { email, password });
			pendingEmail.set(email);
			await goto(destination(step));
		} catch (e) {
			message = errorText(e); // the email stays in the field so it can be fixed
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>{t('signIn.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('signIn.title')} />

<form class="stack" onsubmit={submit}>
	{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
	<Field label={t('signIn.email')} type="email" autocomplete="username" bind:value={email} />
	<Field label={t('signIn.password')} type="password" autocomplete="current-password" bind:value={password} />
	<Button type="submit" block disabled={busy}>{t('signIn.button')}</Button>
</form>

<div class="stack-tight">
	<Button variant="secondary" block href="/api/sign-in/google/start">{t('signIn.google')}</Button>
	<Button variant="quiet" block href="/reset">{t('signIn.forgot')}</Button>
</div>
