<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { formatDay } from '#lib/booking/dates.ts';
	import { formatMoney } from '#lib/booking/money.ts';
	import type { Guest } from '#lib/booking/types.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Dialog from '#lib/ui/Dialog.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import StatusBadge from '#lib/ui/StatusBadge.svelte';
	import Button from '#lib/ui/Button.svelte';

	type Stay = { id: string; code: string; status: string; from: string; to: string; total: number; currency: string };

	let q = $state('');
	let guests = $state<Guest[]>([]);
	let loaded = $state(false);
	let message = $state('');
	let open = $state(false);
	let profile = $state<{ guest: Guest; stays: Stay[] } | null>(null);
	let timer: ReturnType<typeof setTimeout>;

	async function search() {
		try {
			guests = await api.get<Guest[]>(`/api/guests?q=${encodeURIComponent(q)}`);
			loaded = true;
		} catch (e) {
			message = errorText(e);
		}
	}
	$effect(() => {
		q; // search a moment after the last keystroke
		clearTimeout(timer);
		timer = setTimeout(search, 200);
		return () => clearTimeout(timer);
	});

	async function show(id: string) {
		profile = await api.get(`/api/guests/${id}`);
		open = true;
	}
	const canContact = $derived(session.effectiveRole !== 'revenue');
</script>

<svelte:head><title>{t('guests.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('guests.title')} note={t('guests.note')} />
<div class="search"><Field label={t('guests.search')} type="search" bind:value={q} /></div>
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

{#if loaded && guests.length === 0}
	<p class="muted">{t('guests.empty')}</p>
{:else if guests.length}
	<!-- svelte-ignore a11y_no_noninteractive_tabindex -- a scrolling table must be reachable by keyboard -->
	<div class="table-scroll" role="region" aria-label={t('guests.title')} tabindex="0">
		<table class="data-table">
			<thead>
				<tr>
					<th scope="col">{t('guests.name')}</th>
					{#if canContact}<th scope="col">{t('booking.email')}</th><th scope="col">{t('booking.phone')}</th>{/if}
					<th scope="col"><span class="visually-hidden">{t('staff.col.actions')}</span></th>
				</tr>
			</thead>
			<tbody>
				{#each guests as g (g.id)}
					<tr>
						<th scope="row">{g.name}</th>
						{#if canContact}<td>{g.email ?? ''}</td><td>{g.phone ?? ''}</td>{/if}
						<td class="actions"><Button variant="quiet" onclick={() => show(g.id)}>{t('guests.open')}</Button></td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}

<Dialog title={profile?.guest.name ?? t('guests.title')} wide bind:open>
	{#if profile}
		{#if profile.guest.email || profile.guest.phone}<p class="muted">{profile.guest.email} {profile.guest.phone}</p>{/if}
		{#if profile.guest.notes}<p class="muted">{profile.guest.notes}</p>{/if}
		<h3>{t('guests.stays')}</h3>
		{#if profile.stays.length === 0}
			<p class="muted">{t('guests.noStays')}</p>
		{:else}
			<ul class="stays">
				{#each profile.stays as s (s.id)}
					<li>
						<a href="/calendar?reservation={s.id}">{s.code}</a>
						<span>{formatDay(s.from)} to {formatDay(s.to)}</span>
						<StatusBadge tone={s.status === 'checked_out' ? 'neutral' : 'info'}>{s.status.replace('_', ' ')}</StatusBadge>
						<span class="muted">{formatMoney(s.total, s.currency)}</span>
					</li>
				{/each}
			</ul>
		{/if}
	{/if}
</Dialog>

<style>
	.search {
		max-inline-size: var(--form-width);
	}
	h3,
	p {
		margin: 0;
	}
	h3 {
		font: var(--font-heading);
	}
	.stays {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
	}
	.stays li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--gap-related);
		font: var(--font-body);
	}
</style>
