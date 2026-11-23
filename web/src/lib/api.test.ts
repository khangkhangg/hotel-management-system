import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { api, ApiError, resetApi, setProperty, errorText, onUnauthorized } from './api.ts';

type Call = { url: string; init: RequestInit };
let calls: Call[];
let replies: Response[];

const json = (status: number, body: unknown) =>
	new Response(status === 204 ? null : JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

beforeEach(() => {
	calls = [];
	replies = [];
	resetApi();
	vi.stubGlobal(
		'fetch',
		vi.fn(async (url: string, init: RequestInit = {}) => {
			calls.push({ url, init });
			const next = replies.shift();
			if (!next) throw new Error(`unexpected request to ${url}`);
			return next;
		})
	);
});
afterEach(() => vi.unstubAllGlobals());

const header = (c: Call, name: string) => new Headers(c.init.headers).get(name);

describe('api', () => {
	test('a GET sends no CSRF token and returns the body', async () => {
		replies.push(json(200, { ok: 1 }));
		expect(await api.get('/api/me')).toEqual({ ok: 1 });
		expect(calls).toHaveLength(1);
		expect(header(calls[0], 'X-CSRF-Token')).toBeNull();
	});

	test('a CSV reply comes back as text, not parsed as JSON', async () => {
		const csv = 'code,total\nAB12,200.00\n';
		replies.push(new Response(csv, { status: 200, headers: { 'Content-Type': 'text/csv; charset=utf-8' } }));
		expect(await api.get<string>('/api/reports/reservations.csv')).toBe(csv);
	});

	test('a POST fetches the CSRF token once and sends it every time', async () => {
		replies.push(json(200, { token: 'abc' }), json(204, null), json(204, null));
		await api.post('/api/sign-out');
		await api.post('/api/sign-out');
		expect(calls.map((c) => c.url)).toEqual(['/api/csrf', '/api/sign-out', '/api/sign-out']);
		expect(header(calls[1], 'X-CSRF-Token')).toBe('abc');
		expect(header(calls[2], 'X-CSRF-Token')).toBe('abc');
	});

	test('a POST sends the body as JSON', async () => {
		replies.push(json(200, { token: 't' }), json(200, { next: 'code' }));
		expect(await api.post('/api/sign-in', { email: 'a@b.c' })).toEqual({ next: 'code' });
		expect(calls[1].init.body).toBe('{"email":"a@b.c"}');
		expect(header(calls[1], 'Content-Type')).toBe('application/json');
	});

	test('PATCH and PUT use their methods', async () => {
		replies.push(json(200, { token: 't' }), json(204, null), json(204, null));
		await api.patch('/api/staff/1', { role: 'manager' });
		await api.put('/api/notification-preferences', {});
		expect(calls[1].init.method).toBe('PATCH');
		expect(calls[2].init.method).toBe('PUT');
	});

	test('DELETE sends the CSRF token and no body', async () => {
		replies.push(json(200, { token: 't' }), json(204, null));
		await api.delete('/api/staff-invites/1');
		expect(calls[1].init.method).toBe('DELETE');
		expect(header(calls[1], 'X-CSRF-Token')).toBe('t');
		expect(calls[1].init.body).toBeUndefined();
	});

	test('the chosen property goes along as X-Property-Id', async () => {
		setProperty('prop-1');
		replies.push(json(200, []));
		await api.get('/api/staff');
		expect(header(calls[0], 'X-Property-Id')).toBe('prop-1');
		setProperty(null);
		replies.push(json(200, []));
		await api.get('/api/staff');
		expect(header(calls[1], 'X-Property-Id')).toBeNull();
	});

	test('an error response throws an ApiError with status, code and message', async () => {
		replies.push(json(401, { error: { code: 'invalid_credentials', message: 'The email or password is not right.' } }));
		const err = await api.get('/api/me').catch((e) => e);
		expect(err).toBeInstanceOf(ApiError);
		expect(err).toMatchObject({ status: 401, code: 'invalid_credentials', message: 'The email or password is not right.' });
	});

	test('a response that is not JSON still throws an ApiError', async () => {
		replies.push(new Response('<html>bad gateway</html>', { status: 502 }));
		const err = await api.get('/api/me').catch((e) => e);
		expect(err).toMatchObject({ status: 502, code: 'error' });
	});

	test('a rejected CSRF token is renewed once and the request retried', async () => {
		replies.push(
			json(200, { token: 'old' }),
			json(403, { error: { code: 'csrf', message: 'x' } }),
			json(200, { token: 'new' }),
			json(204, null)
		);
		await api.post('/api/sign-out');
		expect(header(calls[3], 'X-CSRF-Token')).toBe('new');
	});

	test('a second CSRF rejection is reported, not retried forever', async () => {
		replies.push(json(200, { token: 'a' }), json(403, { error: { code: 'csrf', message: 'x' } }), json(200, { token: 'b' }), json(403, { error: { code: 'csrf', message: 'x' } }));
		await expect(api.post('/api/sign-out')).rejects.toMatchObject({ code: 'csrf' });
		expect(calls).toHaveLength(4);
	});

	test('errorText prefers the screen copy for a known code and falls back to the server message', () => {
		expect(errorText(new ApiError(401, 'invalid_credentials', 'server words'))).toBe('The email or password is not right.');
		expect(errorText(new ApiError(400, 'something_new', 'server words'))).toBe('server words');
		expect(errorText(new Error('boom'))).toBe('Something failed on our side. Try again.');
	});
});

describe('an expired session', () => {
	test('a 401 calls the handler once, then still throws', async () => {
		const seen: string[] = [];
		onUnauthorized((path) => seen.push(path));
		replies.push(json(401, { error: { code: 'unauthenticated', message: 'Sign in to continue.' } }));
		await expect(api.get('/api/staff')).rejects.toMatchObject({ status: 401 });
		expect(seen).toEqual(['/api/staff']);
		onUnauthorized(null);
	});

	test('a failed sign-in is a 401 too but does not trigger the handler', async () => {
		const seen: string[] = [];
		onUnauthorized((path) => seen.push(path));
		replies.push(json(200, { token: 't' }), json(401, { error: { code: 'invalid_credentials', message: 'x' } }));
		await expect(api.post('/api/sign-in', {})).rejects.toMatchObject({ code: 'invalid_credentials' });
		expect(seen).toEqual([]);
		onUnauthorized(null);
	});
});
