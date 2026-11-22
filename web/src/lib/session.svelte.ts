import { api, resetApi, setProperty } from './api.ts';
import type { Role } from './nav.ts';

export type Me = {
	user: { id: string; name: string; email: string };
	organization: { id: string; name: string };
	memberships: { propertyId: string; propertyName: string; role: Role }[];
	viewAs: string;
};
export type Property = { id: string; name: string };

const remembered = (key: string): string | null => {
	try {
		return localStorage.getItem(key);
	} catch {
		return null;
	}
};
const remember = (key: string, value: string) => {
	try {
		localStorage.setItem(key, value);
	} catch {
		/* storage blocked: the choice lasts until the page closes */
	}
};

class SessionStore {
	current = $state<Me | null>(null);
	properties = $state<Property[]>([]);
	activeProperty = $state<string | null>(null);

	/** The person's real role: owner if they own the organization, else their role at the active property. */
	get role(): Role | null {
		const memberships = this.current?.memberships ?? [];
		if (memberships.some((m) => m.role === 'owner')) return 'owner';
		return (memberships.find((m) => m.propertyId === this.activeProperty) ?? memberships[0])?.role ?? null;
	}

	/** The role whose screens are shown: the one an owner is viewing as, otherwise their own. */
	get effectiveRole(): Role | null {
		return (this.current?.viewAs as Role) || this.role;
	}

	get viewingAs(): Role | null {
		return (this.current?.viewAs as Role) || null;
	}

	get propertyName(): string {
		return this.properties.find((p) => p.id === this.activeProperty)?.name ?? this.current?.organization.name ?? '';
	}

	async load() {
		const me = await api.get<Me>('/api/me');
		const properties = await api.get<Property[]>('/api/properties');
		const saved = remembered(`property:${me.user.id}`);
		this.current = me;
		this.properties = properties;
		this.selectProperty(properties.find((p) => p.id === saved)?.id ?? properties[0]?.id ?? null);
	}

	selectProperty(id: string | null) {
		this.activeProperty = id;
		setProperty(id);
		if (id && this.current) remember(`property:${this.current.user.id}`, id);
	}

	/** Owners only: look at the app as another role. An empty role goes back to the owner view. */
	async setViewAs(role: Role | '') {
		await api.post('/api/view-as', { role });
		if (this.current) this.current.viewAs = role;
	}

	clear() {
		this.current = null;
		this.properties = [];
		this.activeProperty = null;
		resetApi();
	}
}

export const session = new SessionStore();
