import * as fs from 'fs';
import * as path from 'path';

/**
 * **Which build of the core, from where.** The values come from a configuration file, not from code
 * — somebody moving the repository or pointing at an internal mirror has to be able to swap that one
 * file without touching TypeScript.
 *
 * ⚠ **The file is `clients/contract/core-release.properties`, and the JetBrains plugin reads the same
 * one.** A copy per editor is a fact stored twice, and this tree has already paid for that shape more
 * than once — most recently a council preview that was wrong in three independent shapers. Two
 * editors quietly fetching different cores would be the same defect wearing a different hat.
 *
 * The asset naming is the one the core release ALREADY uses (goreleaser's names, the ones the
 * self-update reads). Inventing a second convention here would create the same kind of pair.
 *
 * Pure functions only — no network, no disk beyond reading that file. That is what makes it testable.
 */
export class CoreRelease {
  constructor(private readonly conf: Record<string, string>) {}

  get version(): string { return this.conf['core.version'] ?? ''; }

  /**
   * Fetch without checking the certificate. **Off by default**, and the place to turn it on is the
   * configuration file.
   *
   * An internal mirror on a private CA is a real situation, so there is a road. What it costs is
   * stated plainly: the checksums arrive over the same channel, so whoever is in the middle can
   * change BOTH the file and its checksum. A sha256 then says "the transfer was not corrupted" and
   * cannot say "this is what they published".
   */
  get insecure(): boolean {
    return ['true', 'yes', '1', 'on'].includes((this.conf['core.insecure'] ?? '').trim().toLowerCase());
  }

  /**
   * Whether what arrives gets checked. The other face of {@link insecure}, **named separately on
   * purpose**: a caller needs to ask "is this verified" rather than "is the certificate checked".
   *
   * Insecure turns verification OFF ENTIRELY rather than weakening it. A weak check is worse than
   * none — "checksum verified" in a log reads as a guarantee — and a corrupt archive fails at the
   * unpacking step anyway.
   */
  get verifies(): boolean { return !this.insecure; }

  /**
   * Is the configuration actually loaded. **Not loaded and "no build for this machine" are different
   * facts**; conflated, a person whose resource file failed to load is told it is their machine.
   */
  get configured(): boolean {
    return ['core.version', 'core.asset', 'core.url', 'core.checksums']
      .every((k) => (this.conf[k] ?? '').trim() !== '');
  }

  /** Whether to go looking for something newer than {@link version} before fetching. */
  get tracksLatest(): boolean { return (this.conf['core.track'] ?? '').trim().toLowerCase() === 'latest'; }

  /** Where the core publishes its own version as one line of text, if it does. */
  latestUrl(): string | null {
    const u = this.conf['core.latest'];
    return u && u.startsWith('https://') ? u : null;
  }

  /** That one line, as a version — or null when the body is anything else (a 404 page, say). */
  readLatest(body: string): string | null {
    for (const raw of body.split('\n')) {
      const line = raw.trim().replace(/^v/, '');
      if (line === '') continue;
      return /^\d+(?:\.\d+)*$/.test(line) ? line : null;
    }
    return null;
  }

  releasesUrl(): string | null {
    const u = this.conf['core.releases'];
    return u && u.startsWith('https://') ? u : null;
  }

  tagPattern(): string { return (this.conf['core.tag'] ?? '').trim() || String.raw`^v(\d+(?:\.\d+)*)$`; }

  /** The same configuration pointed at another version. */
  at(version: string): CoreRelease { return new CoreRelease({ ...this.conf, 'core.version': version }); }

  /**
   * The asset name for this machine, or **null** for a combination we do not know. Better to say
   * "there is no build for this machine" than to invent a name, collect a 404, and call it a network
   * error.
   */
  asset(platform: string = process.platform, arch: string = process.arch): string | null {
    const os = osOf(platform);
    const a = archOf(arch);
    if (!os || !a) return null;
    // Windows alone ships zip (goreleaser's default) — the OS decides the extension.
    const ext = os === 'windows' ? 'zip' : 'tar.gz';
    const name = (this.conf['core.asset'] ?? '')
      .replace('{os}', os).replace('{arch}', a).replace('{ext}', ext);
    // A leftover placeholder means the name was INVENTED. Guarding only the URL and not this left
    // the two paths at different strictness, and an invented name ends as a quiet 404.
    return name !== '' && !name.includes('{') ? name : null;
  }

  url(asset: string): string | null { return this.fill(this.conf['core.url'], asset); }
  checksumsUrl(): string | null { return this.fill(this.conf['core.checksums'], ''); }

