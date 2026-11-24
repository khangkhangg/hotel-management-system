import { render, screen, within } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import { createRawSnippet, type Component } from 'svelte';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import AlertList from './AlertList.svelte';
import Checkbox from './Checkbox.svelte';
import Dialog from './Dialog.svelte';
import Select from './Select.svelte';
import Button from './Button.svelte';
import Field from './Field.svelte';
import HoverCard from './HoverCard.svelte';
import Icon, { iconNames } from './Icon.svelte';
import PageHeader from './PageHeader.svelte';
import Segmented from './Segmented.svelte';
import SidePanel from './SidePanel.svelte';
import SearchBox from './SearchBox.svelte';
import FilterChips from './FilterChips.svelte';
import Toaster from './Toaster.svelte';
import { toasts } from './toast.svelte.ts';
import DataTable from './DataTable.svelte';
import StatTile from './StatTile.svelte';
import StatusBadge from './StatusBadge.svelte';

const text = (t: string) => createRawSnippet(() => ({ render: () => `<span>${t}</span>` }));

describe('Button', () => {
	test('icon-only button has an accessible name', () => {
		render(Button, { icon: 'close', iconOnly: true, children: text('Close chat') });
		expect(screen.getByRole('button', { name: 'Close chat' })).toBeInTheDocument();
	});
	test('a button with an href is a link', () => {
		render(Button, { href: '/book', children: text('Book a room') });
		expect(screen.getByRole('link', { name: 'Book a room' })).toHaveAttribute('href', '/book');
	});
	test('a disabled button cannot be pressed', async () => {
		const onclick = vi.fn();
		render(Button, { disabled: true, onclick, children: text('Sold out') });
		await userEvent.click(screen.getByRole('button', { name: 'Sold out' }));
		expect(onclick).not.toHaveBeenCalled();
	});
	test('an enabled button calls onclick once', async () => {
		const onclick = vi.fn();
		render(Button, { onclick, children: text('Check in') });
		await userEvent.click(screen.getByRole('button', { name: 'Check in' }));
		expect(onclick).toHaveBeenCalledTimes(1);
	});
});

describe('Icon', () => {
	test('every icon name resolves to an svg', () => {
		for (const name of iconNames) {
			const { container, unmount } = render(Icon, { name });
			expect(container.querySelector('svg'), name).not.toBeNull();
			unmount();
		}
	});
	test('an icon is hidden from screen readers', () => {
		const { container } = render(Icon, { name: 'check' });
		expect(container.querySelector('svg')).toHaveAttribute('aria-hidden', 'true');
	});
});

describe('Segmented', () => {
	const options = [
		{ id: 'frontdesk', label: 'Front desk' },
		{ id: 'revenue', label: 'Revenue' },
		{ id: 'owner', label: 'Owner' }
	];
	test('marks the chosen option and follows a click', async () => {
		render(Segmented, { options, value: 'frontdesk' });
		expect(screen.getByRole('button', { name: 'Front desk' })).toHaveAttribute('aria-pressed', 'true');
		await userEvent.click(screen.getByRole('button', { name: 'Revenue' }));
		expect(screen.getByRole('button', { name: 'Revenue' })).toHaveAttribute('aria-pressed', 'true');
		expect(screen.getByRole('button', { name: 'Front desk' })).toHaveAttribute('aria-pressed', 'false');
		expect(screen.getByRole('button', { name: 'Owner' })).toHaveAttribute('aria-pressed', 'false');
	});
	test('calls onchange with the id', async () => {
		const onchange = vi.fn();
		render(Segmented, { options, value: 'frontdesk', onchange });
		await userEvent.click(screen.getByRole('button', { name: 'Owner' }));
		expect(onchange).toHaveBeenCalledWith('owner');
	});
});

