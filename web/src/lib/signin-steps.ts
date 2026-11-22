import { returnTo } from './draft.ts';

// Small things the sign-in steps hand to each other. They live in sessionStorage, not the address bar,
// so an email address never ends up in a URL or the browser history.
export type OrganizationChoice = { id: string; name: string };

const read = (key: string) => {
	try {
		return sessionStorage.getItem(key);
	} catch {
		return null; // storage can be blocked; the screens still work without it
	}
};
const write = (key: string, value: string | null) => {
	try {
		if (value === null) sessionStorage.removeItem(key);
		else sessionStorage.setItem(key, value);
	} catch {
		/* storage blocked */
	}
};

export const pendingEmail = {
	get: () => read('signin-email'),
	set: (email: string | null) => write('signin-email', email)
};

export const organizationChoices = {
	get: (): OrganizationChoice[] => {
		try {
			return JSON.parse(read('signin-orgs') ?? '[]');
		} catch {
			return [];
		}
	},
	set: (list: OrganizationChoice[] | null) => write('signin-orgs', list ? JSON.stringify(list) : null)
};

/** The response of a step that may finish sign-in, or ask for an organization. */
export type NextStep = { next: 'code' | 'done' | 'choose_organization' | 'sign_in'; organizations?: OrganizationChoice[] };

/** Where to go after a sign-in step answered with `step`. */
export function destination(step: NextStep): string {
	if (step.next === 'code') return '/sign-in/code';
	if (step.next === 'sign_in') return '/sign-in';
	if (step.next === 'choose_organization') {
		organizationChoices.set(step.organizations ?? []);
		return '/sign-in/organization';
	}
	return returnTo.take() ?? '/';
}
