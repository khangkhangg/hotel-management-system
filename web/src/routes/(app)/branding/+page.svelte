<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { brandPresets, carriesWhiteText } from '#lib/brand.ts';
	import { readBase64 } from '#lib/file.ts';
	import { t } from '#lib/i18n.ts';
	import Button from '#lib/ui/Button.svelte';
	import Field from '#lib/ui/Field.svelte';
	import Section from '#lib/ui/Section.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Segmented from '#lib/ui/Segmented.svelte';

	type Site = { slug: string; color: string; logo: string };

	let slug = $state('');
	let saved = $state('');
	let color = $state<string>(brandPresets[0].color);
	let logo = $state('');
	let message = $state('');
	let ok = $state('');
	let busy = $state(false);
	// The forms appear once the saved values are in, so typing cannot be overwritten by them.
	let loaded = $state(false);

	const valid = $derived(/^#[0-9A-Fa-f]{6}$/.test(color));
	const readable = $derived(carriesWhiteText(color));

	async function load() {
		try {
			const s = await api.get<Site>('/api/site');
			slug = s.slug;
			saved = s.color;
			if (s.color) color = s.color;
			logo = s.logo;
			loaded = true;
		} catch (e) {
			message = errorText(e);
		}
	}

	$effect(() => {
		load();
	});

	async function run(f: () => Promise<void>, done: string) {
		message = ok = '';
		busy = true;
		try {
			await f();
			ok = done;
		} catch (e) {
			message = errorText(e);
		} finally {
			busy = false;
		}
	}

	const save = (event: SubmitEvent) => {
		event.preventDefault();
		return run(async () => {
			await api.put('/api/site/brand', { color });
			saved = color;
		}, t('branding.saved'));
	};

	const upload = (event: Event) => {
		const input = event.currentTarget as HTMLInputElement;
		const file = input.files?.[0];
		if (!file) return;
		return run(async () => {
			const made = await api.post<{ id: string }>('/api/site/photos', { kind: 'logo', caption: '', data: await readBase64(file) });
			logo = made.id;
			input.value = '';
		}, t('branding.logoSaved'));
	};

	const remove = () =>
		run(async () => {
			await api.delete(`/api/site/photos/${logo}`);
			logo = '';
		}, t('branding.logoRemoved'));
</script>

<svelte:head><title>{t('branding.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('branding.title')} note={t('branding.note')} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
{#if ok}<p class="notice success" role="status">{ok}</p>{/if}

{#if loaded}
<Section title={t('branding.preview')}>
	<div class="preview" data-brand-scope style:--brand={readable ? color : saved || brandPresets[0].color}>
		{#if logo}<img class="logo" src={`/api/public/${slug}/photos/${logo}`} alt={t('branding.logoAlt')} />{/if}
		<div class="row"><Button type="button">{t('branding.sampleButton')}</Button></div>
	</div>
</Section>

<Section title={t('branding.colour')}>
<form class="stack" onsubmit={save}>
	<Segmented options={brandPresets.map((p) => ({ id: p.color, label: p.name }))} bind:value={color} />
	<Field label={t('branding.code')} placeholder="#1F5F4F" bind:value={color} error={!valid ? t('branding.codeHelp') : readable ? undefined : t('branding.tooLight')} />
	<p class="muted">{t('branding.contrast')}</p>
	<div class="row">
		<Button type="submit" disabled={busy || !readable}>{t('common.save')}</Button>
	</div>
</form>
</Section>

<Section title={t('branding.logo')}>
	<p class="muted">{t('branding.logoNote')}</p>
	<label class="file">
		<span>{t('branding.chooseLogo')}</span>
		<input type="file" accept="image/png,image/jpeg,image/webp" onchange={upload} disabled={busy} />
	</label>
	{#if logo}<div class="row"><Button variant="secondary" disabled={busy} onclick={remove}>{t('branding.removeLogo')}</Button></div>{/if}
</Section>

{/if}

<style>
	p {
		margin: 0;
	}
	.preview {
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
		padding: var(--space-4);
		border-radius: var(--radius-md);
		background: var(--color-surface-muted);
	}
	.logo {
		block-size: var(--space-8);
		inline-size: auto;
		align-self: flex-start;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: var(--gap-related);
	}
	.file {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		font: var(--font-label);
	}
	.muted {
		color: var(--color-text-muted);
	}
</style>
