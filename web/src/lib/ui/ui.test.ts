import { render, screen, within } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import { createRawSnippet } from 'svelte';
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
