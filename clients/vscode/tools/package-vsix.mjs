import { readFile, unlink, stat } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import path from 'node:path';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { fileURLToPath } from 'node:url';
import { verifyVsixArchive } from './verify-vsix.mjs';

const execFileAsync = promisify(execFile);

/**
 * How to run `npx` without a shell.
 *
 * ⚠ **`execFile('npx', …)` cannot work on Windows, and neither can the obvious fix.** Measured on
 * this machine (Windows 11, Node 24.19):
 *
 *   execFile('npx', …)      → ENOENT   — npx is `npx.cmd`, and execFile/spawn do not consult
 *                                         PATHEXT, so the name resolves to nothing.
 *   execFile('npx.cmd', …)  → EINVAL   — Node refuses to spawn `.cmd`/`.bat` without `shell: true`
 *                                         (the CVE-2024-27980 fix). So appending the extension,
 *                                         which is what one tries next, fails differently.
 *
 * That left `npm run package` unable to build a VSIX on Windows at all, and four §5.8.5 tests red
 * with `spawn npx ENOENT`.
 *
 * `shell: true` would work and is the third thing one tries — and it is the wrong one here. With a
 * shell, Node joins argv into one command line WITHOUT quoting, so any path containing a space is
 * torn apart; this very tool exists to pass `-o`/`--out` paths, and its own tests cover a target
 * directory with spaces. Trading ENOENT for a quoting bug is not a fix.
 *
 * So the launcher is run the way any other JS CLI is: with the node binary we are already inside.
 * `npx-cli.js` ships beside npm in every standard install, and running it needs no shell, no
 * extension guessing, and no argv flattening. If it is somewhere this does not find it, the plain
 * name is used — that is exactly the old behaviour, and it still works everywhere it used to.
 */
function npxCommand() {
  const bundled = path.join(path.dirname(process.execPath), 'node_modules', 'npm', 'bin', 'npx-cli.js');
  return existsSync(bundled) ? [process.execPath, bundled] : ['npx'];
}

const OPTIONS_WITH_VALUE = new Set([
  '-o', '--out',
  '-t', '--target',
  '--readme-path',
  '--changelog-path',
  '-m', '--message',
  '--githubBranch',
  '--gitlabBranch',
  '--baseContentUrl',
  '--baseImagesUrl',
  '--ignoreFile',
  '--sign-tool',
]);

/**
 * Resolves packaging target file path and options from raw command-line arguments.
 *
 * @param {string[]} rawArgs
 * @param {object} [context]
 * @param {string} [context.rootDir]
 * @param {{ name: string, version: string }} [context.pkg]
 * @returns {Promise<{
 *   targetVsix: string,
 *   targetArch: string | null,
 *   outPath: string | null,
 *   versionOverride: string | null,
 *   pkgVersion: string,
 *   defaultFileName: string
 * }>}
 */
export async function resolvePackageConfig(rawArgs = [], context = {}) {
  const rootDir = context.rootDir
    ? path.resolve(context.rootDir)
    : path.resolve(fileURLToPath(import.meta.url), '..', '..');

  let pkg = context.pkg;
  if (!pkg) {
    const pkgText = await readFile(path.join(rootDir, 'package.json'), 'utf8');
    pkg = JSON.parse(pkgText);
  }

  let targetArch = null;
  let outPath = null;
  let versionOverride = null;
  const otherTokens = [];

  for (let i = 0; i < rawArgs.length; i++) {
    const arg = rawArgs[i];
    if (arg === '-o' || arg === '--out') {
      if (i + 1 >= rawArgs.length) {
        throw new Error(`Missing argument for option: ${arg}`);
      }
      outPath = rawArgs[++i];
    } else if (arg.startsWith('--out=')) {
      outPath = arg.slice('--out='.length);
      if (!outPath) {
        throw new Error('Missing argument for option: --out');
      }
    } else if (arg.startsWith('-o=')) {
      outPath = arg.slice('-o='.length);
      if (!outPath) {
        throw new Error('Missing argument for option: -o');
      }
    } else if (arg === '-t' || arg === '--target') {
      if (i + 1 >= rawArgs.length) {
        throw new Error(`Missing argument for option: ${arg}`);
      }
      targetArch = rawArgs[++i];
    } else if (arg.startsWith('--target=')) {
      targetArch = arg.slice('--target='.length);
      if (!targetArch) {
        throw new Error('Missing argument for option: --target');
      }
    } else if (arg.startsWith('-t=')) {
      targetArch = arg.slice('-t='.length);
      if (!targetArch) {
        throw new Error('Missing argument for option: -t');
      }
    } else if (OPTIONS_WITH_VALUE.has(arg)) {
      if (i + 1 >= rawArgs.length) {
        throw new Error(`Missing argument for option: ${arg}`);
      }
      otherTokens.push(arg, rawArgs[++i]);
    } else {
      if (!arg.startsWith('-')) {
        // Positional version argument
        versionOverride = arg;
      }
      otherTokens.push(arg);
    }
  }

  const pkgVersion = versionOverride || pkg.version;
  const defaultFileName = targetArch
    ? `${pkg.name}-${targetArch}-${pkgVersion}.vsix`
    : `${pkg.name}-${pkgVersion}.vsix`;

  let targetVsix;
  if (!outPath) {
    targetVsix = path.resolve(rootDir, defaultFileName);
  } else {
    const resolvedOut = path.resolve(rootDir, outPath);
    let isDir = false;
    try {
      const st = await stat(resolvedOut);
      isDir = st.isDirectory();
    } catch (err) {
      if (err.code !== 'ENOENT') throw err;
    }

    if (isDir) {
      targetVsix = path.join(resolvedOut, defaultFileName);
    } else {
      targetVsix = resolvedOut;
    }
  }

  const normalizedArgs = [...otherTokens];
  if (targetArch !== null) {
    normalizedArgs.push('--target', targetArch);
  }
  if (outPath !== null) {
    normalizedArgs.push('--out', outPath);
  }

  return {
    targetVsix,
    targetArch,
    outPath,
    versionOverride,
    pkgVersion,
    defaultFileName,
    normalizedArgs,
  };
}

