// Keeps work across an expired session: where the person was, and what they had typed.
// Everything lives in sessionStorage, so it goes when the tab closes.

function store<T>(fn: () => T, otherwise: T): T {
	try {
		return fn();
	} catch {
		return otherwise; // storage can be blocked
	}
}

/** True for a path inside this app that is worth returning to. */
const returnable = (path: string) =>
	path.startsWith('/') && !path.startsWith('//') && !['/sign-in', '/invite', '/reset'].some((p) => path.startsWith(p));

export const returnTo = {
	set(path: string) {
		if (returnable(path)) store(() => sessionStorage.setItem('return-to', path), undefined);
	},
	/** The remembered path, once. */
	take(): string | null {
		const path = store(() => sessionStorage.getItem('return-to'), null);
		store(() => sessionStorage.removeItem('return-to'), undefined);
		return path && returnable(path) ? path : null;
	}
};

const getters = new Map<string, () => unknown>();

export const drafts = {
	/** A screen says how to read its unsaved form values. Returns the function that withdraws it. */
	register(key: string, read: () => unknown): () => void {
		getters.set(key, read);
		return () => {
			if (getters.get(key) === read) getters.delete(key);
		};
	},
	/** Stores every registered screen's values for the path. */
	save(path: string) {
		const out: Record<string, unknown> = {};
		for (const [key, read] of getters) {
			try {
				out[key] = read();
			} catch {
				/* one broken screen must not lose the others */
			}
		}
		store(() => sessionStorage.setItem(`draft:${path}`, JSON.stringify(out)), undefined);
	},
	/** The stored values for the path, once. */
	restore(path: string): Record<string, unknown> {
		const raw = store(() => sessionStorage.getItem(`draft:${path}`), null);
		store(() => sessionStorage.removeItem(`draft:${path}`), undefined);
		try {
			return raw ? JSON.parse(raw) : {};
		} catch {
			return {};
		}
	}
};
