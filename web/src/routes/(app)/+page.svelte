<script lang="ts">
	import { t } from '#lib/i18n.ts';
	import { milestoneFor } from '#lib/nav.ts';
	import PageHeader from '#lib/ui/PageHeader.svelte';
</script>

<svelte:head><title>{t('overview.title')} · {t('app.name')}</title></svelte:head>

<PageHeader title={t('overview.title')} note={t('overview.note')} />

<p class="muted">{t('overview.empty', { milestone: milestoneFor('/calendar') })}</p>
