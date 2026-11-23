<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { api, errorText } from '#lib/api.ts';
	import type { Alert } from '#lib/booking/types.ts';
	import { t } from '#lib/i18n.ts';
	import Button from '#lib/ui/Button.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import StatusBadge from '#lib/ui/StatusBadge.svelte';

	let { onchange }: { onchange?: () => void } = $props();
	let alerts = $state<Alert[]>([]);
	let lastRun = $state<string | null>(null);
	let loaded = $state(false);
	let message = $state('');
	let confirming = $state<Alert | null>(null);
	let confirmOpen = $state(false);

	const tones = { urgent: 'danger', warning: 'warning', info: 'info' } as const;
	const words = { urgent: 'Urgent', warning: 'Needs action', info: 'For your information' } as const;

	async function load() {
		try {
			alerts = await api.get<Alert[]>('/api/alerts');
			lastRun = (await api.get<{ lastRun: string | null }>('/api/alerts/status')).lastRun;
			loaded = true;
		} catch (e) {
			message = errorText(e);
		}
	}

	onMount(() => {
		load();
		const timer = setInterval(load, 20_000);
		return () => clearInterval(timer);
	});

	async function run(path: string, body?: unknown) {
		message = '';
		try {
			await api.post(path, body);
			confirmOpen = false;
			await load();
			onchange?.(); // the screen around the panel shows the same bookings, so it reloads too
		} catch (e) {
			message = errorText(e);
		}
	}

	function choose(a: Alert) {
		if (a.action?.kind === 'open') goto(String(a.action.params?.path ?? '/'));
		else {
			confirming = a;
			confirmOpen = true;
		}
	}
	const time = (iso: string) => new Intl.DateTimeFormat('en', { timeStyle: 'short' }).format(new Date(iso));
</script>

<section class="panel" aria-label={t('attention.title')}>
	<h2>{t('attention.title')}</h2>
	{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

	{#if loaded && alerts.length === 0}
		<p class="muted">{t('attention.none')}{lastRun ? ` ${t('attention.checked', { time: time(lastRun) })}` : ''}</p>
	{:else}
		<ul>
			{#each alerts as a (a.id)}
				<li class={a.severity}>
					<p class="top"><StatusBadge tone={tones[a.severity]}>{words[a.severity]}</StatusBadge></p>
					<p class="text">{a.reason.text}</p>
					<p class="row">
						{#if a.action}<Button variant={a.action.kind === 'open' ? 'secondary' : 'primary'} onclick={() => choose(a)}>{a.action.label}</Button>{/if}
						<Button variant="quiet" onclick={() => run(`/api/alerts/${a.id}/snooze`, { hours: 3 })}>{t('attention.snooze')}</Button>
						<Button variant="quiet" onclick={() => run(`/api/alerts/${a.id}/dismiss`)}>{t('attention.dismiss')}</Button>
					</p>
				</li>
			{/each}
		</ul>
	{/if}
</section>

<Dialog title={confirming?.action?.label ?? ''} bind:open={confirmOpen}>
	<p class="muted">{t('attention.willDo')}</p>
	<ul class="changes">
		{#each confirming?.action?.changes ?? [] as c (c)}<li>{c}</li>{/each}
	</ul>
	{#snippet actions()}
		<Button variant="quiet" onclick={() => (confirmOpen = false)}>{t('common.cancel')}</Button>
		<Button onclick={() => confirming && run(`/api/alerts/${confirming.id}/apply`)}>{t('attention.apply')}</Button>
	{/snippet}
</Dialog>

<style>
	.panel {
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
	}
	h2 {
		margin: 0;
		font: var(--font-heading);
	}
	p {
		margin: 0;
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
	}
	.panel > ul > li {
		display: flex;
		flex-direction: column;
		gap: var(--gap-tight);
		padding: var(--space-3) var(--space-4);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
	}
	.text {
		font: var(--font-body);
		color: var(--color-text);
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: var(--gap-related);
	}
	.changes li {
		font: var(--font-body);
	}
</style>
