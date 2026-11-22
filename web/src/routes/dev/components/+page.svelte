<script lang="ts">
	import { brandPresets } from '#lib/brand.ts';
	import AlertList from '#lib/ui/AlertList.svelte';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import Field from '#lib/ui/Field.svelte';
	import HoverCard from '#lib/ui/HoverCard.svelte';
	import { iconNames } from '#lib/ui/Icon.svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import SectionTitle from '#lib/ui/SectionTitle.svelte';
	import Segmented from '#lib/ui/Segmented.svelte';
	import StatTile from '#lib/ui/StatTile.svelte';
	import StatusBadge from '#lib/ui/StatusBadge.svelte';

	let brand = $state<string>(brandPresets[0].color);
	let role = $state('frontdesk');
	let guests = $state('2');
	const roles = [
		{ id: 'frontdesk', label: 'Front desk' },
		{ id: 'revenue', label: 'Revenue' },
		{ id: 'owner', label: 'Owner' },
		{ id: 'housekeeping', label: 'Housekeeping' }
	];
	const alerts = [
		{ id: 'a', level: 'urgent' as const, title: 'Expedia has not confirmed an update for 6 minutes', reason: 'Standard has 1 room left on Tue 24 Nov, so a late update could sell it twice.', action: 'Stop sell on Expedia' },
		{ id: 'b', level: 'today' as const, title: 'Room 103 is not ready for a 14:00 arrival', reason: 'Amir Khan arrives in 2 hours. Cleaning has not started.', action: 'Move up the cleaning list' }
	];
	const tones = ['success', 'warning', 'danger', 'info', 'neutral'] as const;
</script>

<svelte:head><title>Components</title></svelte:head>

<main data-brand-scope style:--brand={brand}>
	<PageHeader title="Components" note="Every shared component, with the tokens from tokens.css">
		{#snippet secondary()}<Button variant="secondary">Export tokens</Button>{/snippet}
		{#snippet primary()}<Button icon="plus">New booking</Button>{/snippet}
	</PageHeader>

	<Card label="Brand colour">
		<SectionTitle title="Brand colour" note="One token, --brand. Status colours do not change." />
		<div class="row">
			<Segmented options={brandPresets.map((p) => ({ id: p.color, label: p.name }))} bind:value={brand} />
			<Button>Primary action</Button>
			<StatusBadge tone="success">Paid</StatusBadge>
		</div>
	</Card>

	<Card label="Buttons">
		<SectionTitle title="Buttons" />
		<div class="row">
			<Button>Primary</Button>
			<Button variant="secondary">Secondary</Button>
			<Button variant="quiet">Quiet</Button>
			<Button icon="arrivals" variant="secondary">With icon</Button>
			<Button icon="close" iconOnly variant="secondary">Close chat</Button>
			<Button disabled>Sold out</Button>
			<Button href="#buttons" variant="quiet">As a link</Button>
		</div>
	</Card>

	<Card label="Fields">
		<SectionTitle title="Fields and segmented control" />
		<div class="row">
			<Field label="Check-in" type="date" value="2026-11-27" />
			<Field label="Guests" type="number" bind:value={guests} />
			<Field label="Email" type="email" value="mai@" error="Enter an email address like name@example.com" />
		</div>
		<div class="row"><Segmented options={roles} bind:value={role} /></div>
	</Card>

	<Card label="Status and figures">
		<SectionTitle title="Status badges and stat tiles" />
		<div class="row">
			{#each tones as tone (tone)}<StatusBadge {tone}>{tone}</StatusBadge>{/each}
		</div>
		<div class="tiles">
			<StatTile label="Occupancy" value="71" unit="%" note="+6 points vs October" tone="up" />
			<StatTile label="Channel commission" value="$1,312" note="+$143 vs October" tone="down" />
			<StatTile label="Arrivals today" value="3" note="2 pay at the desk" />
		</div>
	</Card>

	<Card label="Hover card">
		<SectionTitle title="Hover card" note="Hover for 300 ms, or focus with the keyboard" />
		<div class="row">
t			<!-- ai-design-check-ignore-next-line sentence-case -- a person's name -->
			<HoverCard title="Amir Khan" badge="Arriving today" tone="info" lines={[{ label: 'Room', value: '103, Standard' }, { label: 'Stay', value: '23 Nov to 26 Nov' }, { label: 'Payment', value: 'Paid in full' }]} note="Room not ready" hint="Click to open the booking">
				{#snippet trigger(attrs)}<Button variant="secondary" {...attrs}>Amir Khan</Button>{/snippet}
			</HoverCard>
		</div>
	</Card>

	<AlertList items={alerts} checkedAt="12:04" />

	<Card label="Icons">
		<SectionTitle title="Icons" note="Lucide, one size and stroke from tokens" />
		<div class="row">
			{#each iconNames as name (name)}<span class="icon-cell" title={name}><Icon {name} /></span>{/each}
		</div>
	</Card>
</main>

<style>
	main {
		display: flex;
		flex-direction: column;
		gap: var(--gap-section);
		max-inline-size: var(--content-max-width);
		margin-inline: auto;
		padding: var(--pad-page);
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: var(--gap-group);
		margin-block-start: var(--space-3);
	}
	.tiles {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(var(--hovercard-width), 1fr));
		gap: var(--gap-group);
		margin-block-start: var(--space-3);
	}
	.icon-cell {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		inline-size: var(--control-height);
		block-size: var(--control-height);
		border: var(--border-width) solid var(--color-border);
		border-radius: var(--radius-md);
	}
</style>
