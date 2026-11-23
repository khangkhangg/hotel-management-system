<script lang="ts">
	import { onMount } from 'svelte';
	import { api, errorText } from '#lib/api.ts';
	import { drafts } from '#lib/draft.ts';
	import { formatWhen } from '#lib/format.ts';
	import { t } from '#lib/i18n.ts';
	import { roleLabels, type Role } from '#lib/nav.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Select from '#lib/ui/Select.svelte';
	import StatusBadge from '#lib/ui/StatusBadge.svelte';

	type Row = {
		id: string;
		name: string;
		email: string;
		role: Role;
		propertyName: string;
		lastSignInAt: string | null;
		status: 'active' | 'deactivated' | 'invited';
	};

	const tones = { active: 'success', deactivated: 'neutral', invited: 'info' } as const;
	const statusLabels = { active: 'Active', deactivated: 'Deactivated', invited: 'Invited' } as const;

	let rows = $state<Row[]>([]);
	let loaded = $state(false);
	let message = $state('');
	let inviteOpen = $state(false);
	let inviteEmail = $state('');
	let inviteRole = $state<string>('front_desk');
	let inviteError = $state('');
	let sending = $state(false);
	let confirming = $state<Row | null>(null);
	let confirmOpen = $state(false);

	const isOwner = $derived(session.role === 'owner');
	const roleOptions = $derived((isOwner ? (['owner', 'manager', 'front_desk', 'revenue', 'housekeeping'] as Role[]) : (['manager', 'front_desk', 'revenue', 'housekeeping'] as Role[])).map((r) => ({ id: r, label: roleLabels[r] })));
	const you = $derived(session.current?.user.id);

	const load = async () => {
		rows = await api.get<Row[]>('/api/staff');
		loaded = true;
	};

	onMount(() => {
		// If the session ran out while an invite was being typed, put it back.
		const saved = drafts.restore('/staff')['staff-invite'] as { open: boolean; email: string; role: string } | undefined;
		if (saved) ({ open: inviteOpen, email: inviteEmail, role: inviteRole } = saved);
		const stop = drafts.register('staff-invite', () => ({ open: inviteOpen, email: inviteEmail, role: inviteRole }));
		load().catch((e) => (message = errorText(e)));
		return stop;
	});

	async function invite(event: SubmitEvent) {
		event.preventDefault();
		sending = true;
		inviteError = '';
		try {
			await api.post('/api/staff/invites', { email: inviteEmail, role: inviteRole, ...(inviteRole === 'owner' ? {} : { propertyId: session.activeProperty }) });
			message = t('staff.invited', { email: inviteEmail });
			inviteOpen = false;
			inviteEmail = '';
			await load();
		} catch (e) {
			inviteError = errorText(e);
		} finally {
			sending = false;
		}
	}

	async function act(row: Row, action: 'deactivate' | 'reactivate' | 'reset-sign-in') {
		message = '';
		try {
			await api.post(`/api/staff/${row.id}/${action}`);
			message = t(`staff.done.${action}`, { name: row.name });
			await load();
		} catch (e) {
			message = errorText(e);
		}
	}

	async function revoke(row: Row) {
		message = '';
		try {
			await api.delete(`/api/staff-invites/${row.id}`);
			message = t('staff.done.revoke', { email: row.email });
			await load();
		} catch (e) {
			message = errorText(e);
		}
	}

	function askDeactivate(row: Row) {
		confirming = row;
		confirmOpen = true;
	}
</script>