describe('Field', () => {
	test('shows its error and links it to the input', () => {
		render(Field, { label: 'Email', error: 'Enter an email address like name@example.com' });
		const input = screen.getByLabelText('Email');
		expect(input).toHaveAttribute('aria-invalid', 'true');
		const message = screen.getByText('Enter an email address like name@example.com');
		expect(input.getAttribute('aria-describedby')).toBe(message.id);
	});
	test('has no error attributes when there is no error', () => {
		render(Field, { label: 'Email' });
		const input = screen.getByLabelText('Email');
		expect(input).not.toHaveAttribute('aria-invalid');
		expect(input).not.toHaveAttribute('aria-describedby');
	});
	test('typing updates the value', async () => {
		render(Field, { label: 'Guests', type: 'number', value: '2' });
		const input = screen.getByLabelText('Guests') as HTMLInputElement;
		await userEvent.clear(input);
		await userEvent.type(input, '3');
		expect(input.value).toBe('3');
	});
});

describe('StatusBadge and StatTile', () => {
	test('a badge shows its word', () => {
		render(StatusBadge, { tone: 'success', children: text('Paid') });
		expect(screen.getByText('Paid')).toBeInTheDocument();
	});
	test('a tile reads label, value, change', () => {
		const { container } = render(StatTile, { label: 'Occupancy', value: '71', unit: '%', note: '+6 points vs October', tone: 'up' });
		const lines = [...container.querySelectorAll('[data-line]')].map((el) => el.textContent?.trim());
		expect(lines).toEqual(['Occupancy', '71%', '+6 points vs October']);
	});
});

describe('PageHeader', () => {
	test('has one heading, and the primary action sits in the primary slot', () => {
		render(PageHeader, { title: 'Calendar', note: '23 Nov to 6 Dec', primary: text('New booking') });
		expect(screen.getByRole('heading', { level: 1, name: 'Calendar' })).toBeInTheDocument();
		expect(screen.getByText('New booking')).toBeInTheDocument();
	});
});

describe('HoverCard', () => {
	beforeEach(() => vi.useFakeTimers({ shouldAdvanceTime: false }));
	afterEach(() => vi.useRealTimers());

	const trigger = createRawSnippet<[{ 'aria-describedby': string }]>((attrs) => ({
		render: () => `<button aria-describedby="${attrs()['aria-describedby']}">Amir Khan</button>`
	}));
	const props = { title: 'Amir Khan', lines: [{ label: 'Room', value: '103, Standard' }], trigger };

	test('does not open before 300 ms of hover and opens at 300 ms', async () => {
		const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
		render(HoverCard, props);
		await user.hover(screen.getByRole('button', { name: 'Amir Khan' }));
		vi.advanceTimersByTime(299);
		expect(screen.queryByRole('tooltip')).toBeNull();
		vi.advanceTimersByTime(1);
		expect(await screen.findByRole('tooltip')).toBeInTheDocument();
	});
	test('opens at once on focus, describes the trigger, and closes on Escape', async () => {
		const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
		render(HoverCard, props);
		await user.tab();
		const tip = await screen.findByRole('tooltip');
		expect(screen.getByRole('button', { name: 'Amir Khan' }).getAttribute('aria-describedby')).toBe(tip.id);
		expect(within(tip).getByText('103, Standard')).toBeInTheDocument();
		await user.keyboard('{Escape}');
		expect(screen.queryByRole('tooltip')).toBeNull();
	});
	test('leaving the trigger closes it', async () => {
		const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
		render(HoverCard, props);
		await user.hover(screen.getByRole('button', { name: 'Amir Khan' }));
		vi.advanceTimersByTime(300);
		await screen.findByRole('tooltip');
		await user.unhover(screen.getByRole('button', { name: 'Amir Khan' }));
		expect(screen.queryByRole('tooltip')).toBeNull();
	});
});

