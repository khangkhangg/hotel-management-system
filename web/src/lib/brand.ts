/** Sample brand colours for the component page. The real presets arrive with the branding settings. */
export const brandPresets = [
	{ id: 'forest', name: 'Forest', color: '#1F5F4F' },
	{ id: 'ocean', name: 'Ocean', color: '#1D5C8F' },
	{ id: 'plum', name: 'Plum', color: '#6B3A6B' },
	{ id: 'clay', name: 'Clay', color: '#9A4B2B' }
] as const;

/** True when white text reads on the colour (contrast 4.5 or more), which is the rule the server keeps too. */
export function carriesWhiteText(hex: string): boolean {
	if (!/^#[0-9A-Fa-f]{6}$/.test(hex)) return false;
	const light = (i: number) => {
		const x = parseInt(hex.slice(i, i + 2), 16) / 255;
		return x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4;
	};
	return 1.05 / (0.2126 * light(1) + 0.7152 * light(3) + 0.0722 * light(5) + 0.05) >= 4.5;
}
