import { render, screen, within } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import { createRawSnippet } from 'svelte';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import AlertList from './AlertList.svelte';
import Button from './Button.svelte';
import Field from './Field.svelte';
import HoverCard from './HoverCard.svelte';
import Icon, { iconNames } from './Icon.svelte';
import PageHeader from './PageHeader.svelte';
import Segmented from './Segmented.svelte';
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
