<script lang="ts">
	import Button from './Button.svelte';
	import Card from './Card.svelte';
	import SectionTitle from './SectionTitle.svelte';
	import StatusBadge from './StatusBadge.svelte';

	type Alert = { id: string; level: 'urgent' | 'today'; title: string; reason: string; action: string };
	type Props = { items: Alert[]; onact?: (id: string) => void; ondismiss?: (id: string) => void; checkedAt?: string };
	let { items, onact, ondismiss, checkedAt }: Props = $props();

	let gone = $state<Record<string, true>>({});
	const open = $derived(items.filter((item) => !gone[item.id]));
	const plural = new Intl.PluralRules('en');
	const count = $derived(open.length);
	const note = $derived(
		count === 0
			? 'Nothing needs attention'
			: `${count} ${plural.select(count) === 'one' ? 'item' : 'items'}${checkedAt ? ` · checked at ${checkedAt}` : ''}`
	);
	const levels = { urgent: { label: 'Urgent', tone: 'danger' }, today: { label: 'Today', tone: 'warning' } } as const;

	function close(id: string, acted: boolean) {
		gone[id] = true;
		if (acted) onact?.(id);
		else ondismiss?.(id);
	}
</script>

<Card label="Needs attention">
	<SectionTitle title="Needs attention" {note} />
	<ul>
		{#each open as item (item.id)}
			<li>
				<StatusBadge tone={levels[item.level].tone}>{levels[item.level].label}</StatusBadge>
				<div class="what">
					<div class="title">{item.title}</div>
					<div class="reason">{item.reason}</div>
				</div>
				<div class="actions">
					<Button variant="secondary" onclick={() => close(item.id, true)}>{item.action}</Button>
					<Button variant="quiet" onclick={() => close(item.id, false)}>Dismiss</Button>
				</div>
			</li>
		{/each}
	</ul>
</Card>

<style>
	ul {
		margin: 0;
		padding: 0;
		list-style: none;
	}
	li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--gap-related) var(--gap-group);
		margin-block-start: var(--space-3);
		padding-block-start: var(--space-3);
		border-block-start: var(--border-width) solid var(--color-divider);
	}
	.what {
		flex: 1 1 var(--panel-width);
		min-inline-size: 0;
	}
	.title {
		font: var(--font-body-strong);
	}
	.reason {
		font: var(--font-caption);
		color: var(--color-text-muted);
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: var(--gap-related);
	}
</style>