<svelte:head><title>{t('staff.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('staff.title')} note={t('staff.note', { where: session.propertyName })}>
	{#snippet primary()}
		<Button icon="plus" onclick={() => (inviteOpen = true)}>{t('staff.invite')}</Button>
	{/snippet}
</PageHeader>

{#if message}<p class="notice success" role="status">{message}</p>{/if}

{#if loaded && rows.length === 0}
	<p class="muted">{t('staff.empty')}</p>
{:else if rows.length > 0}
	<!-- svelte-ignore a11y_no_noninteractive_tabindex -- a scrolling table must be reachable by keyboard -->
	<div class="scroll" role="region" aria-label={t('staff.title')} tabindex="0">
		<table>
			<thead>
				<tr>
					<th scope="col">{t('staff.col.person')}</th>
					<th scope="col">{t('staff.col.role')}</th>
					<th scope="col">{t('staff.col.property')}</th>
					<th scope="col">{t('staff.col.lastSignIn')}</th>
					<th scope="col">{t('staff.col.status')}</th>
					<th scope="col"><span class="visually-hidden">{t('staff.col.actions')}</span></th>
				</tr>
			</thead>
			<tbody>
				{#each rows as row (row.id + row.status)}
					<tr>
						<th scope="row">
							<span class="person">{row.name || row.email}</span>
							{#if row.name}<span class="email">{row.email}</span>{/if}
						</th>
						<td>{roleLabels[row.role]}</td>
						<td>{row.propertyName}</td>
						<td>{row.status === 'invited' ? '' : formatWhen(row.lastSignInAt)}</td>
						<td><StatusBadge tone={tones[row.status]}>{statusLabels[row.status]}</StatusBadge></td>
						<td class="actions">
							{#if row.status === 'invited'}
								<Button variant="quiet" onclick={() => revoke(row)}>{t('staff.revoke')}</Button>
							{:else if row.id !== you && (isOwner || row.role !== 'owner')}
								{#if row.status === 'active'}
									<Button variant="quiet" onclick={() => act(row, 'reset-sign-in')}>{t('staff.resetSignIn')}</Button>
									<Button variant="quiet" onclick={() => askDeactivate(row)}>{t('staff.deactivate')}</Button>
								{:else}
									<Button variant="quiet" onclick={() => act(row, 'reactivate')}>{t('staff.reactivate')}</Button>
								{/if}
							{/if}
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}

<Dialog title={t('staff.invite')} bind:open={inviteOpen}>
	<form id="invite-form" class="stack" onsubmit={invite}>
		{#if inviteError}<p class="notice danger" role="alert">{inviteError}</p>{/if}
		<Field label={t('signIn.email')} type="email" autocomplete="off" bind:value={inviteEmail} />
		<Select label={t('staff.col.role')} options={roleOptions} bind:value={inviteRole} />
		{#if inviteRole !== 'owner'}<p class="muted">{t('staff.inviteWhere', { where: session.propertyName })}</p>{/if}
	</form>
	{#snippet actions()}
		<Button variant="quiet" onclick={() => (inviteOpen = false)}>{t('common.cancel')}</Button>
		<Button type="submit" disabled={sending} form="invite-form">{t('staff.sendInvite')}</Button>
	{/snippet}
</Dialog>

<Dialog title={t('staff.deactivate.title', { name: confirming?.name ?? '' })} bind:open={confirmOpen}>
	<p class="muted">{t('staff.deactivate.help')}</p>
	{#snippet actions()}
		<Button variant="quiet" onclick={() => (confirmOpen = false)}>{t('common.cancel')}</Button>
		<Button
			onclick={() => {
				confirmOpen = false;
				if (confirming) act(confirming, 'deactivate');
			}}>{t('staff.deactivate')}</Button
		>
	{/snippet}
</Dialog>

<style>
	.scroll {
		position: relative; /* keeps the visually hidden header text inside the scrolling box */
		overflow-x: auto;
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-lg);
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
		vertical-align: middle;
		border-block-end: var(--border-width) solid var(--color-divider);
		white-space: nowrap;
	}
	thead th {
		font: var(--font-label);
		color: var(--color-text-subtle);
		background: var(--color-surface-muted);
	}
	tbody tr:last-child > * {
		border-block-end: 0;
	}
	tbody th {
		font-weight: var(--font-weight-regular);
	}
	.person {
		display: block;
		font: var(--font-control);
		color: var(--color-text);
	}
	.email {
		display: block;
		font: var(--font-caption);
		color: var(--color-text-muted);
	}
	.actions {
		text-align: end;
	}
	.visually-hidden {
		position: absolute;
		inline-size: var(--size-hidden);
		block-size: var(--size-hidden);
		overflow: hidden;
		clip-path: inset(50%);
	}
</style>
