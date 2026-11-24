type Task = { roomNumber: string; arrivalToday: boolean; status: string };

/** Open rooms with a guest arriving today first, then the rest by room number, finished rooms last. */
export const cleaningOrder = <T extends Task>(tasks: T[]): T[] =>
	[...tasks].sort((a, b) => Number(a.status === 'done') - Number(b.status === 'done') || Number(b.arrivalToday) - Number(a.arrivalToday) || a.roomNumber.localeCompare(b.roomNumber, undefined, { numeric: true }));
