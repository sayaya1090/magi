import { createHash } from 'crypto';
import { execFile } from 'child_process';
import * as fs from 'fs';
import * as https from 'https';
import * as os from 'os';
import * as path from 'path';
import { CoreRelease, checksums, pickLatest } from './release';

/**
 * Everything this file needs from the network, as two methods — so the fetching can be measured
 * without one.
 *
 * A test that mocks `https` mocks the library; one that implements this implements the CONTRACT, and
 * the difference shows up the first time the real one is wrong in a way the mock cannot be.
 */
export interface Wire {
  /** A small document — the checksums table, a version line. */
  text(url: string): Promise<string>;
  /** A file, streamed to disk. Big enough that holding it in memory is a choice, not a default. */
  file(url: string, dest: string): Promise<void>;
}

/** How long a single request may take before it is a failure rather than a slow network. */
export const requestTimeoutMs = 60_000;

/**
 * The real wire.
 *
 * ⚠ **`insecure` turns off certificate checking for these requests, and the caller has already been
 * told what that costs** (see `CoreRelease.insecure`): the checksums come over the same connection,
 * so whoever is in the middle can change both. Plain http is refused regardless — a private CA is a
 * circumstance, plaintext is not — and that refusal lives in `CoreRelease.url`, which never returns
 * anything but https.
 */
export function wire(insecure = false): Wire {
  const get = (url: string, onResponse: (res: import('http').IncomingMessage) => void, reject: (e: Error) => void, depth = 0) => {
    if (depth > 5) return reject(new Error(`too many redirects fetching ${url}`));
    if (!url.startsWith('https://')) return reject(new Error(`refusing to fetch over plain http: ${url}`));
    const req = https.get(url, {
      rejectUnauthorized: !insecure,
      headers: { 'user-agent': 'magi-vscode', accept: '*/*' },
      timeout: requestTimeoutMs,
    }, (res) => {
      const code = res.statusCode ?? 0;
      if (code >= 300 && code < 400 && res.headers.location) {
        res.resume();
        // Relative redirects are ordinary on release hosts; resolve against the URL we asked.
        return get(new URL(res.headers.location, url).toString(), onResponse, reject, depth + 1);
      }
      if (code !== 200) { res.resume(); return reject(new Error(`${url} answered ${code}`)); }
      onResponse(res);
    });
    req.on('timeout', () => req.destroy(new Error(`${url} did not answer within ${requestTimeoutMs}ms`)));
    req.on('error', reject);
  };
  return {
    text: (url) => new Promise((resolve, reject) => {
      get(url, (res) => {
        let body = '';
        res.setEncoding('utf8');
        res.on('data', (c) => { body += c; });
        res.on('end', () => resolve(body));
        res.on('error', reject);
      }, reject);
    }),
    file: (url, dest) => new Promise((resolve, reject) => {
      get(url, (res) => {
        const out = fs.createWriteStream(dest);
        res.pipe(out);
        out.on('finish', () => out.close((e) => (e ? reject(e) : resolve())));
        out.on('error', reject);
        res.on('error', reject);
      }, reject);
    }),
  };
}

/** Where a fetched core is kept: one directory per version, beside whatever a sibling client fetched. */
export function cachedAt(configDir: string, version: string, platform = process.platform): string {
  return path.join(configDir, 'bin', version, platform === 'win32' ? 'magi.exe' : 'magi');
}

/**
 * Fetch the core described by `release` and leave it in the per-version cache, returning its path.
 *
 * The order is the contract, and each step's failure is a different sentence: a machine with no build
 * is not an unreachable host, and a checksum that does not match is not a corrupt archive. Anything
 * that fails leaves the previous state alone — nothing is written outside a temporary directory until
 * the binary has been verified and extracted.
 */
