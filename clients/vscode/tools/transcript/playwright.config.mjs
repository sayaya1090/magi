import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { createRequire } from 'node:module';

const require = createRequire(new URL('../../../web/e2e/package.json', import.meta.url));
const { defineConfig } = require('@playwright/test');

const currentDir = path.dirname(fileURLToPath(import.meta.url));

const testMatch = process.env.PLAYWRIGHT_TEST_MATCH
  ? new RegExp(process.env.PLAYWRIGHT_TEST_MATCH)
  : /transcript\.spec\.mjs$/;

const maxFailures = process.env.PLAYWRIGHT_MAX_FAILURES !== undefined
  ? parseInt(process.env.PLAYWRIGHT_MAX_FAILURES, 10)
  : 1;

export default defineConfig({
  testDir: currentDir,
  testMatch,
  maxFailures,
  workers: 1,
  fullyParallel: false,
  retries: 0,
  outputDir: path.join(currentDir, 'artifacts'),
  use: {
    headless: true,
    viewport: { width: 420, height: 600 },
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  reporter: [
    ['list'],
  ],
});
