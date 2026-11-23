import { describe, expect, it } from 'vitest';
import { brandPresets, carriesWhiteText } from './brand.ts';

describe('brand colour', () => {
	it('accepts a dark colour and refuses one that white text cannot be read on', () => {
		expect(carriesWhiteText('#1F5F4F')).toBe(true);
		expect(carriesWhiteText('#FFE680')).toBe(false);
		expect(carriesWhiteText('#ffffff')).toBe(false);
		expect(carriesWhiteText('#000000')).toBe(true);
	});
	it('refuses what is not six hex digits', () => {
		for (const bad of ['', 'red', '#12', '#GGGGGG', '1F5F4F']) expect(carriesWhiteText(bad)).toBe(false);
	});
	it('keeps every preset readable', () => {
		for (const p of brandPresets) expect(carriesWhiteText(p.color), p.name).toBe(true);
	});
});
