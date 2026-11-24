const zeroDecimal = new Set(['VND', 'JPY', 'KRW', 'CLP', 'IDR']);

/** An amount in the currency's smallest unit, written for people: 20000 USD is $200.00. */
export function formatMoney(amount: number, currency: string): string {
	const digits = zeroDecimal.has(currency) ? 0 : 2;
	const value = digits === 0 ? amount : amount / 100;
	try {
		return new Intl.NumberFormat('en', { style: 'currency', currency, minimumFractionDigits: digits, maximumFractionDigits: digits }).format(value);
	} catch {
		return `${value.toFixed(digits)} ${currency}`;
	}
}

/** Typed text as an amount in the smallest unit, or null when it is not a plain positive amount. "1,500" is refused rather than read as 1.50. */
export function parseMoney(text: string, currency: string): number | null {
	const digits = zeroDecimal.has(currency) ? 0 : 2;
	const match = new RegExp(`^(\\d+)(?:[.,](\\d{1,${digits || 1}}))?$`).exec(text.trim());
	if (!match || (digits === 0 && match[2] !== undefined)) return null;
	const whole = Number(match[1]);
	const fraction = digits === 0 ? 0 : Number((match[2] ?? '').padEnd(2, '0'));
	const amount = digits === 0 ? whole : whole * 100 + fraction;
	return amount > 0 ? amount : null;
}

/** An amount in the smallest unit, written the way a person types it into a field: 24000 USD is 240.00. */
export function plainAmount(amount: number, currency: string): string {
	return zeroDecimal.has(currency) ? String(amount) : (amount / 100).toFixed(2);
}

