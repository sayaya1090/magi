import { readFile, unlink } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import path from 'node:path';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { fileURLToPath } from 'node:url';
import { verifyVsixArchive } from './verify-vsix.mjs';

const execFileAsync = promisify(execFile);

async function main() {
  const rootDir = path.resolve(fileURLToPath(import.meta.url), '..', '..');
  const pkgText = await readFile(path.join(rootDir, 'package.json'), 'utf8');
  const pkg = JSON.parse(pkgText);

  const rawArgs = process.argv.slice(2);
  let targetVsix = null;

  for (let i = 0; i < rawArgs.length; i++) {
    const arg = rawArgs[i];
    if (arg === '-o' || arg === '--out') {
      targetVsix = rawArgs[i + 1];
    } else if (arg.startsWith('--out=')) {
      targetVsix = arg.slice('--out='.length);
    }
  }

  if (!targetVsix) {
    targetVsix = path.join(rootDir, `${pkg.name}-${pkg.version}.vsix`);
  } else {
    targetVsix = path.resolve(rootDir, targetVsix);
  }

  // Ensure stale artifact from past packaging is removed prior to build
  if (existsSync(targetVsix)) {
    await unlink(targetVsix);
  }

  console.log(`Packaging VS Code extension ${pkg.name}@${pkg.version} -> ${targetVsix}...`);

  const vsceArgs = ['package', '--no-dependencies', ...rawArgs];

  try {
    const { stdout, stderr } = await execFileAsync('npx', ['--yes', '@vscode/vsce', ...vsceArgs], {
      cwd: rootDir,
      env: process.env,
    });
    if (stdout.trim()) console.log(stdout.trim());
    if (stderr.trim()) console.warn(stderr.trim());
  } catch (err) {
    console.error(`vsce package failed with code ${err.code}: ${err.stderr || err.message}`);
    process.exit(err.code || 1);
  }

  if (!existsSync(targetVsix)) {
    console.error(`Packaging failed: expected output file was not created: ${targetVsix}`);
    process.exit(1);
  }

  console.log(`Verifying packaged VSIX archive immediately: ${targetVsix}...`);
  try {
    const res = await verifyVsixArchive(targetVsix, { expectedVersion: pkg.version, rootDir });
    console.log(`✓ Package and verification SUCCESS: ${res.vsixPath}`);
    console.log(`  Version: ${res.version}, Size: ${res.sizeBytes} bytes, Files: ${res.fileCount}`);
  } catch (err) {
    console.error(`✗ Post-packaging verification FAILED: ${err.message}`);
    process.exit(1);
  }
}

main().catch((err) => {
  console.error('Unexpected error during packaging:', err);
  process.exit(1);
});
