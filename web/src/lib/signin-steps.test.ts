import { beforeEach, expect, test } from 'vitest';
import { returnTo } from './draft.ts';
import { destination } from './signin-steps.ts';

beforeEach(() => sessionStorage.clear());

test('after sign-in the person goes home, or back to where the session ran out', () => {
	expect(destination({ next: 'done' })).toBe('/');
	returnTo.set('/staff');
	expect(destination({ next: 'done' })).toBe('/staff');
	expect(destination({ next: 'done' })).toBe('/');
});

test('the code and sign-in steps lead to their own screens', () => {
	expect(destination({ next: 'code' })).toBe('/sign-in/code');
	expect(destination({ next: 'sign_in' })).toBe('/sign-in');
});

test('choosing an organization keeps the list for the next screen', () => {
	expect(destination({ next: 'choose_organization', organizations: [{ id: '1', name: 'A' }] })).toBe('/sign-in/organization');
	expect(sessionStorage.getItem('signin-orgs')).toContain('"A"');
});
