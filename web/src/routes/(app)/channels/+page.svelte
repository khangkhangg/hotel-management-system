<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { t } from '#lib/i18n.ts';
	import Button from '#lib/ui/Button.svelte';
	import Checkbox from '#lib/ui/Checkbox.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Segmented from '#lib/ui/Segmented.svelte';
	import Select from '#lib/ui/Select.svelte';

	type Connection = { mode: string; provider: string; externalHotelId: string; baseUrl: string; hasSecret: boolean; enabled: boolean };
	type Map = { ratePlanId: string; externalRoomId: string; externalRateId: string };
	type Plan = { id: string; name: string; roomTypeId: string };

	const providers: Record<string, string[]> = { channel_manager: ['cm_generic'], direct: ['agoda', 'booking_com', 'expedia'] };

	let mode = $state('channel_manager');
	let provider = $state('cm_generic');
	let hotelId = $state('');
	let baseUrl = $state('');
	let secretSetting = $state('');
	let enabled = $state(false);
	let saved = $state(false);
	let plans = $state<Plan[]>([]);
	let codes = $state<Record<string, { room: string; rate: string }>>({});
	let message = $state('');
	let ok = $state('');
	let busy = $state(false);

	async function load() {
		try {
			const c = await api.get<{ connection: Connection | null; maps: Map[] }>('/api/channel');
			if (c.connection) {
				({ mode, provider, enabled } = c.connection);
				hotelId = c.connection.externalHotelId;
				baseUrl = c.connection.baseUrl;
				saved = true;
			}
			plans = await api.get<Plan[]>('/api/rate-plans');
			codes = Object.fromEntries(plans.map((p) => [p.id, { room: '', rate: '' }]));
			for (const m of c.maps) codes[m.ratePlanId] = { room: m.externalRoomId, rate: m.externalRateId };
		} catch (e) {
			message = errorText(e);
		}
	}

	$effect(() => {
		load();
	});

	function chooseMode(id: string) {
		mode = id;
		provider = providers[id][0];
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		message = ok = '';
		try {
			await api.put('/api/channel', { mode, provider, externalHotelId: hotelId, baseUrl, secretSetting, enabled });
			secretSetting = '';
			saved = true;
			ok = t('channels.saved');
		} catch (e) {
			message = errorText(e);
		}
	}

	async function saveMap(p: Plan) {
		message = ok = '';
		try {
			await api.put('/api/channel/map', { ratePlanId: p.id, externalRoomId: codes[p.id].room, externalRateId: codes[p.id].rate });
			ok = t('channels.mapSaved', { name: p.name });
		} catch (e) {
			message = errorText(e);
		}
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

<form class="card" onsubmit={save}>
	<h2>{t('channels.how')}</h2>
	<Segmented
		options={[
			{ id: 'channel_manager', label: t('channels.mode.channel_manager') },
			{ id: 'direct', label: t('channels.mode.direct') }
		]}
		value={mode}
		onchange={chooseMode}
	/>
	<p class="muted">{t(`channels.modeNote.${mode}`)}</p>
	<Select label={t('channels.provider')} options={providers[mode].map((p) => ({ id: p, label: t(`channels.provider.${p}`) }))} bind:value={provider} />
	<Field label={t('channels.hotelId')} bind:value={hotelId} />
	<Field label={t('channels.baseUrl')} type="text" placeholder="https://" bind:value={baseUrl} />
	<Field label={t('channels.secret')} bind:value={secretSetting} placeholder={saved ? t('channels.secretKept') : ''} />
	<p class="muted">{t('channels.secretNote')}</p>
	<Checkbox label={t('channels.enabled')} bind:checked={enabled} />
	<div><Button type="submit">{t('common.save')}</Button></div>
</form>

{#if saved}
	<section class="card">
		<h2>{t('channels.mapping')}</h2>
		<p class="muted">{t('channels.mappingNote')}</p>
		{#each plans as p (p.id)}
			<div class="map">
				<strong>{p.name}</strong>
				<Field label={t('channels.roomCode')} bind:value={codes[p.id].room} />
				<Field label={t('channels.rateCode')} bind:value={codes[p.id].rate} />
				<div><Button variant="secondary" disabled={!codes[p.id].room.trim()} onclick={() => saveMap(p)}>{t('common.save')}</Button></div>
			</div>
		{/each}
	</section>
	<div class="row">
		<Button onclick={syncNow} disabled={busy || !enabled}>{t('channels.syncNow')}</Button>
		<Button variant="quiet" href="/sync-log">{t('channels.openLog')}</Button>
	</div>
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
</style>
