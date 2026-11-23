<script lang="ts">
	import { page } from '$app/state';
	import { api, ApiError } from '#lib/api.ts';
	import { formatMoney } from '#lib/booking/money.ts';
	import { direction, offered, pickLocale, translate } from '#lib/guest/locale.ts';
	import { canonicalUrl, hotelJsonLd, metaDescription, serializeJsonLd } from '#lib/guest/seo.ts';
	import Button from '#lib/ui/Button.svelte';
	import Field from '#lib/ui/Field.svelte';

	type Info = {
		name: string;
		currency: string;
		defaultLocale: string;
		address: string;
		payments: string[];
		roomTypes: { id: string; name: string; baseGuests: number; maxGuests: number }[];
		ratePlans: { id: string; roomTypeId: string; name: string }[];
		extras: { id: string; name: string; unit: string; price: number }[];
		site: { color: string; logo: string; texts: Record<string, Record<string, string>>; amenities: string[]; photos: { id: string; caption: string }[] };
		reviews: { count: number; average: number; recent: { rating: number; comment: string; name: string; reply: string; createdAt: string }[] };
	};
	type Quote = { total: number; currency: string; soldOut: boolean; lines: { description: string; amount: number }[] };
	type Made = { code: string; total?: number; currency: string; status: string; checkoutUrl?: string; bankDetails?: string; holdUntil?: string };

	const slug = page.params.slug ?? '';
	const saved = (() => {
		try {
			return localStorage.getItem('guest-locale') ?? undefined;
		} catch {
			return undefined;
		}
	})();

	let { data }: { data: { info: Info } } = $props();
	const info = $derived<Info | null>(data.info);
	let missing = $state(false);
	let locale = $state('en');
	let step = $state<'room' | 'details' | 'done'>('room');
	let from = $state('');
	let to = $state('');
	let adults = $state('2');
	let children = $state('0');
	let quotes = $state<Record<string, Quote>>({});
	let chosen = $state<{ typeId: string; planId: string } | null>(null);
	let name = $state('');
	let email = $state('');
	let phone = $state('');
	let kind = $state('');
	let promo = $state('');
	let searched = $state(false);
	let wlName = $state('');
	let wlEmail = $state('');
	let wlDone = $state(false);
	let error = $state('');
	let busy = $state(false);
	let picked = $state<string[]>([]);
	let requoted = $state<Quote | null>(null);
	let made = $state<Made | null>(null);

	const t = (key: string, params: Record<string, string | number> = {}) => translate(locale, key, params);
	const nights = $derived(from && to ? Math.round((Date.parse(to) - Date.parse(from)) / 86400000) : 0);
	const dir = $derived(direction(locale));
	const money = (n: number) => formatMoney(n, info?.currency ?? 'USD');
	const current = $derived(requoted ?? (chosen ? quotes[chosen.planId] : undefined));
	const typeName = $derived(info?.roomTypes.find((x) => x.id === chosen?.typeId)?.name ?? '');
	const steps = $derived(['room', 'details', 'done'] as const);

	// The hotel arrives with the page, so search engines read it. The guest's own language is picked here, in the browser.
	$effect(() => {
		if (!info) return;
		locale = pickLocale({ url: page.url.searchParams.get('lang') ?? undefined, saved, browser: navigator.languages, fallback: info.defaultLocale });
		kind = info.payments[0] ?? '';
	});
	// Back from the payment page: the webhook may take a few seconds to confirm, so ask a few times.
	$effect(() => {
		const code = page.url.searchParams.get('done');
		if (!code) return;
		let tries = 0;
		let timer: ReturnType<typeof setTimeout>;
		const look = async () => {
			try {
				made = await api.get<Made>(`/api/public/${slug}/bookings/${code}`);
				step = 'done';
				if (made.status === 'pending' && ++tries < 10) timer = setTimeout(look, 2000);
			} catch {
				missing = true;
			}
		};
		look();
		return () => clearTimeout(timer);
	});
	// The hotel's text comes in the guest's language when the hotel wrote it, then in the hotel's own language, then in English.
	const siteText = (field: string): string => {
		const x = info?.site.texts;
		if (!x) return '';
		return x[locale]?.[field] || x[info!.defaultLocale]?.[field] || x.en?.[field] || Object.values(x).map((l) => l[field]).find(Boolean) || '';
	};
	const origin = $derived(page.url.origin);
	const description = $derived(info ? metaDescription(info, siteText('tagline'), siteText('description')) : '');
	const canonical = $derived(canonicalUrl(origin, slug));
	const ogImage = $derived(info?.site.photos[0] ? `${origin}/api/public/${slug}/photos/${info.site.photos[0].id}` : info?.site.logo ? `${origin}/api/public/${slug}/photos/${info.site.logo}` : '');
	const ldText = $derived(info ? serializeJsonLd(hotelJsonLd(info, { origin, slug, description })) : '');
	const hasAbout = $derived(!!info && (!!siteText('tagline') || !!siteText('description') || !!siteText('policies') || info.site.amenities.length > 0 || info.site.photos.length > 0));

	// The hotel's colour replaces --brand for the whole page, and only on this page.
	$effect(() => {
		const color = info?.site.color;
		if (!color) return;
		const root = document.documentElement;
		root.setAttribute('data-brand-scope', '');
		root.style.setProperty('--brand', color);
		return () => {
			root.removeAttribute('data-brand-scope');
			root.style.removeProperty('--brand');
		};
	});

	$effect(() => {
		document.documentElement.lang = locale;
		document.documentElement.dir = dir;
	});

	const body = (planId: string, typeId: string) => ({ promo: promo.trim(), extras: picked.map((id) => ({ id, quantity: 1 })), rooms: [{ roomTypeId: typeId, ratePlanId: planId, from, to, adults: +adults || 1, children: +children || 0 }] });

	// Extras change the total, so the quote is asked again with them.
	async function toggleExtra(id: string) {
		picked = picked.includes(id) ? picked.filter((x) => x !== id) : [...picked, id];
		if (!chosen) return;
		try {
			requoted = await api.post<Quote>(`/api/public/${slug}/quote`, { ...body(chosen.planId, chosen.typeId), paymentKind: 'hotel' });
		} catch {
			error = t('guest.error.generic');
		}
	}

	async function search(event: SubmitEvent) {
		event.preventDefault();
		error = '';
		searched = false;
		wlDone = false;
		quotes = {};
		picked = [];
		requoted = null;
		chosen = null;
		if (!info || nights < 1) return;
		busy = true;
		try {
			const entries = await Promise.all(
				// A rate plan with no prices yet is left out, so it cannot hide the plans that can be booked.
				info.ratePlans.map(async (p) => {
					try {
						return [p.id, await api.post<Quote>(`/api/public/${slug}/quote`, { ...body(p.id, p.roomTypeId), paymentKind: 'hotel' })] as const;
					} catch (e) {
						if (e instanceof ApiError && (e.code === 'slow_down' || e.code === 'promo_invalid')) throw e;
						return null;
					}
				})
			);
			quotes = Object.fromEntries(entries.filter((x) => x !== null));
			searched = true;
		} catch (e) {
			error = e instanceof ApiError ? t(`guest.error.${e.code}`) : t('guest.error.generic');
		}
		busy = false;
	}

	const allSoldOut = $derived(searched && !Object.values(quotes).some((q) => !q.soldOut));

	async function joinWaitlist(event: SubmitEvent) {
		event.preventDefault();
		error = '';
		busy = true;
		try {
			await api.post(`/api/public/${slug}/waitlist`, { name: wlName, email: wlEmail, arrive: from, depart: to, adults: +adults || 1, locale });
			wlDone = true;
		} catch (e) {
			error = e instanceof ApiError ? t(`guest.error.${e.code}`) : t('guest.error.generic');
		}
		busy = false;
	}

	// One key per page, so a double click or a retry finds the booking already made.
	const bookingKey = crypto.randomUUID();

	async function book(event: SubmitEvent) {
		event.preventDefault();
		if (!chosen) return;
		error = '';
		busy = true;
		try {
			made = await api.post<Made>(`/api/public/${slug}/bookings`, {
				guest: { name, email, phone, locale },
				paymentKind: kind,
				key: bookingKey,
				...body(chosen.planId, chosen.typeId)
			});
			if (made.checkoutUrl) {
				window.location.href = made.checkoutUrl;
				return;
			}
			step = 'done';
		} catch (e) {
			const code = e instanceof ApiError ? e.code : 'generic';
			error = t(`guest.error.${code}`) === `guest.error.${code}` ? t('guest.error.generic') : t(`guest.error.${code}`);
			if (code === 'sold_out') step = 'room';
		}
		busy = false;
	}

	function pick(l: string) {
		locale = l;
		try {
			localStorage.setItem('guest-locale', l);
		} catch {
			/* the choice lasts until the page closes */
		}
	}
