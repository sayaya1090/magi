import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { createRequire } from 'node:module';

const require = createRequire(new URL('../../../web/e2e/package.json', import.meta.url));
const { defineConfig } = require('@playwright/test');

const currentDir = path.dirname(fileURLToPath(import.meta.url));

const testMatch = process.env.PLAYWRIGHT_TEST_MATCH
  ? new RegExp(process.env.PLAYWRIGHT_TEST_MATCH)
  : /markdown\.spec\.mjs$/;

export default defineConfig({
  testDir: currentDir,
  testMatch,
  workers: 1,
  fullyParallel: false,
  retries: 0,
  outputDir: path.join(currentDir, 'artifacts'),
  use: {
    headless: true,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  reporter: [
    ['list'],
  ],
});
