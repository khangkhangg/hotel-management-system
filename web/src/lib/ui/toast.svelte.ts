export type Toast = { id: number; message: string; undo?: { label: string; run: () => void } };

// Matches --duration-toast in tokens.css (5000ms). The timer lives here because CSS cannot dismiss a message.
const TOAST_MS = 5000;
const MOST = 3;

class Toasts {
	items = $state<Toast[]>([]);
	private next = 1;
	private timers = new Map<number, ReturnType<typeof setTimeout>>();

	show(message: string, options: { ms?: number; undo?: Toast['undo'] } = {}) {
		const id = this.next++;
		this.items = [...this.items, { id, message, undo: options.undo }].slice(-MOST);
		this.timers.set(id, setTimeout(() => this.dismiss(id), options.ms ?? TOAST_MS));
	}

	dismiss(id: number) {
		clearTimeout(this.timers.get(id));
		this.timers.delete(id);
		this.items = this.items.filter((t) => t.id !== id);
	}

	clear() {
		for (const id of [...this.timers.keys()]) this.dismiss(id);
	}
}

export const toasts = new Toasts();