</script>

<svelte:head>
	<title>{info?.name ?? 'Book'}</title>
	{#if info}
		<meta name="description" content={description} />
		<link rel="canonical" href={canonical} />
		<meta name="robots" content="index,follow,max-image-preview:large" />
		<meta property="og:type" content="website" />
		<meta property="og:title" content={info.name} />
		<meta property="og:description" content={description} />
		<meta property="og:url" content={canonical} />
		{#if ogImage}<meta property="og:image" content={ogImage} />{/if}
		<meta name="twitter:card" content={ogImage ? 'summary_large_image' : 'summary'} />
		{@html '<script type="application/ld+json">' + ldText + '</' + 'script>'}
	{/if}
</svelte:head>

{#if missing}
	<main class="page"><p>{translate('en', 'guest.empty')}</p></main>
{:else if info}
	<header class="top">
		<h1>{#if info.site.logo}<img class="logo" src={`/api/public/${slug}/photos/${info.site.logo}`} alt="" />{/if}{info.name}</h1>
		<Button variant="quiet" href={`/book/${slug}/manage`}>{t('guest.manage')}</Button>
		{#if offered().length > 1}
			<label class="lang">
				<span class="visually-hidden">{t('guest.language')}</span>
				<select value={locale} onchange={(e) => pick(e.currentTarget.value)}>
					{#each offered() as l (l.code)}<option value={l.code} lang={l.code}>{l.name}</option>{/each}
				</select>
			</label>
		{/if}
	</header>

	<nav class="progress" aria-label={t('guest.stepOf', { n: steps.indexOf(step) + 1, total: steps.length })}>
		{#each steps as s, i (s)}
			<span class:active={s === step} class:past={i < steps.indexOf(step)} aria-current={s === step ? 'step' : undefined}>{i + 1}. {t(`guest.step.${s}`)}</span>
		{/each}
	</nav>

	<main class="page">
		{#if step === 'room'}
			{#if hasAbout}
				<section class="about stack" aria-label={t('guest.about')}>
					{#if siteText('tagline')}<p class="lead">{siteText('tagline')}</p>{/if}
					{#if siteText('description')}
						<h2>{t('guest.about')}</h2>
						<p class="text">{siteText('description')}</p>
					{/if}
					{#if info.site.photos.length}
						<ul class="gallery" aria-label={t('guest.photos')}>
							{#each info.site.photos as ph (ph.id)}
								<li><figure><img src={`/api/public/${slug}/photos/${ph.id}`} alt="" loading="lazy" />{#if ph.caption}<figcaption class="muted">{ph.caption}</figcaption>{/if}</figure></li>
							{/each}
						</ul>
					{/if}
					{#if info.site.amenities.length}
						<h3>{t('guest.amenities')}</h3>
						<ul class="amenities">
							{#each info.site.amenities as a (a)}<li>{t(`amenity.${a}`)}</li>{/each}
						</ul>
					{/if}
					{#if siteText('policies')}
						<h3>{t('guest.policies')}</h3>
						<p class="text">{siteText('policies')}</p>
					{/if}
				</section>
			{/if}
			<form class="stack search" onsubmit={search}>
				<Field label={t('guest.arrival')} type="date" bind:value={from} />
				<Field label={t('guest.departure')} type="date" bind:value={to} />
				<Field label={t('guest.adults')} inputmode="numeric" bind:value={adults} />
				<Field label={t('guest.children')} inputmode="numeric" bind:value={children} />
				<Field label={t('guest.promo')} bind:value={promo} />
				<Button type="submit" disabled={busy || nights < 1}>{t('guest.search')}</Button>
			</form>
			{#if error}<p class="error" role="alert">{error}</p>{/if}
			{#if info.roomTypes.length === 0}<p class="muted">{t('guest.empty')}</p>{/if}
			{#if allSoldOut}
				<section class="about stack" aria-label={t('guest.waitlist.title')}>
					<h2>{t('guest.waitlist.title')}</h2>
					{#if wlDone}
						<p role="status">{t('guest.waitlist.done')}</p>
					{:else}
						<form class="stack" onsubmit={joinWaitlist}>
							<Field label={t('guest.name')} bind:value={wlName} />
							<Field label={t('guest.email')} type="email" bind:value={wlEmail} />
							<Button type="submit" disabled={busy}>{t('guest.waitlist.join')}</Button>
						</form>
					{/if}
				</section>
			{/if}
			<ul class="rooms">
				{#each info.ratePlans as p (p.id)}
					{@const rt = info.roomTypes.find((x) => x.id === p.roomTypeId)}
					{@const q = quotes[p.id]}
					{#if rt && q}
						<li class="card">
							<div>
								<h2>{rt.name} <span class="muted">{p.name}</span></h2>
								<p class="muted">{t('guest.upTo', { count: rt.maxGuests })} · {t('guest.nights', { count: nights })}</p>
							</div>
							<div class="buy">
								<p><strong>{money(q.total)}</strong> <span class="muted">{t('guest.withTaxes')}</span></p>
								{#if q.soldOut}
									<p class="muted">{t('guest.soldOut')}</p>
								{:else}
									<Button
										onclick={() => {
											picked = [];
											requoted = null;
											chosen = { typeId: rt.id, planId: p.id };
											step = 'details';
										}}>{t('guest.select')}</Button
									>
								{/if}
							</div>
						</li>
					{/if}
				{/each}
			</ul>
		{:else if step === 'details' && current}
			<div class="split">
				<form id="guest-form" class="stack" onsubmit={book}>
					<Field label={t('guest.name')} autocomplete="name" bind:value={name} />
					<Field label={t('guest.email')} type="email" autocomplete="email" bind:value={email} />
					<Field label={t('guest.phone')} type="tel" autocomplete="tel" bind:value={phone} />
					{#if info?.extras.length}
						<fieldset class="pay">
							<legend>{t('guest.extras')}</legend>
							{#each info.extras as x (x.id)}
								<label class="choice">
									<input type="checkbox" checked={picked.includes(x.id)} onchange={() => toggleExtra(x.id)} />
									<span><strong>{x.name}</strong><br /><span class="muted">{money(x.price)} {t(`guest.extra.${x.unit}`)}</span></span>
								</label>
							{/each}
						</fieldset>
					{/if}
					<fieldset class="pay">
						<legend>{t('guest.payHow')}</legend>
						{#each info.payments as k (k)}
							<label class="choice">
								<input type="radio" name="kind" value={k} bind:group={kind} />
								<span><strong>{t(`guest.pay.${k}`)}</strong><br /><span class="muted">{t(`guest.pay.${k}Note`)}</span></span>
							</label>
						{/each}
					</fieldset>
					{#if error}<p class="error" role="alert">{error}</p>{/if}
					<Button type="submit" disabled={busy || !name.trim() || !email.trim() || !kind}>{kind === 'card' ? t('guest.payNow') : t('guest.confirm')}</Button>
				</form>
				<aside class="card summary" aria-label={t('guest.yourStay')}>
					<h2>{t('guest.yourStay')}</h2>
					<p>{typeName} · {from} → {to} · {t('guest.nights', { count: nights })}</p>
					<ul class="plain">
						{#each current.lines as l (l.description)}<li><span>{l.description}</span><span>{money(l.amount)}</span></li>{/each}
					</ul>
					<p class="total"><span>{t('guest.total')}</span><strong>{money(current.total)}</strong></p>
				</aside>
			</div>
		{:else if step === 'done' && made}
			<section class="card stack">
				<h2>{t('guest.done.title', { state: t(`guest.done.${made.status}`) })}</h2>
				<p>{t('guest.done.code')}: <strong>{made.code}</strong></p>
				{#if made.total !== undefined}<p><strong>{money(made.total)}</strong></p>{/if}
				{#if made.holdUntil}<p class="muted">{t('guest.done.holdUntil', { time: new Date(made.holdUntil).toLocaleString(locale) })}</p>{/if}
				{#if made.bankDetails}<p>{t('guest.done.bank')}</p><pre>{made.bankDetails}</pre>{/if}
			</section>
		{/if}
	</main>
{/if}

<style>
	.logo {
		block-size: var(--space-8);
		inline-size: auto;
		vertical-align: middle;
		margin-inline-end: var(--space-2);
	}
	.about {
		margin-block-end: var(--space-6);
	}
	.about h2,
	.about h3 {
		margin: 0;
		font: var(--font-heading);
	}
	.lead {
		margin: 0;
		font: var(--font-heading);
		color: var(--color-text-muted);
	}
	.text {
		margin: 0;
		white-space: pre-line;
	}
	.gallery {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(var(--card-min), 1fr));
		gap: var(--gap-related);
	}
	.gallery figure {
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.gallery img {
		display: block;
		inline-size: 100%;
		aspect-ratio: 3 / 2;
		object-fit: cover;
		border-radius: var(--radius-md);
	}
	.amenities {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.amenities li {
		padding: var(--space-1) var(--space-3);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-full);
	}
	.top {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: var(--gap-related);
		padding: var(--space-4);
		border-block-end: var(--border-width) solid var(--color-border);
		background: var(--color-surface);
	}
	h1,
	h2 {
		margin: 0;
		font: var(--font-heading);
	}
	.progress {
		display: flex;
		gap: var(--space-4);
		padding: var(--space-3) var(--space-4);
		font: var(--font-body);
		color: var(--color-text-muted);
	}
	.progress .active {
		color: var(--color-text);
		font-weight: var(--font-weight-semibold);
	}
	.page {
		max-inline-size: var(--page-width);
		margin-inline: auto;
		padding: var(--space-4);
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
		font: var(--font-body);
	}
	.search {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(var(--field-min), 1fr));
		align-items: end;
		gap: var(--gap-related);
	}
	.rooms,
	.plain {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
	}
	.card {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		gap: var(--space-4);
		padding: var(--space-4);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
	}
	.buy {
		display: flex;
		flex-direction: column;
		align-items: flex-end;
		gap: var(--gap-related);
	}
	.split {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(var(--card-min), 1fr));
		gap: var(--space-6);
		align-items: start;
	}
	.stack {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}
	.summary {
		flex-direction: column;
	}
	.plain li,
	.total {
		display: flex;
		justify-content: space-between;
		gap: var(--space-4);
		margin: 0;
	}
	.pay {
		border: 0;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
	}
	.choice {
		display: flex;
		gap: var(--gap-related);
		align-items: flex-start;
		padding: var(--space-3);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
	}
	.muted {
		color: var(--color-text-muted);
	}
	.error {
		margin: 0;
		color: var(--palette-danger-fg);
	}
	pre {
		margin: 0;
		white-space: pre-wrap;
		font: var(--font-body);
	}
	select {
		min-block-size: var(--control-height);
		font: var(--font-body);
	}
</style>
