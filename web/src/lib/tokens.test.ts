import { readFileSync, readdirSync, statSync } from 'node:fs';
import { extname, join, relative } from 'node:path';
import { expect, test } from 'vitest';

const src = join(process.cwd(), 'src');
const tokensFile = join(src, 'lib', 'tokens.css');

function walk(dir: string): string[] {
	return readdirSync(dir).flatMap((name) => {
		const path = join(dir, name);
		return statSync(path).isDirectory() ? walk(path) : [path];
	});
}

const styled = walk(src).filter((f) => ['.svelte', '.css'].includes(extname(f)) && f !== tokensFile);
const tokens = readFileSync(tokensFile, 'utf8');

/** The CSS a file contributes: whole file for .css, style blocks and style="" attributes for .svelte. */
function cssOf(file: string): string {
	const text = readFileSync(file, 'utf8');
	if (extname(file) === '.css') return text;
	const blocks = [...text.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)].map((m) => m[1]);
	const inline = [...text.matchAll(/\sstyle="([^"]*)"/g)].map((m) => m[1]);
	return [...blocks, ...inline].join('\n');
}

const stripComments = (css: string) => css.replace(/\/\*[\s\S]*?\*\//g, '');

/** Every declared custom property in tokens.css with its raw value. */
function declared(css: string): Map<string, string> {
	const out = new Map<string, string>();
	for (const m of stripComments(css).matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)) out.set(m[1], m[2].trim());
	return out;
}

