import { existsSync } from 'node:fs';
import { pathToFileURL, fileURLToPath } from 'node:url';

export const REQUIRED_BUNDLES = [
  'chat_html.js',
  'answer_state.js',
  'chat_adapter.bundle.js',
  // The view: without it the page draws its markup and never wires a single control.
  'chat_view.bundle.js',
];

/**
 * Normalizes a directory path or URL to a URL ending with a slash.
 * Properly converts Windows paths (e.g. C:\...), spaces, and '#' using pathToFileURL (§2.3).
 *
 * @param {string | URL} dirPathOrUrl
 * @returns {URL}
 */
export function toDirectoryUrl(dirPathOrUrl) {
  if (dirPathOrUrl instanceof URL) {
    return dirPathOrUrl.href.endsWith('/') ? dirPathOrUrl : new URL(`${dirPathOrUrl.href}/`);
  }
  if (typeof dirPathOrUrl === 'string' && dirPathOrUrl.startsWith('file:')) {
    return dirPathOrUrl.endsWith('/') ? new URL(dirPathOrUrl) : new URL(`${dirPathOrUrl}/`);
  }
  // Windows drive letter check on any platform (e.g. C:\... or C:/...)
  if (typeof dirPathOrUrl === 'string' && /^[a-zA-Z]:[/\\]/.test(dirPathOrUrl)) {
    const normalized = dirPathOrUrl.replace(/\\/g, '/');
    const encoded = encodeURI(normalized).replace(/#/g, '%23');
    const withSlash = encoded.endsWith('/') ? encoded : `${encoded}/`;
    return new URL(`file:///${withSlash}`);
  }
  const fileUrl = pathToFileURL(dirPathOrUrl);
  return fileUrl.href.endsWith('/') ? fileUrl : new URL(`${fileUrl.href}/`);
}

/**
 * Resolves the URL for a bundle relative to a base directory.
 * @param {string} bundleName
 * @param {string | URL} baseDirInput
 * @returns {{ url: URL, filePath: string }}
 */
export function resolveBundleUrl(bundleName, baseDirInput) {
  const baseDirUrl = toDirectoryUrl(baseDirInput);
  const bundleUrl = new URL(bundleName, baseDirUrl);
  return {
    url: bundleUrl,
    filePath: fileURLToPath(bundleUrl),
  };
}

/**
 * Inspects whether all required webview bundles exist under the given directory.
 * Returns structured result with missing bundle name and formatted error message.
 *
 * @param {string | URL} [baseDirInput]
 * @returns {{ ok: true } | { ok: false, missingBundle: string, missingPath: string, errorMessage: string }}
 */
export function checkRequiredBundles(baseDirInput = new URL('../out/web/', import.meta.url)) {
  for (const bundleName of REQUIRED_BUNDLES) {
    const { url, filePath } = resolveBundleUrl(bundleName, baseDirInput);
    if (!existsSync(url)) {
      return {
        ok: false,
        missingBundle: bundleName,
        missingPath: filePath,
        errorMessage: `Missing required webview asset bundle: ${filePath}\nRun 'npm run build --prefix clients/vscode' first.`,
      };
    }
  }
  return { ok: true };
}

/**
 * Runs preflight bundle check. If a bundle is missing, logs error and invokes exit(1).
 *
 * @param {string | URL} [baseDirInput]
 * @param {object} [options]
 * @param {(code: number) => void} [options.exit]
 * @param {(msg: string) => void} [options.logError]
 * @returns {boolean}
 */
export function runPreflight(baseDirInput, { exit = (code) => process.exit(code), logError = console.error } = {}) {
  const result = checkRequiredBundles(baseDirInput);
  if (!result.ok) {
    logError(result.errorMessage);
    exit(1);
    return false;
  }
  return true;
}

// Standalone CLI invocation: node tools/asset-preflight.mjs [--dir=<dir>]
const isMain = process.argv[1] && (
  import.meta.url === pathToFileURL(process.argv[1]).href ||
  import.meta.url.endsWith(process.argv[1])
);

if (isMain) {
  let targetDir = undefined;
  for (const arg of process.argv.slice(2)) {
    if (arg.startsWith('--dir=')) {
      targetDir = arg.slice('--dir='.length);
    }
  }
  runPreflight(targetDir);
}
