<script lang="ts">
	import { api, errorText } from '#lib/api.ts';
	import type { Room, RoomType } from '#lib/booking/types.ts';
	import { t } from '#lib/i18n.ts';
	import { session } from '#lib/session.svelte.ts';
	import Button from '#lib/ui/Button.svelte';
	import SidePanel from '#lib/ui/SidePanel.svelte';
	import Field from '#lib/ui/Field.svelte';
	import { cleaningOrder } from '#lib/housekeeping.ts';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Select from '#lib/ui/Select.svelte';
	import StatusBadge from '#lib/ui/StatusBadge.svelte';

	type Task = { id: string; roomId: string; roomNumber: string; floor: number; roomType: string; status: string; roomStatus: string; arrivalToday: boolean; departureToday: boolean };

	const today = session.today;
	let rooms = $state<Room[]>([]);
	let types = $state<RoomType[]>([]);
	let tasks = $state<Task[]>([]);
	let loaded = $state(false);
	let message = $state('');
	let typeOpen = $state(false);
	let roomOpen = $state(false);
	let typeName = $state('');
	let base = $state('2');
	let max = $state('3');
	let roomType = $state('');
	let number = $state('');
	let floor = $state('1');

	const tones = { clean: 'success', dirty: 'warning', inspect: 'info', out_of_order: 'danger' } as const;
	const canSetUp = $derived(session.effectiveRole === 'owner' || session.effectiveRole === 'manager');
	const byFloor = $derived(
		Object.entries(
			rooms.reduce<Record<number, Room[]>>((out, r) => {
				(out[r.floor] ??= []).push(r);
				return out;
			}, {})
		).sort(([a], [b]) => +a - +b)
	);

	async function load() {
		try {
			[rooms, types, tasks] = await Promise.all([api.get<Room[]>('/api/rooms'), api.get<RoomType[]>('/api/room-types'), api.get<Task[]>(`/api/housekeeping/tasks?date=${today}`)]);
			roomType = roomType || types[0]?.id || '';
			loaded = true;
		} catch (e) {
			message = errorText(e);
		}
	}
	$effect(() => {
		load();
	});

	async function run(f: () => Promise<unknown>) {
		message = '';
		try {
			await f();
			await load();
		} catch (e) {
			message = errorText(e);
		}
	}
	const statusWord = (v: string) => { const w = v.replace('_', ' '); return w.charAt(0).toLocaleUpperCase('en') + w.slice(1); };
	const count = (v: string) => rooms.filter((r) => r.status === v).length;
	const setStatus = (r: Room, status: string) => run(() => api.patch(`/api/rooms/${r.id}/status`, { status }));
	const setTask = (task: Task, status: string) => run(() => api.patch(`/api/housekeeping/tasks/${task.id}`, { status }));

	async function addType(event: SubmitEvent) {
		event.preventDefault();
		await run(async () => {
			await api.post('/api/room-types', { name: typeName, baseGuests: +base, maxGuests: +max });
			typeOpen = false;
			typeName = '';
		});
	}
	async function addRoom(event: SubmitEvent) {
		event.preventDefault();
		await run(async () => {
			await api.post('/api/rooms', { roomTypeId: roomType, number, floor: +floor || 0 });
			roomOpen = false;
			number = '';
		});
	}
</script>

