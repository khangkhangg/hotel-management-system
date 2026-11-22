<script module lang="ts">
	import {
		Bell,
		BedDouble,
		Calendar,
		ChartColumn,
		Check,
		ChevronDown,
		ChevronLeft,
		ChevronRight,
		ChevronUp,
		ChevronsUpDown,
		CreditCard,
		FileText,
		Link,
		List,
		LogIn,
		MessageSquare,
		Moon,
		Package,
		PanelLeft,
		Plus,
		ShieldCheck,
		Table2,
		Tag,
		TriangleAlert,
		User,
		X
	} from '@lucide/svelte';

	const icons = {
		calendar: Calendar,
		arrivals: LogIn,
		guests: User,
		card: CreditCard,
		moon: Moon,
		grid: Table2,
		tag: Tag,
		link: Link,
		list: List,
		chart: ChartColumn,
		file: FileText,
		shield: ShieldCheck,
		bed: BedDouble,
		alert: TriangleAlert,
		box: Package,
		panel: PanelLeft,
		plus: Plus,
		swap: ChevronsUpDown,
		check: Check,
		bell: Bell,
		close: X,
		chat: MessageSquare,
		left: ChevronLeft,
		right: ChevronRight,
		up: ChevronUp,
		down: ChevronDown
	} as const;

	export type IconName = keyof typeof icons;
	export const iconNames = Object.keys(icons) as IconName[];
</script>

<script lang="ts">
	let { name }: { name: IconName } = $props();
	const Glyph = $derived(icons[name]);
</script>

<span class="icon"><Glyph aria-hidden="true" /></span>

<style>
	.icon {
		display: inline-flex;
		flex: 0 0 auto;
	}
	.icon :global(svg) {
		inline-size: var(--icon-size);
		block-size: var(--icon-size);
		stroke-width: var(--icon-stroke);
	}
</style>