describe('AlertList', () => {
	const items = [
		{ id: 'a', level: 'urgent' as const, title: 'Expedia has not confirmed an update for 6 minutes', reason: 'Standard has 1 room left on Tue 24 Nov.', action: 'Stop sell on Expedia' },
		{ id: 'b', level: 'today' as const, title: 'Room 103 is not ready for a 14:00 arrival', reason: 'Amir Khan arrives in 2 hours.', action: 'Move up the cleaning list' }
	];
	test('calls onact with the id and removes the row', async () => {
		const onact = vi.fn();
		render(AlertList, { items, onact });
		expect(screen.getAllByRole('listitem')).toHaveLength(2);
		await userEvent.click(screen.getByRole('button', { name: 'Stop sell on Expedia' }));
		expect(onact).toHaveBeenCalledWith('a');
		expect(screen.getAllByRole('listitem')).toHaveLength(1);
	});
	test('dismiss removes the row and calls ondismiss, not onact', async () => {
		const onact = vi.fn();
		const ondismiss = vi.fn();
		render(AlertList, { items, onact, ondismiss });
		await userEvent.click(screen.getAllByRole('button', { name: 'Dismiss' })[1]);
		expect(ondismiss).toHaveBeenCalledWith('b');
		expect(onact).not.toHaveBeenCalled();
		expect(screen.getAllByRole('listitem')).toHaveLength(1);
	});
	test('an empty list says nothing needs attention', () => {
		render(AlertList, { items: [] });
		expect(screen.getByText('Nothing needs attention')).toBeInTheDocument();
	});
	test('the header counts the open items with the right plural', async () => {
		render(AlertList, { items });
		expect(screen.getByText(/2 items/)).toBeInTheDocument();
		await userEvent.click(screen.getAllByRole('button', { name: 'Dismiss' })[0]);
		expect(screen.getByText(/1 item(?!s)/)).toBeInTheDocument();
	});
});

describe('Button type', () => {
	test('a submit button has type submit, so Enter in a form sends it', () => {
		render(Button, { type: 'submit', children: text('Go') });
		expect(screen.getByRole('button', { name: 'Go' })).toHaveAttribute('type', 'submit');
	});

	test('a button is not a submit button unless asked', () => {
		render(Button, { children: text('Plain') });
		expect(screen.getByRole('button', { name: 'Plain' })).toHaveAttribute('type', 'button');
	});
});

describe('Checkbox', () => {
	test('is found by its label and toggles', async () => {
		render(Checkbox, { label: 'Trust this device for 30 days' });
		const box = screen.getByLabelText('Trust this device for 30 days');
		expect(box).not.toBeChecked();
		await userEvent.click(box);
		expect(box).toBeChecked();
	});
});

describe('Select', () => {
	const options = [
		{ id: 'a', label: 'Alpha' },
		{ id: 'b', label: 'Beta' }
	];

	test('is found by its label and reports a change', async () => {
		const onchange = vi.fn();
		render(Select, { label: 'Role', options, value: 'a', onchange });
		const select = screen.getByLabelText('Role');
		expect(select).toHaveValue('a');
		await userEvent.selectOptions(select, 'Beta');
		expect(onchange).toHaveBeenCalledWith('b');
	});

	test('can hide its label and still be named', () => {
		render(Select, { label: 'Property', options, value: 'a', hideLabel: true });
		expect(screen.getByRole('combobox', { name: 'Property' })).toBeInTheDocument();
	});
});

describe('Dialog', () => {
	test('shows its title and content when open, named by the title', () => {
		render(Dialog, { title: 'Invite user', open: true, children: text('Body text') });
		expect(screen.getByRole('dialog', { name: 'Invite user' })).toBeInTheDocument();
		expect(screen.getByText('Body text')).toBeInTheDocument();
	});

	test('the close button asks to close', async () => {
		const onclose = vi.fn();
		render(Dialog, { title: 'Invite user', open: true, onclose, children: text('Body') });
		await userEvent.click(screen.getByRole('button', { name: 'Close' }));
		expect(onclose).toHaveBeenCalled();
	});
});