  private fill(template: string | undefined, asset: string): string | null {
    if (this.version.trim() === '') return null; // an empty version is a quiet `…/download/v/…` 404
    if (!template || template.trim() === '') return null;
    const u = template.replace('{version}', this.version).replace('{asset}', asset);
    if (u.includes('{')) return null;
    // **https only.** This is the road an executable arrives on, so the scheme is a rule rather than
    // a property of today's configuration — testing only that the shipped value is https leaves the
    // person who swaps that value untested.
    return u.startsWith('https://') ? u : null;
  }

  /** The host a download comes from. A consent dialog has to be able to say WHERE. */
  host(asset: string): string | null {
    const u = this.url(asset);
    if (!u) return null;
    try { return new URL(u).host; } catch { return null; }
  }

  /**
   * Do the file and its checksum come from the **same origin**? A checksum is the root of the trust,
   * and a root that arrives from somewhere else is not a check.
   */
  sameOrigin(asset: string): boolean {
    const a = this.host(asset);
    const sums = this.checksumsUrl();
    if (!a || !sums) return false;
    try { return a === new URL(sums).host; } catch { return false; }
  }

  /** The executable's name inside what was fetched. Windows alone gets `.exe`. */
  binaryName(platform: string = process.platform): string {
    return osOf(platform) === 'windows' ? 'magi.exe' : 'magi';
  }
}

/**
 * `checksums.txt` as name → sha256. **The only ground for believing what arrived is what was
 * published**, so a malformed line is dropped rather than turning the whole table empty — an empty
 * table looks like "verification skipped", and a caller has to treat a missing entry as "cannot
 * fetch this".
 */
export function checksums(text: string): Map<string, string> {
  const out = new Map<string, string>();
  for (const line of text.split('\n')) {
    const parts = line.trim().split(/\s+/);
    if (parts.length === 2 && parts[0].length === 64) out.set(parts[1].replace(/^\*/, ''), parts[0]);
  }
  return out;
}

/** The newest version in a GitHub releases listing whose tag matches `pattern`, or null. */
export function pickLatest(releasesJson: string, pattern: string): string | null {
  let re: RegExp;
  try { re = new RegExp(pattern); } catch { return null; }
  let best: string | null = null;
  for (const m of releasesJson.matchAll(/"tag_name"\s*:\s*"([^"]+)"/g)) {
    const got = re.exec(m[1]);
    if (!got) continue;
    const v = got[1] ?? '';
    if (!/^\d+(?:\.\d+)*$/.test(v)) continue;
    if (best === null || newer(v, best)) best = v;
  }
  return best;
}

/** Is a newer than b, comparing numerically part by part rather than as strings ("10" > "9"). */
export function newer(a: string, b: string): boolean {
  const x = a.split('.').map((n) => Number(n) || 0);
  const y = b.split('.').map((n) => Number(n) || 0);
  for (let i = 0; i < Math.max(x.length, y.length); i++) {
    const d = (x[i] ?? 0) - (y[i] ?? 0);
    if (d !== 0) return d > 0;
  }
  return false;
}

/**
 * The shipped configuration, read from the shared contract file.
 *
 * Java `.properties`: `key=value`, `#` and `!` comments, and — the one that matters here — a
 * backslash escapes the next character, so `^v(\\d+)` in the file is `^v(\d+)` once loaded. Getting
 * that wrong would silently produce a regex that matches nothing, and "no release matched" looks
 * exactly like "no release exists".
 */
export function shipped(file = contractFile()): Record<string, string> {
  const out: Record<string, string> = {};
  let text: string;
  try { text = fs.readFileSync(file, 'utf8'); } catch { return out; }
  for (const line of text.split('\n')) {
    const s = line.trim();
    if (s === '' || s.startsWith('#') || s.startsWith('!')) continue;
    const eq = s.indexOf('=');
    if (eq <= 0) continue;
    out[s.slice(0, eq).trim()] = unescapeProperties(s.slice(eq + 1).trim());
  }
  return out;
}

function unescapeProperties(v: string): string {
  let out = '';
  for (let i = 0; i < v.length; i++) {
    if (v[i] !== '\\' || i + 1 >= v.length) { out += v[i]; continue; }
    const c = v[++i];
    out += c === 'n' ? '\n' : c === 't' ? '\t' : c === 'r' ? '\r' : c;
  }
  return out;
}

/**
 * Where that file is. Beside the compiled extension when packaged, and up in the repository when
 * running from source — the same two-place lookup the tests exercise.
 */
export function contractFile(): string {
  const beside = path.join(__dirname, '..', '..', 'core-release.properties');
  if (fs.existsSync(beside)) return beside;
  return path.join(__dirname, '..', '..', '..', 'contract', 'core-release.properties');
}

function osOf(platform: string): string | null {
  switch (platform) {
    case 'darwin': return 'darwin';
    case 'win32': return 'windows';
    case 'linux': return 'linux';
    default: return null;
  }
}

function archOf(arch: string): string | null {
  switch (arch) {
    case 'arm64': return 'arm64';
    case 'x64': return 'amd64';
    default: return null;
  }
}
