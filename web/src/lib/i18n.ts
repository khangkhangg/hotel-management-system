import en from '../../messages/en.json';

const messages: Record<string, string> = en;

/** True when the key has a message. */
export const has = (key: string) => key in messages;

/** The text for a key, with {placeholders} filled. A missing key shows itself, so a gap is easy to spot. */
export function t(key: string, params: Record<string, string | number> = {}): string {
	const text = messages[key];
	if (text === undefined) return key;
	return text.replace(/\{(\w+)\}/g, (whole, name) => (name in params ? String(params[name]) : whole));
}