describe('SidePanel', () => {
	test('is named by its title and shows its content', () => {
		render(SidePanel, { title: 'New promo code', open: true, children: text('Fields') });
		expect(screen.getByRole('dialog', { name: 'New promo code' })).toBeInTheDocument();
		expect(screen.getByText('Fields')).toBeInTheDocument();
	});

	test('puts the cursor in the first field when it opens', async () => {
		const field = createRawSnippet(() => ({ render: () => '<input aria-label="Code" />' }));
		render(SidePanel, { title: 'New promo code', open: true, children: field });
		await vi.waitFor(() => expect(screen.getByLabelText('Code')).toHaveFocus());
	});

	test('closes at once when nothing was changed', async () => {
		const onclose = vi.fn();
		render(SidePanel, { title: 'New promo code', open: true, onclose, children: text('Fields') });
		await userEvent.click(screen.getByRole('button', { name: 'Close' }));
		expect(onclose).toHaveBeenCalledTimes(1);
	});

	test('asks before it throws away changes', async () => {
		const onclose = vi.fn();
		render(SidePanel, { title: 'New promo code', open: true, dirty: true, onclose, children: text('Fields') });
		await userEvent.click(screen.getByRole('button', { name: 'Close' }));
		expect(onclose).not.toHaveBeenCalled();
		expect(screen.getByText('Discard your changes?')).toBeInTheDocument();
		await userEvent.click(screen.getByRole('button', { name: 'Keep editing' }));
		expect(screen.queryByText('Discard your changes?')).not.toBeInTheDocument();
		await userEvent.click(screen.getByRole('button', { name: 'Close' }));
		await userEvent.click(screen.getByRole('button', { name: 'Discard' }));
		expect(onclose).toHaveBeenCalledTimes(1);
	});
});

describe('SearchBox', () => {
	test('reports what was typed once the person pauses', async () => {
		const onsearch = vi.fn();
		render(SearchBox, { label: 'Search guests', delay: 10, onsearch });
		await userEvent.type(screen.getByRole('searchbox', { name: 'Search guests' }), 'lan');
		await vi.waitFor(() => expect(onsearch).toHaveBeenLastCalledWith('lan'));
		expect(onsearch).toHaveBeenCalledTimes(1);
	});

	test('shows its label as the placeholder so the box is not unnamed', () => {
		render(SearchBox, { label: 'Search guests' });
		expect(screen.getByRole('searchbox')).toHaveAttribute('placeholder', 'Search guests');
	});

	test('clears with one button that only shows when there is text', async () => {
		const onsearch = vi.fn();
		render(SearchBox, { label: 'Search guests', delay: 0, value: 'lan', onsearch });
		await userEvent.click(screen.getByRole('button', { name: 'Clear search' }));
		await vi.waitFor(() => expect(onsearch).toHaveBeenLastCalledWith(''));
		expect(screen.queryByRole('button', { name: 'Clear search' })).not.toBeInTheDocument();
	});
});

describe('FilterChips', () => {
	const chips = [
		{ id: 'status', label: 'Status: Paid' },
		{ id: 'staff', label: 'Staff: Lan' }
	];

	test('each active filter can be removed by name', async () => {
		const onremove = vi.fn();
		render(FilterChips, { chips, onremove, onclear: vi.fn() });
		await userEvent.click(screen.getByRole('button', { name: 'Remove Status: Paid' }));
		expect(onremove).toHaveBeenCalledWith('status');
	});

	test('Clear all shows from two filters up', async () => {
		const onclear = vi.fn();
		const { rerender } = render(FilterChips, { chips, onremove: vi.fn(), onclear });
		await userEvent.click(screen.getByRole('button', { name: 'Clear all' }));
		expect(onclear).toHaveBeenCalled();
		await rerender({ chips: [chips[0]], onremove: vi.fn(), onclear });
		expect(screen.queryByRole('button', { name: 'Clear all' })).not.toBeInTheDocument();
	});

	test('shows nothing when no filter is set', () => {
		const { container } = render(FilterChips, { chips: [], onremove: vi.fn(), onclear: vi.fn() });
		expect(container.querySelector('ul')).toBeNull();
	});
});

// DataTable is generic, so the test treats it as one that takes loose props.
const Table = DataTable as unknown as Component<Record<string, unknown>>;

