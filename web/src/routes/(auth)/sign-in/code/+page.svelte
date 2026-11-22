<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { api, ApiError, errorText } from '#lib/api.ts';
	import { t } from '#lib/i18n.ts';
	import { destination, pendingEmail, type NextStep } from '#lib/session.ts';
	import Button from '#lib/ui/Button.svelte';
	import Checkbox from '#lib/ui/Checkbox.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';

	let email = $state('your email');
	let code = $state('');
	let trust = $state(false);
	let message = $state('');
	let info = $state('');
	let busy = $state(false);

	onMount(() => {
		email = pendingEmail.get() ?? email;
	});

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		message = info = '';
		try {
			const step = await api.post<NextStep>('/api/sign-in/code', { code, trustDevice: trust });
			pendingEmail.set(null);
			await goto(destination(step));
		} catch (e) {
			if (e instanceof ApiError && e.status === 401) await goto('/sign-in'); // the sign-in step expired
			else message = errorText(e);
		} finally {
			busy = false;
		}
	}

	async function resend() {
		message = info = '';
		try {
			await api.post('/api/sign-in/code/resend');
			info = t('code.resent');
			code = '';
		} catch (e) {
			if (e instanceof ApiError && e.status === 401) await goto('/sign-in');
			else message = errorText(e);
		}
	}
</script>

<svelte:head><title>{t('code.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('code.title')} note={t('code.help', { email })} />

<form class="stack" onsubmit={submit}>
	{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
	{#if info}<p class="notice success" role="status">{info}</p>{/if}
	<Field label={t('code.label')} inputmode="numeric" autocomplete="one-time-code" bind:value={code} />
	<Checkbox label={t('code.trust')} bind:checked={trust} />
	<Button type="submit" block disabled={busy}>{t('code.continue')}</Button>
</form>

<Button variant="quiet" block onclick={resend}>{t('code.resend')}</Button>
