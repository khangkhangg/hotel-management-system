import { afterEach, describe, expect, it, vi } from 'vitest';
import { listen } from './sse.ts';

afterEach(() => {
	vi.unstubAllGlobals();
	vi.useRealTimers();
});

describe('listen', () => {
	it('stops for good when the session is gone', async () => {
		vi.useFakeTimers();
		const fetchMock = vi.fn(async () => new Response(null, { status: 401 }));
		vi.stubGlobal('fetch', fetchMock);
		const names: string[] = [];
		const stop = listen('/api/stream', {}, (name) => names.push(name));
		await vi.advanceTimersByTimeAsync(10000);
		stop();
		expect(fetchMock).toHaveBeenCalledTimes(1);
		expect(names).toEqual([]);
	});
});