describe('DataTable', () => {
	type Row = { id: string; name: string; status: string; total: string };
	const rows: Row[] = [
		{ id: 'a', name: 'Anna Lee', status: 'Paid', total: '120.00' },
		{ id: 'b', name: 'Ben Ho', status: 'Due', total: '80.00' }
	];
	const columns = [
		{ id: 'name', label: 'Guest', value: (r: Row) => r.name, priority: 'main' as const, sortable: true },
		{ id: 'status', label: 'Status', value: (r: Row) => r.status, priority: 'status' as const },
		{ id: 'total', label: 'Total', value: (r: Row) => r.total, align: 'end' as const, priority: 'extra' as const }
	];
	const base = { label: 'Stays', rows, columns, rowKey: (r: Row) => r.id };

	test('shows a header cell per column and a row per item', () => {
		render(Table, base);
		expect(screen.getByRole('table', { name: 'Stays' })).toBeInTheDocument();
		expect(screen.getAllByRole('columnheader')).toHaveLength(3);
		expect(screen.getAllByRole('row')).toHaveLength(3);
		expect(screen.getByText('Ben Ho')).toBeInTheDocument();
	});

	test('opening a row is one button in its main cell', async () => {
		const onopen = vi.fn();
		render(Table, { ...base, onopen });
		await userEvent.click(screen.getByRole('button', { name: 'Anna Lee' }));
		expect(onopen).toHaveBeenCalledWith(rows[0]);
	});

	test('a sortable column is a button that says which way it sorts', async () => {
		const onsort = vi.fn();
		render(Table, { ...base, sortBy: 'name', sortDir: 'asc', onsort });
		expect(screen.getByRole('columnheader', { name: 'Guest' })).toHaveAttribute('aria-sort', 'ascending');
		await userEvent.click(screen.getByRole('button', { name: 'Guest' }));
		expect(onsort).toHaveBeenCalledWith('name');
		expect(screen.getByRole('columnheader', { name: 'Status' })).not.toHaveAttribute('aria-sort');
	});

	test('a header with a hidden label is still named for screen readers', () => {
		const withActions = [...columns, { id: 'actions', label: 'Actions', value: () => '', hideLabel: true }];
		render(Table, { ...base, columns: withActions });
		const header = screen.getByRole('columnheader', { name: 'Actions' });
		expect(header.querySelector('.visually-hidden')).not.toBeNull();
	});

	test('numbers align to the end', () => {
		render(Table, base);
		expect(screen.getByText('120.00').closest('td')).toHaveAttribute('data-align', 'end');
	});

	test('the saved row is marked', () => {
		render(Table, { ...base, marked: 'b' });
		expect(screen.getByText('Ben Ho').closest('tr')).toHaveAttribute('data-marked', 'true');
		expect(screen.getByText('Anna Lee').closest('tr')).not.toHaveAttribute('data-marked');
	});

	test('with no rows it shows the empty state and no table', () => {
		render(Table, { ...base, rows: [], empty: text('Nothing here yet') });
		expect(screen.getByText('Nothing here yet')).toBeInTheDocument();
		expect(screen.queryByRole('table')).not.toBeInTheDocument();
	});
});

describe('Toaster', () => {
	afterEach(() => toasts.clear());

	test('shows a message in a status region and removes it after its time', async () => {
		render(Toaster);
		toasts.show('The code is added.', { ms: 30 });
		expect(await screen.findByRole('status')).toHaveTextContent('The code is added.');
		await vi.waitFor(() => expect(screen.queryByText('The code is added.')).not.toBeInTheDocument());
	});

	test('Undo runs its action and closes the message', async () => {
		const run = vi.fn();
		render(Toaster);
		toasts.show('The code is removed.', { ms: 5000, undo: { label: 'Undo', run } });
		await userEvent.click(await screen.findByRole('button', { name: 'Undo' }));
		expect(run).toHaveBeenCalledTimes(1);
		expect(screen.queryByText('The code is removed.')).not.toBeInTheDocument();
	});

	test('keeps at most 3 messages so they never pile up', async () => {
		render(Toaster);
		for (const m of ['one', 'two', 'three', 'four']) toasts.show(m, { ms: 5000 });
		await screen.findByText('four');
		expect(screen.queryByText('one')).not.toBeInTheDocument();
		expect(screen.getByText('two')).toBeInTheDocument();
	});
});
