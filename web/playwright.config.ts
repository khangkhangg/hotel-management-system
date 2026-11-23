import { defineConfig } from '@playwright/test';

// The browser tests run the real API (api/cmd/e2e: a throwaway database and a fake mail sender) and the dev server.
// They use the Edge or Chrome that is already installed, so nothing is downloaded.
export default defineConfig({
	testDir: 'e2e',
	globalSetup: './e2e/warmup.ts',
	workers: 1,
	reporter: 'list',
	use: { baseURL: 'http://127.0.0.1:5173', channel: process.env.E2E_BROWSER ?? 'msedge' },
	webServer: [
		{
			command: 'go run ./cmd/e2e',
			cwd: '../api',
			url: 'http://127.0.0.1:8080/api/health',
			timeout: 180_000,
			reuseExistingServer: false
		},
		{
			command: 'npm run dev -- --host 127.0.0.1 --port 5173 --strictPort',
			url: 'http://127.0.0.1:5173/sign-in',
			timeout: 120_000,
			reuseExistingServer: false
		}
	]
});
