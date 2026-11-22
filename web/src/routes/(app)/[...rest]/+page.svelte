<script lang="ts">
	import { page } from '$app/state';
	import { t } from '#lib/i18n.ts';
	import { itemsFor, milestoneFor, roles } from '#lib/nav.ts';
	import PageHeader from '#lib/ui/PageHeader.svelte';

	// A screen that a later milestone builds. It names the milestone, so the menu never leads nowhere.
	const label = $derived(
		roles
			.flatMap(itemsFor)
			.find((i) => page.url.pathname === i.href || page.url.pathname.startsWith(i.href + '/'))?.label ?? t('placeholder.unknown')
	);
</script>

<svelte:head><title>{label} · {t('app.name')}</title></svelte:head>

<PageHeader title={label} note={t('placeholder.help', { milestone: milestoneFor(page.url.pathname) })} />
