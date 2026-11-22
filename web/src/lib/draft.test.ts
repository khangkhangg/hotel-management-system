import { beforeEach, describe, expect, test } from 'vitest';
import { drafts, returnTo } from './draft.ts';

beforeEach(() => sessionStorage.clear());

describe('returnTo', () => {
	test('remembers a path once', () => {
		returnTo.set('/staff');
		expect(returnTo.take()).toBe('/staff');
		expect(returnTo.take()).toBeNull();
	});

	test('does not remember the sign-in screens or other sites', () => {
		returnTo.set('/sign-in/code');
		expect(returnTo.take()).toBeNull();
		returnTo.set('//evil.example/path');
		expect(returnTo.take()).toBeNull();
		returnTo.set('https://evil.example');
		expect(returnTo.take()).toBeNull();
	});
});

describe('drafts', () => {
	test('saves what screens registered and gives it back once for that path', () => {
		const stop = drafts.register('invite', () => ({ email: 'new@hotel.com', open: true }));
		drafts.save('/staff');
		stop();
		expect(drafts.restore('/activity')).toEqual({});
		expect(drafts.restore('/staff')).toEqual({ invite: { email: 'new@hotel.com', open: true } });
		expect(drafts.restore('/staff')).toEqual({});
	});

	test('a screen that has gone away is no longer saved', () => {
		const stop = drafts.register('gone', () => ({ x: 1 }));
		stop();
		drafts.save('/staff');
		expect(drafts.restore('/staff')).toEqual({});
	});

	test('a getter that throws does not lose the other drafts', () => {
		const a = drafts.register('bad', () => {
			throw new Error('boom');
		});
		const b = drafts.register('good', () => ({ ok: true }));
		drafts.save('/staff');
		a();
		b();
		expect(drafts.restore('/staff')).toEqual({ good: { ok: true } });
	});
});