test('no component file holds a literal design value', () => {
	const problems: string[] = [];
	for (const file of styled) {
		const css = stripComments(cssOf(file));
		const media = [...css.matchAll(/@media[^{]*/g)].map((m) => m[0]);
		const withoutMedia = media.reduce((acc, m) => acc.replace(m, ''), css);
		const checks: [string, RegExp][] = [
			['hex colour', /#[0-9a-fA-F]{3,8}\b/],
			['colour function', /\b(?:rgb|rgba|hsl|hsla|oklch|oklab|color-mix)\(/],
			['length in px, rem or em', /(?<![\w.-])(?:[1-9]\d*|0?\.\d+)(?:px|rem|em)\b/],
			['duration', /(?<![\w.-])(?:[1-9]\d*|0?\.\d+)m?s\b/],
			['z-index number', /z-index\s*:\s*-?\d/],
			['cubic-bezier', /cubic-bezier\(/],
			['font-family literal', /font-family\s*:\s*(?!var\()/]
		];
		for (const [label, re] of checks) {
			const hit = withoutMedia.match(re);
			if (hit) problems.push(`${relative(src, file)}: ${label} "${hit[0]}"`);
		}
		for (const m of media) {
			for (const px of m.matchAll(/(\d+)px/g)) {
				if (!['640', '1024'].includes(px[1])) problems.push(`${relative(src, file)}: media query "${m.trim()}" uses a width that is not a breakpoint`);
			}
		}
	}
	expect(problems).toEqual([]);
});

test('every var(--x) used in a component is defined in tokens.css or in the same file', () => {
	const known = declared(tokens);
	const problems: string[] = [];
	for (const file of styled) {
		const text = readFileSync(file, 'utf8');
		const local = new Set([...text.matchAll(/(--[\w-]+)\s*:/g)].map((m) => m[1]));
		for (const m of cssOf(file).matchAll(/var\((--[\w-]+)/g)) {
			if (!known.has(m[1]) && !local.has(m[1])) problems.push(`${relative(src, file)} uses ${m[1]}`);
		}
	}
	expect(problems).toEqual([]);
});

test('a meaning token reaches a raw value in at most 2 steps', () => {
	const map = declared(tokens);
	const depth = (name: string, seen = new Set<string>()): number => {
		const value = map.get(name) ?? '';
		const next = value.match(/^var\((--[\w-]+)\)$/);
		if (!next || seen.has(name)) return 0;
		return 1 + depth(next[1], new Set([...seen, name]));
	};
	const deep = [...map.keys()].filter((name) => depth(name) > 2);
	expect(deep).toEqual([]);
});

test('a brand override changes the action colour and no status colour', () => {
	const map = declared(tokens);
	const roots = (name: string, seen = new Set<string>()): Set<string> => {
		const out = new Set<string>();
		if (seen.has(name)) return out;
		seen.add(name);
		for (const m of (map.get(name) ?? '').matchAll(/var\((--[\w-]+)\)/g)) {
			out.add(m[1]);
			roots(m[1], seen).forEach((r) => out.add(r));
		}
		return out;
	};
	expect(roots('--color-action').has('--brand')).toBe(true);
	expect(roots('--color-action-hover').has('--brand')).toBe(true);
	for (const tone of ['success', 'warning', 'danger', 'info', 'neutral']) {
		for (const part of ['bg', 'fg', 'dot', 'border']) {
			const name = `--color-status-${tone}-${part}`;
			expect(map.has(name), name).toBe(true);
			expect(roots(name).has('--brand'), `${name} must not depend on the brand`).toBe(false);
		}
	}
});

test('tokens derived from the brand are re-evaluated wherever a brand is set', () => {
	// CSS resolves var() where a custom property is declared. A derived token declared only on :root
	// would ignore a --brand set on a wrapper, so each one also needs the [data-brand-scope] selector.
	const css = stripComments(tokens);
	const selectorOf = new Map<string, string>();
	for (const block of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
		for (const decl of block[2].matchAll(/(--[\w-]+)\s*:/g)) selectorOf.set(decl[1], block[1].trim());
	}
	const map = declared(tokens);
	const dependsOnBrand = (name: string, seen = new Set<string>()): boolean => {
		if (seen.has(name)) return false;
		seen.add(name);
		return [...(map.get(name) ?? '').matchAll(/var\((--[\w-]+)\)/g)].some((m) => m[1] === '--brand' || dependsOnBrand(m[1], seen));
	};
	const wrong = [...map.keys()].filter((name) => name !== '--brand' && dependsOnBrand(name) && !(selectorOf.get(name) ?? '').includes('[data-brand-scope]'));
	expect(wrong).toEqual([]);
});

test('every value the design spec fixes exists', () => {
	const map = declared(tokens);
	const want: Record<string, string> = {
		'--font-size-xs': '12px', '--font-size-sm': '14px', '--font-size-md': '16px', '--font-size-lg': '20px', '--font-size-xl': '28px',
		'--font-weight-regular': '400', '--font-weight-medium': '500', '--font-weight-semibold': '600',
		'--space-1': '4px', '--space-2': '8px', '--space-3': '12px', '--space-4': '16px', '--space-6': '24px', '--space-8': '32px',
		'--radius-sm': '4px', '--radius-md': '8px', '--radius-lg': '12px',
		'--control-height': '44px',
		'--palette-page': '#FDFBFA', '--palette-surface': '#FFFFFF', '--palette-border': '#EBEBEB', '--palette-border-strong': '#8F8F8F',
		'--palette-text': '#101828', '--palette-text-muted': '#666666', '--brand': '#1F5F4F', '--radius-xl': '16px', '--line-height-xs': '18px', '--row-height-compact': '40px', '--row-height-comfortable': '52px', '--bottom-bar-height': '56px', '--side-panel-width': '30rem', '--duration-toast': '5000ms', '--duration-skeleton': '1200ms',
		'--duration-screen': '200ms', '--duration-fast': '150ms', '--ease-standard': 'cubic-bezier(0.2, 0, 0, 1)',
		'--breakpoint-tablet': '640px', '--breakpoint-desktop': '1024px', '--sidebar-width': '240px', '--rail-width': '68px'
	};
	for (const [name, value] of Object.entries(want)) expect(map.get(name), name).toBe(value);
	expect(map.get('--font-family')).toContain('IBM Plex Sans');
});

test('shape meanings follow the design system', () => {
	const map = declared(tokens);
	const want: Record<string, string> = { '--radius-control': 'var(--radius-lg)', '--radius-card': 'var(--radius-xl)', '--radius-panel': 'var(--radius-xl)', '--radius-badge': 'var(--radius-full)' };
	for (const [name, value] of Object.entries(want)) expect(map.get(name), name).toBe(value);
});

test('compact density shortens rows and nothing else', () => {
	const css = stripComments(tokens);
	expect(css).toMatch(/:root\s*\{[\s\S]*--row-height:\s*var\(--row-height-comfortable\)/);
	expect(css).toMatch(/\[data-density="compact"\]\s*\{\s*--row-height:\s*var\(--row-height-compact\);\s*\}/);
});

test('a marked row follows the hotel brand', () => {
	expect(stripComments(tokens)).toMatch(/--color-row-marked:\s*var\(--brand-wash\)/);
});

const channel = (v: number) => (v / 255 <= 0.03928 ? v / 255 / 12.92 : ((v / 255 + 0.055) / 1.055) ** 2.4);
const luminance = (hex: string) => {
	const [r, g, b] = [1, 3, 5].map((i) => channel(parseInt(hex.slice(i, i + 2), 16)));
	return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};
const contrast = (a: string, b: string) => {
	const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
	return (hi + 0.05) / (lo + 0.05);
};

test('text colours reach 4.5:1 on every surface they sit on', () => {
	const map = declared(tokens);
	const hex = (name: string) => map.get(name) ?? '';
	const surfaces = ['page', 'surface', 'surface-muted', 'surface-sunken'];
	const low: string[] = [];
	for (const text of ['text', 'text-subtle', 'text-muted'])
		for (const s of surfaces) if (contrast(hex(`--palette-${text}`), hex(`--palette-${s}`)) < 4.5) low.push(`${text} on ${s}`);
	for (const tone of ['success', 'warning', 'danger', 'info'])
		if (contrast(hex(`--palette-${tone}-fg`), hex(`--palette-${tone}-bg`)) < 4.5) low.push(`${tone} fg on bg`);
	expect(low).toEqual([]);
});
