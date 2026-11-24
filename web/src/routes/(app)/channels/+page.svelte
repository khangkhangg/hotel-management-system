<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { t } from '#lib/i18n.ts';
	import Button from '#lib/ui/Button.svelte';
	import Checkbox from '#lib/ui/Checkbox.svelte';
	import FormGrid from '#lib/ui/FormGrid.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Select from '#lib/ui/Select.svelte';
	import StatusBadge from '#lib/ui/StatusBadge.svelte';

	type Profile = { id: string; mode: string; name: string; recommended: boolean; fields: string[]; steps: string[]; reaches: string[]; verified: boolean; secretHint: string; bookings: boolean; access: string; connectable: boolean; markets: string[] };
	type Connection = { mode: string; provider: string; externalHotelId: string; baseUrl: string; hasSecret: boolean; enabled: boolean };
	type Map = { ratePlanId: string; externalRoomId: string; externalRateId: string };
	type Plan = { id: string; name: string; roomTypeId: string };
	type Remote = { RoomID: string; RoomName: string; RateID: string; RateName: string };
	type Suggestion = { planId: string; roomId: string; rateId: string };

	let profiles = $state<Profile[]>([]);
	let provider = $state('');
	let hotelId = $state('');
	let baseUrl = $state('');
	let secretSetting = $state('');
	let enabled = $state(false);
	let saved = $state(false);
	let plans = $state<Plan[]>([]);
	let codes = $state<Record<string, { room: string; rate: string }>>({});
	let remote = $state<Remote[]>([]);
	let listed = $state(false);
	let suggested = $state(0);
	let tested = $state<{ ok: boolean; message?: string } | null>(null);
	let message = $state('');
	let ok = $state('');
	let busy = $state(false);

	const profile = $derived(profiles.find((p) => p.id === provider));
	let market = $state('');
	const countries = ['VN', 'SG', 'TH', 'JP', 'TW', 'MX', 'US', 'EU'];
	const here = (p: Profile) => market !== '' && p.markets.includes(market);
	const inMarket = (p: Profile) => market === '' || here(p) || p.markets.includes('ALL');
	const shown = $derived(profiles.filter((p) => p.connectable && inMarket(p)).sort((a, b) => Number(here(b)) - Number(here(a))));
	const partners = $derived(profiles.filter((p) => !p.connectable && inMarket(p)));
	const pairs = $derived(remote.map((r) => ({ id: `${r.RoomID}|${r.RateID}`, label: `${r.RoomName} · ${r.RateName}` })));

	async function load() {
		try {
			profiles = await api.get<Profile[]>('/api/channel/providers');
			provider = profiles[0]?.id ?? '';
			const c = await api.get<{ connection: Connection | null; maps: Map[] }>('/api/channel');
			plans = await api.get<Plan[]>('/api/rate-plans');
			codes = Object.fromEntries(plans.map((p) => [p.id, { room: '', rate: '' }]));
			for (const m of c.maps) codes[m.ratePlanId] = { room: m.externalRoomId, rate: m.externalRateId };
			if (c.connection) {
				({ provider, enabled } = c.connection);
				hotelId = c.connection.externalHotelId;
				baseUrl = c.connection.baseUrl;
				saved = true;
			}
		} catch (e) {
			message = errorText(e);
		}
	}

	$effect(() => {
		load();
	});

	async function save(event?: SubmitEvent) {
		event?.preventDefault();
		message = ok = '';
		tested = null;
		try {
			await api.put('/api/channel', { mode: profile?.mode, provider, externalHotelId: hotelId, baseUrl, secretSetting, enabled });
			secretSetting = '';
			saved = true;
			ok = t('channels.saved');
		} catch (e) {
			message = errorText(e);
		}
	}

	async function test() {
		message = ok = '';
		busy = true;
		try {
			tested = await api.post<{ ok: boolean; message?: string }>('/api/channel/test');
		} catch (e) {
			message = errorText(e);
		}
		busy = false;
	}

	async function findMatches() {
		message = ok = '';
		busy = true;
		try {
			const r = await api.get<{ remote: Remote[]; suggestions: Suggestion[]; listed: boolean }>('/api/channel/remote');
			remote = r.remote;
			listed = r.listed;
			suggested = r.suggestions.length;
			for (const s of r.suggestions) codes[s.planId] = { room: s.roomId, rate: s.rateId };
			ok = r.listed ? t('channels.found', { found: r.remote.length, matched: r.suggestions.length }) : t('channels.cannotList');
		} catch (e) {
			message = errorText(e);
		}
		busy = false;
	}

	function pick(p: Plan, id: string) {
		const [room, rate] = id.split('|');
		codes[p.id] = { room, rate };
	}

	async function saveMatches() {
		message = ok = '';
		busy = true;
		try {
			for (const p of plans) {
				if (codes[p.id].room.trim()) await api.put('/api/channel/map', { ratePlanId: p.id, externalRoomId: codes[p.id].room, externalRateId: codes[p.id].rate });
			}
			ok = t('channels.matchesSaved');
		} catch (e) {
			message = errorText(e);
		}
		busy = false;
	}

	async function syncNow() {
		message = ok = '';
		busy = true;
		try {
			const r = await api.post<{ pushed: number; pulled: number }>('/api/channel/sync');
			ok = t('channels.synced', { pushed: r.pushed, pulled: r.pulled });
		} catch (e) {
			message = errorText(e);
		}
		busy = false;
	}
