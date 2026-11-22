/** A time as a short date and time, such as "Nov 22, 2026, 8:30 AM". Null reads as "Never". */
export function formatWhen(iso: string | null, timeZone?: string): string {
	if (!iso) return 'Never';
	const date = new Date(iso);
	if (Number.isNaN(date.getTime())) return iso;
	return new Intl.DateTimeFormat('en', { dateStyle: 'medium', timeStyle: 'short', timeZone }).format(date);
}
