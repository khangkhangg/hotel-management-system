<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import { formatDay } from '#lib/booking/dates.ts';
	import { formatMoney } from '#lib/booking/money.ts';
	import type { Guest } from '#lib/booking/types.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import DataTable from '#lib/ui/DataTable.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import SearchBox from '#lib/ui/SearchBox.svelte';
	import SidePanel from '#lib/ui/SidePanel.svelte';
	import StatusBadge from '#lib/ui/StatusBadge.svelte';

	type Stay = { id: string; code: string; status: string; from: string; to: string; total: number; currency: string };

	let q = $state('');
	let searched = $state('');
	let guests = $state<Guest[]>([]);
	let loaded = $state(false);
	let message = $state('');
	let open = $state(false);
	let profile = $state<{ guest: Guest; stays: Stay[] } | null>(null);

	async function search(term: string) {
		try {
			searched = term;
			guests = await api.get<Guest[]>(`/api/guests?q=${encodeURIComponent(term)}`);
			loaded = true;
		} catch (e) {
			message = errorText(e);
		}
	}

	$effect(() => {
		search('');
	});

	async function show(id: string) {
		try {
			profile = await api.get(`/api/guests/${id}`);
			open = true;
		} catch (e) {
			message = errorText(e);
		}
	}

	const word = (v: string) => {
		const w = v.replace('_', ' ');
		return w.charAt(0).toLocaleUpperCase('en') + w.slice(1);
	};

	// Revenue sees names only, so the API sends no contact details and the columns stay out.
	const canContact = $derived(session.effectiveRole !== 'revenue');
	const columns = $derived([
		{ id: 'name', label: t('guests.name'), value: (g: Guest) => g.name, priority: 'main' as const },
		...(canContact
			? [
					{ id: 'email', label: t('guests.col.email'), value: (g: Guest) => g.email ?? '', priority: 'detail' as const },
					{ id: 'phone', label: t('guests.col.phone'), value: (g: Guest) => g.phone ?? '', priority: 'extra' as const }
				]
			: [])
	]);
</script>

<svelte:head><title>{t('guests.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('guests.title')} note={t('guests.note')} />
<SearchBox label={t('guests.search')} bind:value={q} onsearch={search} />
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

{#if loaded}
	<DataTable label={t('guests.title')} rows={guests} {columns} rowKey={(g) => g.id} onopen={(g) => show(g.id)}>
		{#snippet empty()}
			<p class="muted">{searched ? t('guests.noMatch', { q: searched }) : t('guests.empty')}</p>
			{#if searched}<Button variant="secondary" onclick={() => { q = ''; search(''); }}>{t('search.clear')}</Button>{/if}
		{/snippet}
	</DataTable>
{/if}

<SidePanel title={profile?.guest.name ?? t('guests.title')} bind:open>
	{#if profile}
		{#if profile.guest.email}<p class="muted">{profile.guest.email}</p>{/if}
		{#if profile.guest.phone}<p class="muted">{profile.guest.phone}</p>{/if}
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
						<StatusBadge tone={s.status === 'checked_out' ? 'neutral' : 'info'}>{word(s.status)}</StatusBadge>
						<span class="muted">{formatMoney(s.total, s.currency)}</span>
					</li>
				{/each}
			</ul>
		{/if}
	{/if}
</SidePanel>

<style>
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
	.stays a {
		color: var(--color-action-ink);
		font: var(--font-body-strong);
	}
	.muted {
		color: var(--color-text-muted);
	}
</style>