</script>

<svelte:head><title>{t('channels.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('channels.title')} note={t('channels.note')} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}
{#if ok}<p class="notice success" role="status">{ok}</p>{/if}

<section class="card">
	<h2>{t('channels.step1')}</h2>
		<Select label={t('channels.market')} options={[{ id: '', label: t('channels.market.all') }, ...countries.map((c) => ({ id: c, label: t(`channels.market.${c}`) }))]} value={market} onchange={(id) => (market = id)} />
	<div class="choices" role="radiogroup" aria-label={t('channels.step1')}>
		{#each shown as p (p.id)}
			<label class="choice" class:on={provider === p.id}>
				<input type="radio" name="provider" value={p.id} bind:group={provider} />
				<span>
					<strong>{p.name}</strong>
					{#if p.recommended}<StatusBadge tone="info">{t('channels.recommended')}</StatusBadge>{/if}
					<br />
					<span class="muted">{t(`channels.mode.${p.mode}`)}{p.reaches.length ? ` · ${p.reaches.join(', ')}` : ''}</span>
				</span>
			</label>
		{/each}
		</div>
		{#if partners.length}
			<details class="partners">
			<summary>{t('channels.partnerTitle')}</summary>
			<p class="muted">{t('channels.partnerNote')}</p>
			{#each partners as p (p.id)}
				<details class="partner">
					<summary>{p.name}</summary>
					<ol class="steps">{#each p.steps as s}<li>{s}</li>{/each}</ol>
				</details>
			{/each}
		</details>
		{/if}
	</section>

{#if profile}
	<form class="card" onsubmit={save}>
		<h2>{t('channels.step2', { name: profile.name })}</h2>
		{#if !profile.verified}<p class="notice" role="note">{t('channels.unverified')}</p>{/if}
		{#if !profile.bookings}<p class="notice" role="note">{t('channels.noBookings')}</p>{/if}
		<ol class="steps">
			{#each profile.steps as s (s)}<li>{s}</li>{/each}
		</ol>
		<FormGrid>
		{#if profile.fields.includes('hotelId')}<Field label={t('channels.hotelId')} bind:value={hotelId} />{/if}
		{#if profile.fields.includes('baseUrl')}<Field label={t('channels.baseUrl')} placeholder="https://" bind:value={baseUrl} />{/if}
		{#if profile.fields.includes('secret')}
			<Field label={t('channels.secret')} bind:value={secretSetting} placeholder={saved ? t('channels.secretKept') : ''} />
			<p class="muted wide">{profile.secretHint} {t('channels.secretNote')}</p>
		{/if}
		</FormGrid>
		<div class="row">
			<Button type="submit">{t('common.save')}</Button>
			<Button variant="secondary" disabled={busy || !saved} onclick={test}>{t('channels.test')}</Button>
		</div>
		{#if tested}
			<p role="status" class="notice" class:success={tested.ok} class:danger={!tested.ok}>{tested.ok ? t('channels.testOk') : t('channels.testFailed', { reason: tested.message ?? '' })}</p>
		{/if}
	</form>
{/if}

{#if saved}
	<section class="card">
		<h2>{t('channels.step3')}</h2>
		<p class="muted">{t('channels.mappingNote')}</p>
		<div><Button variant="secondary" disabled={busy} onclick={findMatches}>{t('channels.findMatches')}</Button></div>
		{#each plans as p (p.id)}
			<div class="map">
				<strong>{p.name}</strong>
				{#if listed}
					<Select label={t('channels.theirs')} options={[{ id: '|', label: t('channels.none') }, ...pairs]} value={`${codes[p.id].room}|${codes[p.id].rate}`} onchange={(id) => pick(p, id)} />
				{:else}
					<Field label={t('channels.roomCode')} bind:value={codes[p.id].room} />
					<Field label={t('channels.rateCode')} bind:value={codes[p.id].rate} />
				{/if}
			</div>
		{/each}
		<div><Button disabled={busy} onclick={saveMatches}>{t('channels.saveMatches')}</Button></div>
	</section>

	<section class="card">
		<h2>{t('channels.step4')}</h2>
		<Checkbox label={t('channels.enabled')} bind:checked={enabled} />
		<div class="row">
			<Button disabled={busy} onclick={() => save()}>{t('common.save')}</Button>
			<Button variant="secondary" onclick={syncNow} disabled={busy || !enabled}>{t('channels.syncNow')}</Button>
			<Button variant="quiet" href="/sync-log">{t('channels.openLog')}</Button>
		</div>
	</section>
{/if}

<style>
	h2,
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
	.choices {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(var(--card-min), 1fr));
		gap: var(--gap-related);
	}
	.choice {
		display: flex;
		gap: var(--gap-related);
		padding: var(--space-3);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
	}
	.partner {
		padding: var(--space-3);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
	}
	.partner summary {
		cursor: pointer;
	}
	.choice.on {
		border-color: var(--color-focus);
	}
	.steps {
		margin: 0;
		padding-inline-start: var(--space-6);
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.map {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(var(--card-min), 1fr));
		gap: var(--gap-related);
		align-items: end;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: var(--gap-related);
	}
	.muted {
		color: var(--color-text-muted);
	}
	.partners > summary {
		font: var(--font-heading);
		cursor: pointer;
	}
</style>