<svelte:head><title>{t('rooms.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('rooms.title')} note={t('rooms.note')}>
	{#snippet secondary()}{#if canSetUp}<Button variant="secondary" onclick={() => (typeOpen = true)}>{t('rooms.addType')}</Button>{/if}{/snippet}
	{#snippet primary()}{#if canSetUp}<Button icon="plus" disabled={types.length === 0} onclick={() => (roomOpen = true)}>{t('rooms.addRoom')}</Button>{/if}{/snippet}
</PageHeader>
{#if message}<p class="notice danger" role="alert">{message}</p>{/if}

{#if tasks.length}
	<section class="stack-tight" aria-label={t('rooms.tasks')}>
		<h2>{t('rooms.tasks')}</h2>
		<ul class="tasks">
			{#each cleaningOrder(tasks) as task (task.id)}
				<li class:done={task.status === 'done'}>
					<span><strong>{t('rooms.room', { number: task.roomNumber })}</strong>, {t('rooms.floor', { floor: task.floor })}
						{#if task.arrivalToday}<StatusBadge tone="info">{t('rooms.arrivalToday')}</StatusBadge>{/if}</span>
					<span class="row-actions">
						<StatusBadge tone={task.status === 'done' ? 'success' : task.status === 'in_progress' ? 'info' : 'warning'}>{statusWord(task.status)}</StatusBadge>
						{#if task.status === 'todo'}<Button variant="secondary" onclick={() => setTask(task, 'in_progress')}>{t('rooms.start')}</Button>{/if}
						{#if task.status !== 'done'}<Button onclick={() => setTask(task, 'done')}>{t('rooms.markClean')}</Button>{/if}
					</span>
				</li>
			{/each}
		</ul>
	</section>
{/if}

{#if loaded && rooms.length > 0}
	<p class="muted">{t('rooms.summary', { clean: count('clean'), dirty: count('dirty'), inspect: count('inspect') })}</p>
{/if}

{#if loaded && rooms.length === 0}
	<p class="muted">{t('rooms.empty')}</p>
{:else}
	{#each byFloor as [floorNo, list] (floorNo)}
		<section class="stack-tight" aria-label={t('rooms.floor', { floor: floorNo })}>
			<h2>{t('rooms.floor', { floor: floorNo })}</h2>
			<ul class="board">
				{#each list as r (r.id)}
					<li class="room">
						<p class="top"><strong>{r.number}</strong><StatusBadge tone={tones[r.status as keyof typeof tones] ?? 'neutral'}>{statusWord(r.status)}</StatusBadge></p>
						<p class="muted">{r.roomTypeName}</p>
						{#if r.status !== 'out_of_order'}
							<p class="row-actions">
								{#if r.status !== 'clean'}<Button variant="quiet" onclick={() => setStatus(r, 'clean')}>{t('rooms.clean')}</Button>{/if}
								{#if r.status !== 'dirty'}<Button variant="quiet" onclick={() => setStatus(r, 'dirty')}>{t('rooms.dirty')}</Button>{/if}
								{#if r.status !== 'inspect'}<Button variant="quiet" onclick={() => setStatus(r, 'inspect')}>{t('rooms.inspect')}</Button>{/if}
							</p>
						{/if}
					</li>
				{/each}
			</ul>
		</section>
	{/each}
{/if}

<SidePanel title={t('rooms.addType')} bind:open={typeOpen}>
	<form id="type-form" class="stack" onsubmit={addType}>
		<Field label={t('rooms.typeName')} bind:value={typeName} />
		<Field label={t('rooms.baseGuests')} inputmode="numeric" bind:value={base} />
		<Field label={t('rooms.maxGuests')} inputmode="numeric" bind:value={max} />
	</form>
	{#snippet actions()}
		<Button variant="quiet" onclick={() => (typeOpen = false)}>{t('common.cancel')}</Button>
		<Button type="submit" form="type-form" disabled={typeName.trim() === ''}>{t('rooms.createType')}</Button>
	{/snippet}
</SidePanel>

<SidePanel title={t('rooms.addRoom')} bind:open={roomOpen}>
	<form id="room-form" class="stack" onsubmit={addRoom}>
		<Select label={t('booking.roomType')} options={types.map((x) => ({ id: x.id, label: x.name }))} bind:value={roomType} />
		<Field label={t('rooms.number')} bind:value={number} />
		<Field label={t('rooms.floorLabel')} inputmode="numeric" bind:value={floor} />
	</form>
	{#snippet actions()}
		<Button variant="quiet" onclick={() => (roomOpen = false)}>{t('common.cancel')}</Button>
		<Button type="submit" form="room-form" disabled={number.trim() === ''}>{t('rooms.createRoom')}</Button>
	{/snippet}
</SidePanel>

<style>
	h2 {
		margin: 0;
		font: var(--font-heading);
	}
	p {
		margin: 0;
	}
	.tasks,
	.board {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.tasks {
		display: flex;
		flex-direction: column;
		gap: var(--gap-related);
	}
	.tasks li {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		align-items: center;
		gap: var(--gap-related);
		padding: var(--space-3);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
		font: var(--font-body);
	}
	.tasks li.done {
		color: var(--color-text-muted);
	}
	.board {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(var(--field-min), 1fr));
		gap: var(--gap-related);
	}
	.room {
		display: flex;
		flex-direction: column;
		gap: var(--gap-tight);
		padding: var(--space-3);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
	}
	.top {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: var(--gap-tight);
		font: var(--font-heading);
	}
</style>
