<script lang="ts">
	import { formatDay, overlapColumns, weekday } from './dates.ts';
	import { formatMoney } from './money.ts';
	import HoverCard from '#lib/ui/HoverCard.svelte';
	import type { Room, Stay } from './types.ts';

	type Props = { days: string[]; rooms: Room[]; stays: Stay[]; today: string; unread: string[]; onpick: (stay: Stay) => void; onday: (date: string) => void };
	let { days, rooms, stays, today, unread, onpick, onday }: Props = $props();

	const tones = { pending: 'warning', confirmed: 'info', checked_in: 'success', checked_out: 'neutral', cancelled: 'neutral', no_show: 'danger' } as const;
	const statusWords = { pending: 'Pending', confirmed: 'Confirmed', checked_in: 'Checked in', checked_out: 'Checked out', cancelled: 'Cancelled', no_show: 'No-show' } as const;

	const byRoom = $derived(
		Object.fromEntries(rooms.map((r) => [r.id, stays.filter((s) => s.roomId === r.id).map((s) => ({ s, p: overlapColumns(days, s.from, s.to) })).filter((x) => x.p)]))
	);
	const groups = $derived(
		rooms.reduce<{ name: string; rooms: Room[] }[]>((out, r) => {
			const g = out.find((x) => x.name === r.roomTypeName);
			if (g) g.rooms.push(r);
			else out.push({ name: r.roomTypeName, rooms: [r] });
			return out;
		}, [])
	);
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex -- a scrolling grid must be reachable by keyboard -->
<div class="scroll" role="region" aria-label="Calendar" tabindex="0">
	<div class="grid" style="--days: {days.length}">
		<div class="row head">
			<div class="label"></div>
			{#each days as d (d)}
				<button type="button" class="day" class:today={d === today} onclick={() => onday(d)} aria-label="{weekday(d)} {formatDay(d)}, new booking">
					<span>{weekday(d)}</span> <strong>{formatDay(d)}</strong>
				</button>
			{/each}
		</div>
		{#each groups as g (g.name)}
			<div class="group">{g.name}</div>
			{#each g.rooms as room (room.id)}
				<div class="row" role="group" aria-label="Room {room.number}">
					<div class="label"><strong>{room.number}</strong><small class:dirty={room.status !== 'clean'}>{room.status.replace('_', ' ')}</small></div>
					{#each days as d, i (d)}<div class="cell" class:today={d === today} style="grid-column: {i + 2}"></div>{/each}
					{#each byRoom[room.id] ?? [] as { s, p } (s.id)}
						{#if p}
							<div class="slot" style="grid-column: {p.start + 2} / span {p.span}">
								<HoverCard
									title={s.guestName}
									badge={statusWords[s.status]}
									tone={tones[s.status]}
									lines={[
										{ label: 'Dates', value: `${formatDay(s.from)} to ${formatDay(s.to)}` },
										{ label: 'Guests', value: `${s.adults}${s.children ? ` + ${s.children}` : ''}` },
										{ label: 'Total', value: formatMoney(s.total, s.currency) },
										{ label: 'Code', value: s.code }
									]}
									note={unread.includes(s.reservationId) ? 'New team notes' : undefined}
								>
									{#snippet trigger(attrs)}
										<button type="button" class="bar {s.status}" class:cutLeft={p.cutLeft} class:cutRight={p.cutRight} {...attrs} onclick={() => onpick(s)}>
											<span class="name">{s.guestName}</span>
											{#if unread.includes(s.reservationId)}<span class="dot" role="img" aria-label="New team notes"></span>{/if}
										</button>
									{/snippet}
								</HoverCard>
							</div>
						{/if}
					{/each}
				</div>
			{/each}
		{/each}
	</div>
</div>

<style>
	.scroll {
		overflow-x: auto;
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-lg);
		background: var(--color-surface);
	}
	.grid {
		min-inline-size: calc(var(--calendar-room-col) + var(--days) * var(--calendar-day-col));
	}
	.row {
		display: grid;
		grid-template-columns: var(--calendar-room-col) repeat(var(--days), minmax(var(--calendar-day-col), 1fr));
		min-block-size: var(--calendar-row-height);
		position: relative;
		border-block-end: var(--border-width) solid var(--color-divider);
	}
	.label {
		grid-column: 1;
		position: sticky;
		inset-inline-start: 0;
		z-index: var(--layer-raised);
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--gap-tight);
		padding-inline: var(--space-3);
		background: var(--color-surface);
		border-inline-end: var(--border-width) solid var(--color-divider);
		font: var(--font-control);
	}
	small {
		font: var(--font-caption);
		color: var(--color-text-muted);
	}
	small.dirty {
		color: var(--color-status-warning-fg);
	}
	.head {
		background: var(--color-surface-muted);
	}
	.day {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		border: 0;
		background: transparent;
		color: var(--color-text-subtle);
		font: var(--font-caption);
		cursor: pointer;
	}
	.day strong {
		font: var(--font-control);
		color: var(--color-text);
	}
	.day:focus-visible {
		outline: var(--border-width-strong) solid var(--color-focus);
		outline-offset: calc(-1 * var(--border-width-strong));
	}
	.cell {
		grid-row: 1;
		border-inline-start: var(--border-width) solid var(--color-divider);
	}
	.today {
		background: var(--color-action-wash);
	}
	.group {
		padding: var(--space-1) var(--space-3);
		background: var(--color-surface-muted);
		font: var(--font-label);
		color: var(--color-text-subtle);
	}
	.slot {
		grid-row: 1;
		min-inline-size: 0;
		padding: var(--space-1);
		display: flex;
	}
	.slot :global(.hovercard),
	.slot :global(span) {
		min-inline-size: 0;
	}
	.bar {
		flex: 1;
		min-inline-size: 0;
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--gap-tight);
		padding-inline: var(--space-2);
		border: 0;
		border-radius: var(--radius-sm);
		font: var(--font-caption);
		text-align: start;
		cursor: pointer;
		background: var(--color-status-info-bg);
		color: var(--color-status-info-fg);
	}
	.bar.checked_in {
		background: var(--color-status-success-bg);
		color: var(--color-status-success-fg);
	}
	.bar.pending {
		background: var(--color-status-warning-bg);
		color: var(--color-status-warning-fg);
	}
	.bar.checked_out,
	.bar.cancelled {
		background: var(--color-status-neutral-bg);
		color: var(--color-status-neutral-fg);
	}
	.bar.cutLeft {
		border-start-start-radius: 0;
		border-end-start-radius: 0;
	}
	.bar.cutRight {
		border-start-end-radius: 0;
		border-end-end-radius: 0;
	}
	.bar:focus-visible {
		outline: var(--border-width-strong) solid var(--color-focus);
		outline-offset: var(--border-width-strong);
	}
	.name {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.dot {
		flex: none;
		inline-size: var(--dot-size);
		block-size: var(--dot-size);
		border-radius: var(--radius-full);
		background: var(--color-status-danger-dot);
	}
</style>
