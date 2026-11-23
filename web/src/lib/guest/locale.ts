// The guest site's languages (spec 05). A language is offered only once a native speaker has reviewed its text.
import en from '../../../messages/guest.en.json';
import zhHans from '../../../messages/guest.zh-Hans.json';
import hi from '../../../messages/guest.hi.json';
import es from '../../../messages/guest.es.json';
import fr from '../../../messages/guest.fr.json';
import ar from '../../../messages/guest.ar.json';
import bn from '../../../messages/guest.bn.json';
import pt from '../../../messages/guest.pt.json';
import ru from '../../../messages/guest.ru.json';
import ur from '../../../messages/guest.ur.json';

export type Locale = { code: string; name: string; dir: 'ltr' | 'rtl'; reviewed: boolean };

export const locales: Locale[] = [
	{ code: 'en', name: 'English', dir: 'ltr', reviewed: true },
	{ code: 'zh-Hans', name: '简体中文', dir: 'ltr', reviewed: false },
	{ code: 'hi', name: 'हिन्दी', dir: 'ltr', reviewed: false },
	{ code: 'es', name: 'Español', dir: 'ltr', reviewed: false },
	{ code: 'fr', name: 'Français', dir: 'ltr', reviewed: false },
	{ code: 'ar', name: 'العربية', dir: 'rtl', reviewed: false },
	{ code: 'bn', name: 'বাংলা', dir: 'ltr', reviewed: false },
	{ code: 'pt', name: 'Português', dir: 'ltr', reviewed: false },
	{ code: 'ru', name: 'Русский', dir: 'ltr', reviewed: false },
	{ code: 'ur', name: 'اردو', dir: 'rtl', reviewed: false }
];

// The other nine are drafts. A native speaker reviews each file, and the language is offered when `reviewed` is set above.
const dictionaries: Record<string, Record<string, string>> = { en, 'zh-Hans': zhHans, hi, es, fr, ar, bn, pt, ru, ur };

export const offered = (): Locale[] => locales.filter((l) => l.reviewed);

export const direction = (code: string): 'ltr' | 'rtl' => locales.find((l) => l.code === code)?.dir ?? 'ltr';

const isOffered = (code: string | undefined): code is string => !!code && offered().some((l) => l.code === code);

/** The address wins, then the saved choice, then the browser's languages, then the hotel's own default. */
export function pickLocale(o: { url?: string; saved?: string; browser: readonly string[]; fallback: string }): string {
	if (isOffered(o.url)) return o.url;
	if (isOffered(o.saved)) return o.saved;
	for (const tag of o.browser) {
		const exact = offered().find((l) => l.code.toLocaleLowerCase('en') === tag.toLocaleLowerCase('en'));
		const base = offered().find((l) => l.code.toLocaleLowerCase('en') === tag.split('-')[0].toLocaleLowerCase('en'));
		if (exact ?? base) return (exact ?? base)!.code;
	}
	return isOffered(o.fallback) ? o.fallback : 'en';
}

/** Text for a key in a language, with {placeholders} filled and plural forms (key.one, key.other) chosen for the language. */
export function translate(code: string, key: string, params: Record<string, string | number> = {}): string {
	const dict = dictionaries[code] ?? en;
	const lookup = (k: string) => dict[k] ?? (en as Record<string, string>)[k];
	let text: string | undefined;
	if (typeof params.count === 'number') {
		text = lookup(`${key}.${new Intl.PluralRules(code).select(params.count)}`) ?? lookup(`${key}.other`);
	}
	text ??= lookup(key);
	if (text === undefined) return key;
	return text.replace(/\{(\w+)\}/g, (whole, name) => (name in params ? String(params[name]) : whole));
}
