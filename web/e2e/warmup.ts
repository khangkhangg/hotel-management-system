// The first server render of the booking page makes the dev server compile it, which can take half a minute. Doing it
// once here keeps that cost out of the first test that opens the page.
export default async function warmUp(): Promise<void> {
	const res = await fetch('http://127.0.0.1:5173/book/main-hotel', { signal: AbortSignal.timeout(170_000) });
	await res.text();
}