export async function fetchCore(
  release: CoreRelease,
  configDir: string,
  net: Wire = wire(release.insecure),
  platform = process.platform,
  arch = process.arch,
): Promise<string> {
  if (!release.configured) {
    throw new Error('this build does not say where to fetch the core from (core-release.properties did not load)');
  }
  const asset = release.asset(platform, arch);
  if (!asset) throw new Error(`there is no magi build for ${platform}/${arch}`);
  const url = release.url(asset);
  const sumsUrl = release.checksumsUrl();
  if (!url || !sumsUrl) throw new Error('the configured download address is not usable (it must be https and fully filled in)');

  let want: string | null = null;
  if (release.verifies) {
    // The checksums have to come from where the file comes from, or the check checks nothing.
    if (!release.sameOrigin(asset)) {
      throw new Error(`the checksums come from ${new URL(sumsUrl).host} and the file from ${release.host(asset)} — that is not a check`);
    }
    const table = checksums(await net.text(sumsUrl));
    want = table.get(asset) ?? null;
    if (!want) throw new Error(`${asset} is not listed in checksums.txt — refusing to install something nobody vouched for`);
  }

  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-core-'));
  try {
    const archive = path.join(tmp, asset);
    await net.file(url, archive);
    if (want) {
      const got = await sha256(archive);
      if (got.toLowerCase() !== want.toLowerCase()) {
        throw new Error(`${asset} does not match its published checksum (${got} vs ${want})`);
      }
    }
    const out = path.join(tmp, 'out');
    fs.mkdirSync(out);
    await unpack(archive, out);
    const name = release.binaryName(platform);
    const found = findFile(out, name);
    if (!found) throw new Error(`the archive did not contain ${name}`);
    const dest = cachedAt(configDir, release.version, platform);
    fs.mkdirSync(path.dirname(dest), { recursive: true });
    fs.renameSync(found, dest);
    fs.chmodSync(dest, 0o755);
    return dest;
  } finally {
    // Kept out of the way of a full disk: a 40MB archive per attempt adds up on a machine that keeps
    // failing, and the failure people then report is about space rather than about the download.
    fs.rmSync(tmp, { recursive: true, force: true });
  }
}

/** SHA-256 of a file, streamed — the archive is tens of megabytes and does not belong in memory. */
export function sha256(file: string): Promise<string> {
  return new Promise((resolve, reject) => {
    const h = createHash('sha256');
    const s = fs.createReadStream(file);
    s.on('data', (c) => h.update(c));
    s.on('end', () => resolve(h.digest('hex')));
    s.on('error', reject);
  });
}

/**
 * Extract an archive, with the system's own `tar`.
 *
 * ⚠ **Deliberately not a bundled parser.** This extension ships with no runtime dependencies, and
 * writing a tar reader and a zip reader to keep it that way would be a few hundred lines of security-
 * relevant code in a place nobody looks. `tar` is on macOS and Linux by definition, and on Windows
 * since 1803 (bsdtar, which reads zip too). Where it is not, PowerShell's Expand-Archive is.
 */
export function unpack(archive: string, into: string): Promise<void> {
  const zip = archive.endsWith('.zip');
  const args = zip ? ['-xf', archive, '-C', into] : ['-xzf', archive, '-C', into];
  return new Promise((resolve, reject) => {
    execFile('tar', args, { timeout: requestTimeoutMs }, (err) => {
      if (!err) return resolve();
      if (!zip) return reject(new Error(`could not unpack ${path.basename(archive)}: ${err.message}`));
      // Windows before 1803 has no tar. PowerShell has read zip since 5.0.
      execFile('powershell', ['-NoProfile', '-Command',
        `Expand-Archive -LiteralPath '${archive}' -DestinationPath '${into}' -Force`],
      { timeout: requestTimeoutMs }, (perr) => {
        if (perr) return reject(new Error(`could not unpack ${path.basename(archive)}: ${err.message}; ${perr.message}`));
        resolve();
      });
    });
  });
}

/** The first file with this name anywhere under root — release archives nest differently over time. */
export function findFile(root: string, name: string): string | null {
  for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
    const p = path.join(root, entry.name);
    if (entry.isDirectory()) {
      const deeper = findFile(p, name);
      if (deeper) return deeper;
    } else if (entry.name === name) return p;
  }
  return null;
}

/**
 * Which version to fetch, asked of the network in the order the configuration lays out.
 *
 * `core.latest` first — one line of text, no rate limit, no JSON, and above all **no lane to pick**:
 * this repository publishes three release trains (`v*`, `web-v*`, `jetbrains-v*`) and GitHub's
 * "latest" points at whichever is newest by date, which was `web-v0.2.0` on 2026-08-31 (with no core
 * asset, so a 404). The releases listing is the fallback, and the pinned version is the floor.
 *
 * Every failure falls through rather than stopping: being offline or rate-limited should cost the
 * newest build, not the installation. Pinned configurations skip all of it.
 */
export async function resolveLatest(r: CoreRelease, net: Wire): Promise<CoreRelease> {
  if (!r.tracksLatest) return r;
  const line = r.latestUrl();
  if (line) {
    try {
      const v = r.readLatest(await net.text(line));
      if (v) return r.at(v);
    } catch { /* fall through to the listing */ }
  }
  const listing = r.releasesUrl();
  if (!listing) return r;
  try {
    const v = pickLatest(await net.text(listing), r.tagPattern());
    // ⚠ **Whatever the listing says, not `max(listing, pinned)`** — deliberately the same as the
    // JetBrains side (`CoreBinary.resolve`). The two clients read one configuration file and should
    // not answer differently from it; a listing older than the floor would be a release having been
    // withdrawn, which is a situation to notice rather than to paper over on one client only.
    if (v) return r.at(v);
  } catch { /* the floor below */ }
  return r;
}
