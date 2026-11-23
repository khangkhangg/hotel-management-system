<script lang="ts">
	import { api } from '#lib/api.ts';
	import { t } from '#lib/i18n.ts';
	import StatusBadge from '#lib/ui/StatusBadge.svelte';

	type Risk = { score: number; level: 'low' | 'medium' | 'high'; reasons: { code: string; text: string }[] };
	type Props = { id: string };
	let { id }: Props = $props();

	let risk = $state<Risk | null>(null);
	const tones = { low: 'success', medium: 'warning', high: 'danger' } as const;

	$effect(() => {
		risk = null;
		api
			.get<Risk>(`/api/reservations/${id}/cancel-risk`)
			.then((r) => (risk = r))
			.catch(() => (risk = null));
	});
</script>

{#if risk}
	<div class="risk">
		<StatusBadge tone={tones[risk.level]}>{t(`cancelRisk.${risk.level}`)}</StatusBadge>
		{#if risk.reasons.length}
			<ul>
				{#each risk.reasons as r (r.code)}<li>{r.text}</li>{/each}
			</ul>
		{/if}
		<p class="muted">{t('cancelRisk.note')}</p>
	</div>
{/if}

<style>
	.risk {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	ul {
		margin: 0;
		padding-inline-start: var(--space-6);
	}
	p {
		margin: 0;
	}
	.muted {
		color: var(--color-text-muted);
	}
</style>
