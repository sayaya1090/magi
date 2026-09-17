import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import {
  checkRequiredBundles,
  toDirectoryUrl,
} from '../asset-preflight.mjs';

// Single source of truth for asset routing and URL resolution (§2.1, §5.8.6)
export const TEST_ORIGIN = 'http://magi.test';

export const ASSET_PATHS = {
  document: ['/', '/index.html'],
  answerState: '/out/web/answer_state.js',
  adapterBundle: '/out/web/chat_adapter.bundle.js',
};

export const ASSET_URLS = {
  document: `${TEST_ORIGIN}/`,
  answerState: `${TEST_ORIGIN}${ASSET_PATHS.answerState}`,
  adapterBundle: `${TEST_ORIGIN}${ASSET_PATHS.adapterBundle}`,
};

export const DEFAULT_WEB_OUT_DIR = new URL('../../out/web/', import.meta.url);

/**
 * Prepares the chat webview HTML strictly AFTER asset preflight succeeds (§2.2, §5.8.6).
 * Throws an Error with missing bundle path and build instructions if preflight fails.
 * Never hides bundle missing errors with empty HTML, automatic build, or process.exit.
 *
 * @param {object} [options]
 * @param {string} [options.origin]
 * @param {object} [options.assetUrls]
 * @param {string} [options.nonce]
 * @param {string | URL} [options.baseDir]
 * @returns {Promise<string>}
 */
export async function prepareChatHtml(options = {}) {
  const {
    origin = TEST_ORIGIN,
    assetUrls = ASSET_URLS,
    nonce = 'test-nonce',
    baseDir = process.env.MAGI_WEB_OUT_DIR || DEFAULT_WEB_OUT_DIR,
  } = options;

  // 1. Preflight check runs first: throw structured Error without exiting worker process (§5.8.6 Item 1)
  const preflightResult = checkRequiredBundles(baseDir);
  if (!preflightResult.ok) {
    throw new Error(preflightResult.errorMessage);
  }

  // 2. Dynamic import evaluated strictly AFTER preflight check succeeds
  const chatHtmlUrl = new URL('chat_html.js', toDirectoryUrl(baseDir));
  const { renderChatHtml } = await import(chatHtmlUrl.href);

  return renderChatHtml({
    cspSource: `'self' ${origin}`,
    nonce,
    scriptUri: assetUrls.answerState,
    adapterUri: assetUrls.adapterBundle,
  });
}

/**
 * Installs acquireVsCodeApi mock without postMessage auto-patching (§2.1, §2.3, §5.8.6).
 *
 * @param {import('playwright').Page} page
 */
export async function installAcquireVsCodeApi(page) {
  await page.addInitScript(() => {
    window.__posted = [];
    window.acquireVsCodeApi = () => ({
      postMessage(m) { window.__posted.push(m); },
      getState() {},
      setState() {}
    });
  });
}

/**
 * Attaches listeners for page errors, console errors, and unregistered request recording (§5.8.6).
 *
 * @param {import('playwright').Page} page
 * @returns {{
 *   errors: string[],
 *   routeErrors: string[],
 *   handleUnregisteredRequest: (url: string) => void,
 *   detach: () => void
 * }}
 */
export function attachErrorCollectors(page) {
  const errors = [];
  const routeErrors = [];

  const onPageError = (e) => {
    const msg = e && (e.message || e.stack) ? (e.message || String(e)) : String(e);
    errors.push(msg);
  };
  const onConsole = (m) => {
    if (m.type() === 'error') {
      errors.push(m.text());
    }
  };

  page.on('pageerror', onPageError);
  page.on('console', onConsole);

  const handleUnregisteredRequest = (url) => {
    routeErrors.push(`Unregistered asset requested: ${url}`);
  };

  const detach = () => {
    page.off('pageerror', onPageError);
    page.off('console', onConsole);
  };

  return {
    errors,
    routeErrors,
    handleUnregisteredRequest,
    detach,
  };
}

/**
 * Installs exact asset routing on a Playwright page (§2.1, §5.8.6).
 * Serves compiled HTML and local webview bundles; rejects and logs all other requests.
 *
 * @param {import('playwright').Page} page
 * @param {object} options
 * @param {string} options.html Compiled HTML content for root/document requests.
 * @param {(url: string) => void} [options.onUnregistered] Callback invoked on rejected requests.
 * @param {string | URL} [options.baseDir] Directory containing compiled web assets.
 */
export async function installAssetRouter(page, options = {}) {
  const {
    html,
    onUnregistered,
    baseDir = process.env.MAGI_WEB_OUT_DIR || DEFAULT_WEB_OUT_DIR,
  } = options;

  const baseDirUrl = toDirectoryUrl(baseDir);

  await page.route('**', async (route) => {
    const rawUrl = route.request().url();
    let reqUrl;
    try {
      reqUrl = new URL(rawUrl);
    } catch {
      if (onUnregistered) {
        onUnregistered(rawUrl);
      }
      return route.fulfill({ status: 404, contentType: 'text/plain; charset=utf-8', body: 'Invalid URL' });
    }

    if (reqUrl.origin === TEST_ORIGIN) {
      if (ASSET_PATHS.document.includes(reqUrl.pathname)) {
        return route.fulfill({ contentType: 'text/html; charset=utf-8', body: html });
      }
      if (reqUrl.pathname === ASSET_PATHS.answerState) {
        const jsUrl = new URL('answer_state.js', baseDirUrl);
        const js = await readFile(fileURLToPath(jsUrl), 'utf8');
        return route.fulfill({ contentType: 'application/javascript; charset=utf-8', body: js });
      }
      if (reqUrl.pathname === ASSET_PATHS.adapterBundle) {
        const jsUrl = new URL('chat_adapter.bundle.js', baseDirUrl);
        const js = await readFile(fileURLToPath(jsUrl), 'utf8');
        return route.fulfill({ contentType: 'application/javascript; charset=utf-8', body: js });
      }
    }

    // Any external origin or unregistered path on TEST_ORIGIN is blocked & recorded
    if (onUnregistered) {
      onUnregistered(rawUrl);
    }
    return route.fulfill({ status: 404, contentType: 'text/plain; charset=utf-8', body: 'Blocked or Unregistered Request' });
  });
}
