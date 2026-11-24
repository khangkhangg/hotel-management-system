<script lang="ts" generics="T">
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';

	type Column = { id: string; label: string; value: (row: T) => string; priority?: 'main' | 'status' | 'detail' | 'extra'; align?: 'start' | 'end'; sortable?: boolean; hideLabel?: boolean };
	type Props = {
		label: string;
		rows: T[];
		columns: Column[];
		rowKey: (row: T) => string;
		onopen?: (row: T) => void;
		sortBy?: string;
		sortDir?: 'asc' | 'desc';
		onsort?: (id: string) => void;
		marked?: string;
		empty?: Snippet;
		cell?: Snippet<[T, Column]>;
	};
	let { label, rows, columns, rowKey, onopen, sortBy, sortDir = 'asc', onsort, marked, empty, cell }: Props = $props();
</script>

{#if rows.length === 0}
	{#if empty}<div class="empty">{@render empty()}</div>{/if}
{:else}
	<div class="wrap">
		<table aria-label={label}>
			<thead>
				<tr>
					{#each columns as column (column.id)}
						<th scope="col" data-priority={column.priority ?? 'detail'} data-align={column.align ?? 'start'} aria-sort={sortBy === column.id ? (sortDir === 'asc' ? 'ascending' : 'descending') : undefined}>
							{#if column.sortable}
								<button type="button" onclick={() => onsort?.(column.id)}>
									{column.label}
									{#if sortBy === column.id}<Icon name={sortDir === 'asc' ? 'up' : 'down'} />{/if}
								</button>
							{:else if column.hideLabel}
								<span class="visually-hidden">{column.label}</span>
							{:else}
								{column.label}
							{/if}
						</th>
					{/each}
				</tr>
			</thead>
			<tbody>
				{#each rows as row (rowKey(row))}
					<tr data-marked={marked === rowKey(row) ? 'true' : undefined}>
						{#each columns as column (column.id)}
							<td data-priority={column.priority ?? 'detail'} data-align={column.align ?? 'start'}>
								{#if column.priority === 'main' && onopen}
									<button type="button" class="open" onclick={() => onopen(row)}>{column.value(row)}</button>
								{:else if cell}
									{@render cell(row, column)}
								{:else}
									{column.value(row)}
								{/if}
							</td>
						{/each}
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}

<style>
	.wrap {
		overflow-x: auto;
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-card);
		background: var(--color-surface);
	}
	table {
		inline-size: 100%;
		border-collapse: collapse;
		font: var(--font-body);
		color: var(--color-text);
	}
	th {
		position: sticky;
		inset-block-start: 0;
		padding: var(--space-2) var(--space-3);
		background: var(--color-surface-muted);
		border-block-end: var(--border-width) solid var(--color-border);
		font: var(--font-label);
		color: var(--color-text-subtle);
		text-align: start;
	}
	td {
		block-size: var(--row-height);
		padding: var(--space-2) var(--space-3);
		border-block-end: var(--border-width) solid var(--color-divider);
	}
	tbody tr:last-child td {
		border-block-end: 0;
	}
	[data-align='end'] {
		text-align: end;
		font-variant-numeric: tabular-nums;
	}
	th button,
	.open {
		display: inline-flex;
		align-items: center;
		gap: var(--gap-tight);
		padding: 0;
		border: 0;
		background: transparent;
		color: inherit;
		font: inherit;
		text-align: start;
		cursor: pointer;
	}
	.open {
		font: var(--font-body-strong);
	}
	th button:focus-visible,
	.open:focus-visible {
		outline: var(--border-width-strong) solid var(--color-focus);
		outline-offset: var(--border-width-strong);
	}
	tr[data-marked='true'] td {
		background: var(--color-row-marked);
	}
	@media (hover: hover) {
		tbody tr:hover td {
			background: var(--color-button-quiet-bg-hover);
		}
	}
	.empty {
		padding: var(--space-2) 0;
	}
	@media (width < 1024px) {
		[data-priority='extra'] {
			display: none;
		}
	}
	@media (width < 640px) {
		thead {
			position: absolute;
			inline-size: var(--border-width);
			block-size: var(--border-width);
			overflow: hidden;
			clip-path: inset(50%);
		}
		tbody tr {
			display: grid;
			grid-template-columns: 1fr auto;
			align-items: center;
			padding: var(--space-2) var(--space-3);
			border-block-end: var(--border-width) solid var(--color-divider);
		}
		td {
			block-size: auto;
			padding: var(--space-1) 0;
			border: 0;
		}
		td[data-priority='main'] {
			grid-column: 1;
		}
		td[data-priority='status'] {
			grid-column: 2;
			grid-row: 1;
		}
		td[data-priority='detail'] {
			grid-column: 1 / -1;
			color: var(--color-text-muted);
		}
	}
</style>
