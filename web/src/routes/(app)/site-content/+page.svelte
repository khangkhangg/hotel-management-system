<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { readBase64 } from '#lib/file.ts';
	import { locales, translate } from '#lib/guest/locale.ts';
	import { t } from '#lib/i18n.ts';
	import Button from '#lib/ui/Button.svelte';
	import Checkbox from '#lib/ui/Checkbox.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Select from '#lib/ui/Select.svelte';

	type Site = {
		slug: string;
		catalog: string[];
		texts: Record<string, Record<string, string>>;
		amenities: string[];
		photos: { id: string; caption: string }[];
	};

	const fieldNames = ['tagline', 'description', 'policies'] as const;

	let slug = $state('');
	let catalog = $state<string[]>([]);
	let texts = $state<Record<string, Record<string, string>>>({});
	let picked = $state<Record<string, boolean>>({});
	let photos = $state<{ id: string; caption: string }[]>([]);
	let locale = $state('en');
	let draft = $state<Record<string, string>>({ tagline: '', description: '', policies: '' });
	let caption = $state('');
	let message = $state('');
	let ok = $state('');
	let busy = $state(false);
	// The forms appear once the saved values are in, so typing cannot be overwritten by them.
	let loaded = $state(false);

	function show() {
		for (const f of fieldNames) draft[f] = texts[locale]?.[f] ?? '';
	}

	async function load() {
		try {
			const s = await api.get<Site>('/api/site');
			slug = s.slug;
			catalog = s.catalog;
			texts = s.texts;
			picked = Object.fromEntries(s.catalog.map((c) => [c, s.amenities.includes(c)]));
			photos = s.photos;
			show();
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

	const saveTexts = (event: SubmitEvent) => {
		event.preventDefault();
		return run(async () => {
			for (const f of fieldNames) await api.put(`/api/site/texts/${locale}/${f}`, { body: draft[f] });
			texts[locale] = Object.fromEntries(fieldNames.filter((f) => draft[f].trim()).map((f) => [f, draft[f].trim()]));
		}, t('siteContent.textsSaved', { language: locales.find((l) => l.code === locale)?.name ?? locale }));
	};

	const saveAmenities = () => run(() => api.put('/api/site/amenities', { codes: catalog.filter((c) => picked[c]) }), t('siteContent.amenitiesSaved'));

	const addPhoto = (event: Event) => {
		const input = event.currentTarget as HTMLInputElement;
		const file = input.files?.[0];
		if (!file) return;
		return run(async () => {
			const made = await api.post<{ id: string }>('/api/site/photos', { kind: 'gallery', caption, data: await readBase64(file) });
			photos = [...photos, { id: made.id, caption: caption.trim() }];
			caption = '';
			input.value = '';
		}, t('siteContent.photoAdded'));
	};

	const removePhoto = (id: string) =>
		run(async () => {
			await api.delete(`/api/site/photos/${id}`);
			photos = photos.filter((p) => p.id !== id);
		}, t('siteContent.photoRemoved'));
</script>

<svelte:head><title>{t('siteContent.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('siteContent.title')} note={t('siteContent.note')} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
{#if ok}<p class="notice success" role="status">{ok}</p>{/if}

{#if loaded}
<form class="card" onsubmit={saveTexts}>
	<h2>{t('siteContent.texts')}</h2>
	<p class="muted">{t('siteContent.textsNote')}</p>
	<Select label={t('siteContent.language')} options={locales.map((l) => ({ id: l.code, label: l.name }))} value={locale} onchange={(id) => ((locale = id), show())} />
	<Field label={t('siteContent.tagline')} bind:value={draft.tagline} />
	<label class="area">
		<span>{t('siteContent.description')}</span>
		<textarea rows="6" bind:value={draft.description} dir={locales.find((l) => l.code === locale)?.dir}></textarea>
	</label>
	<label class="area">
		<span>{t('siteContent.policies')}</span>
		<textarea rows="4" bind:value={draft.policies} dir={locales.find((l) => l.code === locale)?.dir}></textarea>
	</label>
	<div class="row"><Button type="submit" disabled={busy}>{t('common.save')}</Button></div>
</form>

<section class="card">
	<h2>{t('siteContent.amenities')}</h2>
	<div class="checks" role="group" aria-label={t('siteContent.amenities')}>
		{#each catalog as code (code)}
			<Checkbox label={translate('en', `amenity.${code}`)} bind:checked={picked[code]} />
		{/each}
	</div>
	<div class="row"><Button disabled={busy} onclick={saveAmenities}>{t('common.save')}</Button></div>
</section>

<section class="card">
	<h2>{t('siteContent.photos')}</h2>
	<p class="muted">{t('siteContent.photosNote')}</p>
	{#if photos.length}
		<ul class="gallery">
			{#each photos as p (p.id)}
				<li>
					<img src={`/api/public/${slug}/photos/${p.id}`} alt={p.caption} loading="lazy" />
					{#if p.caption}<span class="muted">{p.caption}</span>{/if}
					<Button variant="secondary" disabled={busy} onclick={() => removePhoto(p.id)}>{t('siteContent.removePhoto')}</Button>
				</li>
			{/each}
		</ul>
	{/if}
	<Field label={t('siteContent.caption')} bind:value={caption} />
	<label class="file">
		<span>{t('siteContent.choosePhoto')}</span>
		<input type="file" accept="image/png,image/jpeg,image/webp" onchange={addPhoto} disabled={busy} />
	</label>
</section>

{/if}

<style>
	h2 {
		margin: 0;
		font: var(--font-heading);
	}
	p {
		margin: 0;
	}
	.card {
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
		max-inline-size: var(--page-width);
		padding: var(--space-4);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
		font: var(--font-body);
	}
	.area,
	.file {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		font: var(--font-label);
	}
	textarea {
		padding: var(--space-2) var(--space-3);
		border: var(--border-width) solid var(--color-border-input);
		border-radius: var(--radius-md);
		background: var(--color-surface);
		color: var(--color-text);
		font: var(--font-body);
	}
	.checks {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(var(--card-min), 1fr));
		gap: var(--space-2);
	}
	.gallery {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(var(--card-min), 1fr));
		gap: var(--gap-related);
	}
	.gallery li {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.gallery img {
		display: block;
		inline-size: 100%;
		aspect-ratio: 3 / 2;
		object-fit: cover;
		border-radius: var(--radius-md);
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: var(--gap-related);
	}
	.muted {
		color: var(--color-text-muted);
	}
</style>
