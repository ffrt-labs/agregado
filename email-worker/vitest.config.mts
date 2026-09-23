import { defineConfig } from 'vitest/config';
import { cloudflareTest } from '@cloudflare/vitest-pool-workers';

export default defineConfig({
	test: {
		setupFiles: ['./test/apply-migrations.ts'],
	},
	plugins: [cloudflareTest({ wrangler: { configPath: './wrangler.jsonc' } })],
});
