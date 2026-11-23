import { has, t } from './i18n.ts';

export class ApiError extends Error {
	constructor(
		public status: number,
		public code: string,
		message: string
	) {
		super(message);
	}
}

let csrf: string | null = null;
let property: string | null = null;
let unauthorized: ((path: string) => void) | null = null;

/** Called with the path of any request that finds the session gone (401), except sign-in itself. */
export function onUnauthorized(handler: ((path: string) => void) | null) {
	unauthorized = handler;
}

/** Forget the cached CSRF token and property. For tests and for signing out. */
export function resetApi() {
	csrf = null;
	property = null;
}

/** The property the next requests are about, sent as X-Property-Id. */
export function setProperty(id: string | null) {
	property = id;
}

async function csrfToken(): Promise<string> {
	if (!csrf) {
		const res = await fetch('/api/csrf', { credentials: 'same-origin' });
		csrf = ((await res.json()) as { token: string }).token;
	}
	return csrf;
}

async function request<T>(method: string, path: string, body?: unknown, retried = false): Promise<T> {
	const headers: Record<string, string> = { Accept: 'application/json' };
	if (method !== 'GET') {
		headers['X-CSRF-Token'] = await csrfToken();
		if (body !== undefined) headers['Content-Type'] = 'application/json';
	}
	if (property) headers['X-Property-Id'] = property;
	const res = await fetch(path, {
		method,
		headers,
		credentials: 'same-origin',
		body: body === undefined ? undefined : JSON.stringify(body)
	});
	if (res.ok) {
		if (res.headers.get('Content-Type')?.startsWith('text/csv')) return (await res.text()) as T; // an export is a file, not JSON
		return (res.status === 204 ? undefined : await res.json()) as T;
	}

	const data = (await res.json().catch(() => null)) as { error?: { code?: string; message?: string } } | null;
	const code = data?.error?.code ?? 'error';
	if (code === 'csrf' && !retried) {
		csrf = null; // the cookie was renewed or lost; fetch a fresh token and try once more
		return request<T>(method, path, body, true);
	}
	if (res.status === 401 && !path.startsWith('/api/sign-in')) unauthorized?.(path);
	throw new ApiError(res.status, code, data?.error?.message ?? res.statusText);
}

export const api = {
	get: <T>(path: string) => request<T>('GET', path),
	post: <T>(path: string, body?: unknown) => request<T>('POST', path, body ?? {}),
	put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body ?? {}),
	patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body ?? {}),
	delete: <T>(path: string) => request<T>('DELETE', path)
};

/** Words for an error: the screen's own copy for a known code, else the server's message. */
export function errorText(e: unknown): string {
	if (e instanceof ApiError) return has(`error.${e.code}`) ? t(`error.${e.code}`) : e.message;
	return t('error.unknown');
}
