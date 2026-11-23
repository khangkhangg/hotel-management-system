<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { t } from '#lib/i18n.ts';
	import Button from '#lib/ui/Button.svelte';
	import Checkbox from '#lib/ui/Checkbox.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';

	type Kind = { kind: string; enabled: boolean; holdHours: string; bankDetails: string };

	let kinds = $state<Kind[]>([]);
	let message = $state('');
	let saved = $state('');

	async function load() {
		try {
			kinds = (await api.get<(Omit<Kind, 'holdHours'> & { holdHours: number })[]>('/api/payment-types')).map((k) => ({ ...k, holdHours: String(k.holdHours) }));
		} catch (e) {
			message = errorText(e);
		}
	}
	$effect(() => {
		load();
	});

	async function save(k: Kind) {
		message = '';
		saved = '';
		try {
			await api.put(`/api/payment-types/${k.kind}`, { enabled: k.enabled, holdHours: +k.holdHours || 0, bankDetails: k.bankDetails });
			saved = k.kind;
		} catch (e) {
			message = errorText(e);
			await load();
		}
	}
</script>

<svelte:head><title>{t('site.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('site.title')} note={t('site.note')} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

<ul class="types">
	{#each kinds as k (k.kind)}
		<li class="type">
			<h2>{t(`site.kind.${k.kind}`)}</h2>
			<p class="muted">{t(`site.kind.${k.kind}Note`)}</p>
			<Checkbox label={t('site.offer')} bind:checked={k.enabled} />
			{#if k.kind === 'bank_transfer'}
				<Field label={t('site.holdHours')} inputmode="numeric" bind:value={k.holdHours} />
				<Field label={t('site.bankDetails')} bind:value={k.bankDetails} />
			{/if}
			<div class="row">
				<Button onclick={() => save(k)}>{t('common.save')}</Button>
				{#if saved === k.kind}<span role="status" class="muted">{t('site.saved')}</span>{/if}
			</div>
		</li>
	{/each}
</ul>

<style>
	h2 {
		margin: 0;
		font: var(--font-heading);
	}
	p {
		margin: 0;
	}
	.types {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(var(--card-min), 1fr));
		gap: var(--gap-related);
	}
	.type {
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
		padding: var(--space-4);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
		font: var(--font-body);
	}
	.row {
		display: flex;
		align-items: center;
		gap: var(--gap-related);
	}
</style>
