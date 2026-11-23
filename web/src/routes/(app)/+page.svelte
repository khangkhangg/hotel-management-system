<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import AttentionPanel from '#lib/alerts/AttentionPanel.svelte';
	import { addDays } from '#lib/booking/dates.ts';
	import { formatMoney } from '#lib/booking/money.ts';
	import { monthToDate } from '#lib/booking/span.ts';
	import type { CalendarData, Stay } from '#lib/booking/types.ts';
	import { t } from '#lib/i18n.ts';
	import { moreGrouped } from '#lib/nav.ts';
	import { session } from '#lib/session.svelte.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Section from '#lib/ui/Section.svelte';
	import StatTile from '#lib/ui/StatTile.svelte';
	import StatusBadge from '#lib/ui/StatusBadge.svelte';

	const today = session.today;
	let data = $state<CalendarData | null>(null);
	let message = $state('');

	$effect(() => {
		api.get<CalendarData>(`/api/calendar?from=${addDays(today, -1)}&to=${addDays(today, 1)}`)
			.then((d) => (data = d))
			.catch((e) => (message = errorText(e)));
	});

	// Owners and managers answer for the money, so the screen says how this month compares with the last one.
	type Pay = { totals: Record<string, number>; pendingRefunds: unknown[]; payments: { currency: string }[] };
	const span = monthToDate(today);
	const sum = (p: Pay) => Object.values(p.totals).reduce((a, b) => a + b, 0);
	let money = $state<{ now: Pay; before: Pay } | null>(null);
	$effect(() => {
		if (session.effectiveRole !== 'owner' && session.effectiveRole !== 'manager') return;
		Promise.all([api.get<Pay>(`/api/payments?from=${span.from}&to=${span.to}`), api.get<Pay>(`/api/payments?from=${span.prevFrom}&to=${span.prevTo}`)])
			.then(([now, before]) => (money = { now, before }))
			.catch(() => {});
	});
	const currency = $derived(money?.now.payments[0]?.currency ?? money?.before.payments[0]?.currency ?? 'USD');
	const change = $derived.by(() => {
		if (!money) return '';
		const a = sum(money.now), b = sum(money.before);
		if (b === 0) return t('overview.noLastMonth');
		const pct = Math.round(((a - b) * 100) / b);
		return t('overview.vsLastMonth', { amount: formatMoney(b, currency), pct: `${pct > 0 ? '+' : ''}${pct}%` });
	});

	const stays = $derived(data?.stays ?? []);
	const arriving = $derived(stays.filter((s) => s.from === today && (s.status === 'confirmed' || s.status === 'pending')));
	const leaving = $derived(stays.filter((s) => s.to === today && s.status === 'checked_in'));
	const inHouse = $derived(stays.filter((s) => s.status === 'checked_in' && s.from <= today && s.to > today).length);
	const dirty = $derived(data?.rooms.filter((r) => r.status === 'dirty').length ?? 0);
	const rooms = $derived(data?.rooms.length ?? 0);
	const occupancy = $derived(rooms ? Math.round((inHouse * 100) / rooms) : 0);
	// The date reads as a person would say it, in the hotel's own day.
	const heading = $derived(new Intl.DateTimeFormat('en', { weekday: 'long', day: 'numeric', month: 'long', timeZone: 'UTC' }).format(new Date(`${today}T00:00:00Z`)));

	const stateText = (s: Stay) => (s.status === 'pending' ? t('overview.waiting') : s.status === 'checked_in' ? t('overview.checkedIn') : t('overview.confirmed'));
	const tone = (s: Stay) => (s.status === 'pending' ? 'warning' : s.status === 'checked_in' ? 'success' : 'neutral');
</script>

<svelte:head><title>{t('overview.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('overview.title')} note={heading} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

<AttentionPanel />

{#if money}
	<div class="tiles">
		<StatTile label={t('overview.takenMonth')} value={formatMoney(sum(money.now), currency)} note={change} />
		<StatTile label={t('overview.refundsWaiting')} value={String(money.now.pendingRefunds.length)} note={money.now.pendingRefunds.length ? t('overview.decide') : t('overview.none')} />
	</div>
{/if}

{#if data}
	<div class="tiles">
		<StatTile label={t('overview.arriving')} value={String(arriving.length)} note={t('overview.today')} />
		<StatTile label={t('overview.leaving')} value={String(leaving.length)} note={t('overview.today')} />
		<StatTile label={t('overview.inHouse')} value={String(inHouse)} note={t('overview.tonight')} />
		<StatTile label={t('overview.occupancy')} value={`${occupancy}%`} note={t('overview.occupancyNote', { count: inHouse, n: rooms })} />
		<StatTile label={t('overview.dirty')} value={String(dirty)} note={t('overview.ofRooms', { n: rooms })} />
	</div>

	<div class="today">
		{#each [{ key: 'arriving', title: t('overview.arrivingToday'), list: arriving, none: t('overview.noArrivals') }, { key: 'leaving', title: t('overview.leavingToday'), list: leaving, none: t('overview.noLeaving') }] as block (block.key)}
			<Section title={block.title}>
				{#if block.list.length === 0}
					<p class="muted">{block.none}</p>
				{:else}
					<ul class="rows">
						{#each block.list as s (s.id)}
							<li>
								<a href="/calendar">
									<span class="who"><strong>{s.guestName}</strong><span class="muted">{s.roomTypeName} · {s.roomId ? (data.rooms.find((r) => r.id === s.roomId)?.number ?? '') : t('overview.noRoom')}</span></span>
									<StatusBadge tone={tone(s)}>{stateText(s)}</StatusBadge>
								</a>
							</li>
						{/each}
					</ul>
				{/if}
			</Section>
		{/each}
	</div>
{/if}

<nav class="goto" aria-label={t('overview.goTo')}>
	{#each moreGrouped(session.effectiveRole ?? 'manager') as group (group.id)}
		<Section title={group.label}>
			<ul class="links">
				{#each group.items as item (item.id)}
					<li><a href={item.href}><Icon name={item.icon} />{item.label}</a></li>
				{/each}
			</ul>
		</Section>
	{/each}
</nav>

<style>
	.tiles {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(var(--field-min), 1fr));
		gap: var(--gap-related);
	}
	.today,
	.goto {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(var(--card-min), 1fr));
		gap: var(--gap-group);
		align-items: start;
	}
	.rows,
	.links {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.rows a {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--gap-related);
		padding: var(--space-2) var(--space-3);
		border-radius: var(--radius-md);
		color: var(--color-text);
		text-decoration: none;
	}
	.who {
		display: flex;
		flex-direction: column;
		min-inline-size: 0;
	}
	.links a {
		display: flex;
		align-items: center;
		gap: var(--gap-related);
		min-block-size: var(--control-height);
		padding-inline: var(--space-3);
		border-radius: var(--radius-md);
		color: var(--color-text);
		font: var(--font-control);
		text-decoration: none;
	}
	@media (hover: hover) {
		.rows a:hover,
		.links a:hover {
			background: var(--color-button-quiet-bg-hover);
		}
	}
	.rows a:focus-visible,
	.links a:focus-visible {
		outline: var(--border-width-strong) solid var(--color-focus);
		outline-offset: var(--border-width-strong);
	}
	.muted {
		margin: 0;
		color: var(--color-text-muted);
	}
</style>
