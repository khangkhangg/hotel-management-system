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
