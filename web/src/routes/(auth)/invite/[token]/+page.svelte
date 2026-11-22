<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { api, ApiError, errorText } from '#lib/api.ts';
	import { t } from '#lib/i18n.ts';
	import { destination, pendingEmail, type NextStep } from '#lib/session.ts';
	import Button from '#lib/ui/Button.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';

	type Preview = { email: string; organizationName: string; propertyName: string; role: string };
	const token = page.params.token;

	let invite = $state<Preview | null>(null);
	let expired = $state(false);
	let name = $state('');
	let password = $state('');
	let message = $state('');
	let busy = $state(false);

	onMount(async () => {
		try {
			invite = await api.get<Preview>(`/api/invites/${token}`);
		} catch (e) {
			if (e instanceof ApiError && e.status === 404) expired = true;
			else message = errorText(e);
		}
	});

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		message = '';
		try {
			const step = await api.post<NextStep>(`/api/invites/${token}/accept`, { name, password });
			pendingEmail.set(invite?.email ?? null);
			await goto(destination(step));
		} catch (e) {
			message = errorText(e);
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>{t('app.name')}</title></svelte:head>

{#if expired}
	<PageHeader title={t('invite.expired.title')} note={t('invite.expired.help')} />
{:else if invite}
	<PageHeader
		title={t('invite.title', { organization: invite.organizationName })}
		note={t('invite.help', { role: invite.role.replace('_', ' '), email: invite.email })}
	/>
	<form class="stack" onsubmit={submit}>
		{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
		<Field label={t('invite.name')} autocomplete="name" bind:value={name} />
		<Field label={t('invite.password')} type="password" autocomplete="new-password" bind:value={password} />
		<p class="muted">{t('invite.passwordHelp')}</p>
		<Button type="submit" block disabled={busy}>{t('invite.button')}</Button>
	</form>
{:else if message}
	<p class="notice danger" role="alert">{message}</p>
{/if}
