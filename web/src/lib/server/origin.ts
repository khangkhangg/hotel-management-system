// Where the Go API listens, as the server of the web app sees it. Browsers talk to the same address through the web server.
export const apiOrigin = (): string => (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env.API_ORIGIN ?? 'http://127.0.0.1:8080';
