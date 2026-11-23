<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { api, errorText } from '#lib/api.ts';
	import { t } from '#lib/i18n.ts';
	import { organizationChoices, type OrganizationChoice } from '#lib/signin-steps.ts';
	import Button from '#lib/ui/Button.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';

	let choices = $state<OrganizationChoice[]>([]);
	let message = $state('');

	onMount(async () => {
		choices = organizationChoices.get();
		if (choices.length === 0) {
			// Opened in a new tab, or after Google sign-in: ask the server which organizations this session can enter.
			choices = await api.get<OrganizationChoice[]>('/api/organizations').catch(() => []);
		}
		if (choices.length === 0) goto('/sign-in');
	});

	async function choose(id: string) {
		message = '';
		try {
			await api.post('/api/sign-in/organization', { organizationId: id });
			organizationChoices.set(null);
			await goto('/');
		} catch (e) {
			message = errorText(e);
		}
	}
</script>

<svelte:head><title>{t('organization.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('organization.title')} note={t('organization.help')} />

<div class="stack-tight">
	{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
	{#each choices as choice (choice.id)}
		<Button variant="secondary" block onclick={() => choose(choice.id)}>{choice.name}</Button>
	{/each}
</div>
