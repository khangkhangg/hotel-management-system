<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { formatMoney } from '#lib/booking/money.ts';
	import { t } from '#lib/i18n.ts';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import StatTile from '#lib/ui/StatTile.svelte';

	type Row = { id?: string; name?: string; currency: string; rooms: number; occupied: number; arriving: number; leaving: number; paid30: number };
	type Group = { properties: Row[]; totals: Row[] };

	let data = $state<Group | null>(null);
	let message = $state('');

	$effect(() => {
		api
			.get<Group>('/api/group')
			.then((d) => (data = d))
			.catch((e) => (message = errorText(e)));
	});
</script>

<svelte:head><title>{t('group.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('group.title')} note={t('group.note')} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

{#if data}
	{#each data.totals as s (s.currency)}
		<section>
			<h2>{t('group.sum', { currency: s.currency })}</h2>
			<div class="tiles">
				<StatTile label={t('group.rooms')} value={String(s.rooms)} note={t('group.rooms.note')} />
				<StatTile label={t('group.occupied')} value={String(s.occupied)} note={t('group.tonight')} />
				<StatTile label={t('group.arriving')} value={String(s.arriving)} note={t('group.today')} />
				<StatTile label={t('group.leaving')} value={String(s.leaving)} note={t('group.today')} />
				<StatTile label={t('group.paid')} value={formatMoney(s.paid30, s.currency)} note={t('group.last30')} />
			</div>
		</section>
	{/each}

	<section>
		<h2>{t('group.compare')}</h2>
		<!-- svelte-ignore a11y_no_noninteractive_tabindex -- a wide table scrolls inside its box, and keyboard users must be able to reach it -->
		<div class="scroll" role="region" tabindex="0" aria-label={t('group.compare')}>
			<table>
				<thead>
					<tr>
						<th scope="col">{t('group.property')}</th>
						<th scope="col" class="num">{t('group.rooms')}</th>
						<th scope="col" class="num">{t('group.occupied')}</th>
						<th scope="col" class="num">{t('group.arriving')}</th>
						<th scope="col" class="num">{t('group.leaving')}</th>
						<th scope="col" class="num">{t('group.paid')}</th>
					</tr>
				</thead>
				<tbody>
					{#each data.properties as p (p.id)}
						<tr>
							<th scope="row">{p.name}</th>
							<td class="num">{p.rooms}</td>
							<td class="num">{p.occupied}</td>
							<td class="num">{p.arriving}</td>
							<td class="num">{p.leaving}</td>
							<td class="num">{formatMoney(p.paid30, p.currency)}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	</section>
{/if}

<style>
	h2 {
		margin: 0;
		font: var(--font-heading);
	}
	section {
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
	}
	.tiles {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(var(--field-min), 1fr));
		gap: var(--gap-related);
	}
	.scroll {
		overflow-x: auto;
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
	}
	table {
		inline-size: 100%;
		border-collapse: collapse;
		font: var(--font-body);
	}
	th,
	td {
		padding: var(--space-3) var(--space-4);
		text-align: start;
		white-space: nowrap;
	}
	thead th {
		color: var(--color-text-muted);
		font-weight: 500;
	}
	tbody tr + tr {
		border-block-start: var(--border-width) solid var(--color-border);
	}
	.num {
		text-align: end;
		font-variant-numeric: tabular-nums;
	}
</style>
