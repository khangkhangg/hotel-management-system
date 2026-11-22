<script lang="ts">
	import '@fontsource/ibm-plex-sans/400.css';
	import '@fontsource/ibm-plex-sans/500.css';
	import '@fontsource/ibm-plex-sans/600.css';
	import '#lib/tokens.css';
	import '#lib/base.css';

	let { children } = $props();
</script>

{@render children()}
