/**
 * Listens to a server-sent-events URL with fetch, so the request can carry the X-Property-Id header that
 * EventSource cannot send. Calls onEvent with each event's name. Returns a function that stops listening.
 * If the connection drops it tries again after a few seconds, and asks for a refresh when it is back.
 */
export function listen(url: string, headers: Record<string, string>, onEvent: (name: string, data: string) => void): () => void {
	const controller = new AbortController();
	let stopped = false;

	async function run() {
		while (!stopped) {
			try {
				const res = await fetch(url, { headers: { Accept: 'text/event-stream', ...headers }, credentials: 'same-origin', signal: controller.signal });
				if (!res.ok || !res.body) throw new Error(String(res.status));
				const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
				let buffer = '';
				for (;;) {
					const { value, done } = await reader.read();
					if (done) break;
					buffer += value;
					let end: number;
					while ((end = buffer.indexOf('\n\n')) >= 0) {
						const block = buffer.slice(0, end);
						buffer = buffer.slice(end + 2);
						const name = /^event: (.*)$/m.exec(block)?.[1];
						const data = /^data: (.*)$/m.exec(block)?.[1] ?? '';
						if (name) onEvent(name, data);
					}
				}
			} catch {
				if (stopped) return;
			}
			if (stopped) return;
			await new Promise((resolve) => setTimeout(resolve, 3000));
			onEvent('reconnected', '');
		}
	}
	run();
	return () => {
		stopped = true;
		controller.abort();
	};
}