/**
 * Executes vsce package with specified arguments and immediately verifies the generated archive.
 *
 * @param {string[]} [rawArgs]
 * @param {object} [options]
 * @param {string} [options.rootDir]
 * @param {{ name: string, version: string }} [options.pkg]
 * @param {Function} [options.execRunner] Custom exec function (defaults to child_process.execFile).
 * @param {Function} [options.verifyFn] Custom verification function (defaults to verifyVsixArchive).
 * @param {Function} [options.onStdout]
 * @param {Function} [options.onStderr]
 * @returns {Promise<{ version: string, fileCount: number, sizeBytes: number, vsixPath: string, vsceArgs: string[], normalizedArgs: string[] }>}
 */
export async function packageVsix(rawArgs = [], options = {}) {
  const rootDir = options.rootDir
    ? path.resolve(options.rootDir)
    : path.resolve(fileURLToPath(import.meta.url), '..', '..');

  let pkg = options.pkg;
  if (!pkg) {
    const pkgText = await readFile(path.join(rootDir, 'package.json'), 'utf8');
    pkg = JSON.parse(pkgText);
  }

  const config = await resolvePackageConfig(rawArgs, { rootDir, pkg });
  const { targetVsix, pkgVersion, normalizedArgs } = config;

  // Stale artifact removal: only unlink if target is an existing regular file.
  // Never unlink an existing directory!
  try {
    const st = await stat(targetVsix);
    if (st.isFile()) {
      await unlink(targetVsix);
    }
  } catch (err) {
    if (err.code !== 'ENOENT') throw err;
  }

  console.log(`Packaging VS Code extension ${pkg.name}@${pkgVersion} -> ${targetVsix}...`);

  const vsceArgs = ['package', '--no-dependencies', ...normalizedArgs];
  const execAsync = options.execRunner || execFileAsync;
  const [npxBin, ...npxPrefix] = npxCommand();

  try {
    const { stdout, stderr } = await execAsync(npxBin, [...npxPrefix, '--yes', '@vscode/vsce', ...vsceArgs], {
      cwd: rootDir,
      env: process.env,
    });
    if (stdout && stdout.trim()) {
      if (options.onStdout) options.onStdout(stdout);
      else console.log(stdout.trim());
    }
    if (stderr && stderr.trim()) {
      if (options.onStderr) options.onStderr(stderr);
      else console.warn(stderr.trim());
    }
  } catch (err) {
    const msg = `vsce package failed with code ${err.code}: ${err.stderr || err.message}`;
    console.error(msg);
    const error = new Error(msg);
    error.code = err.code || 1;
    throw error;
  }

  if (!existsSync(targetVsix)) {
    const msg = `Packaging failed: expected output file was not created: ${targetVsix}`;
    console.error(msg);
    const error = new Error(msg);
    error.code = 1;
    throw error;
  }

  console.log(`Verifying packaged VSIX archive immediately: ${targetVsix}...`);
  const verifyFn = options.verifyFn || verifyVsixArchive;
  const res = await verifyFn(targetVsix, { expectedVersion: pkgVersion, rootDir });
  return {
    ...res,
    targetVsix,
    vsceArgs,
    normalizedArgs,
  };
}

// Direct CLI invocation
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const rawArgs = process.argv.slice(2);
  packageVsix(rawArgs)
    .then((res) => {
      console.log(`✓ Package and verification SUCCESS: ${res.vsixPath}`);
      console.log(`  Version: ${res.version}, Size: ${res.sizeBytes} bytes, Files: ${res.fileCount}`);
    })
    .catch((err) => {
      console.error(`✗ Packaging or verification FAILED: ${err.message}`);
      process.exit(err.code || 1);
    });
}
