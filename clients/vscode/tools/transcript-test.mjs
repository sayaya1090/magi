#!/usr/bin/env node
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import { spawn } from 'node:child_process';

const ALLOWED_BUNDLES = new Set(['layout', 'asks', 'autocomplete', 'diff', 'a11y', 'markdown']);

function printUsageAndExit(message) {
  console.error(`Error: ${message}`);
  console.error('\nSupported options:');
  console.error('  --bundle=<name>   Run single bundle (layout, asks, autocomplete, diff, a11y, markdown)');
  console.error('  --reverse         Run bundles in reverse order');
  console.error('  --reverse-themes  Audit accessibility themes in reverse order');
  console.error('  --verify-assets   Run only asset preflight and route checks');
  process.exit(1);
}

// Parse and strictly validate CLI arguments (§5.8.6)
const rawArgs = process.argv.slice(2);
let selectedBundleName = null;
let isReverse = false;
let isReverseThemes = false;
let verifyAssetsOnly = false;

for (const arg of rawArgs) {
  if (!arg || typeof arg !== 'string') {
    printUsageAndExit('Empty argument provided.');
  }

  if (arg === '--reverse') {
    isReverse = true;
  } else if (arg === '--reverse-themes') {
    isReverseThemes = true;
  } else if (arg === '--verify-assets') {
    verifyAssetsOnly = true;
  } else if (arg.startsWith('--bundle=')) {
    const bundleName = arg.slice('--bundle='.length).trim();
    if (!bundleName) {
      printUsageAndExit('Missing bundle name in --bundle=<name>.');
    }
    if (!ALLOWED_BUNDLES.has(bundleName)) {
      printUsageAndExit(`Unknown bundle "${bundleName}". Allowed bundles: ${Array.from(ALLOWED_BUNDLES).join(', ')}`);
    }
    selectedBundleName = bundleName;
  } else {
    printUsageAndExit(`Unknown or unsupported option "${arg}".`);
  }
}

// Resolve Playwright CLI installed in clients/web/e2e (§5.8.6)
const webE2eRequire = createRequire(new URL('../../web/e2e/package.json', import.meta.url));
const playwrightCliPath = webE2eRequire.resolve('@playwright/test/cli');
const configPath = fileURLToPath(new URL('./transcript/playwright.config.mjs', import.meta.url));

const childEnv = {
  ...process.env,
  ...(verifyAssetsOnly ? { MAGI_TRANSCRIPT_VERIFY_ASSETS_ONLY: '1' } : {}),
  ...(selectedBundleName ? { MAGI_TRANSCRIPT_BUNDLE: selectedBundleName } : {}),
  ...(isReverse ? { MAGI_TRANSCRIPT_REVERSE: '1' } : {}),
  ...(isReverseThemes ? { MAGI_TRANSCRIPT_REVERSE_THEMES: '1' } : {}),
};

const child = spawn(
  process.execPath,
  [playwrightCliPath, 'test', '-c', configPath],
  {
    stdio: 'inherit',
    env: childEnv,
    shell: false,
  }
);

child.on('error', (err) => {
  console.error('Failed to spawn Playwright runner:', err);
  process.exit(1);
});

child.on('close', (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
  } else {
    process.exit(code ?? 1);
  }
});
