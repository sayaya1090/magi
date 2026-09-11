import { execFile } from 'child_process';
import * as fs from 'fs';
import * as path from 'path';
import { configDir } from './workspace';

/** The binary this account already has, or null. PATH first — an installed magi is that person's. */
export function found(): string | null {
  const name = process.platform === 'win32' ? 'magi.exe' : 'magi';
  // The PATH the person's shell knows, not this process's. A GUI app on macOS inherits a stunted
  // one, and the JetBrains plugin lost this branch entirely to that — it then fetched a second
  // copy of a binary that was already installed.
  for (const dir of (process.env.PATH ?? '').split(path.delimiter)) {
    if (!dir) continue;
    const p = path.join(dir, name);
    try { fs.accessSync(p, fs.constants.X_OK); return p; } catch { /* next */ }
  }
  // Then whatever a sibling client already fetched. ANY version, not the one we expect: the daemon
  // updates itself in place, so `bin/0.29.0/magi` is 0.30 a week later, and insisting on our own
  // number would re-fetch something already current.
  const bin = path.join(configDir(), 'bin');
  try {
    for (const v of fs.readdirSync(bin)) {
      const p = path.join(bin, v, name);
      try { fs.accessSync(p, fs.constants.X_OK); return p; } catch { /* next */ }
    }
  } catch { /* no bin directory yet */ }
  return null;
}


/**
 * What a particular `magi` can do, asked of that binary before it is trusted with anything.
 *
 * ⚠ **The answer is the exit code and the JSON, never the prose.** An older build has no such flag,
 * its flag package refuses the argument, and THAT is how a client learns "no features here" —
 * docs/CLIENT_LIFECYCLE §4 says it in as many words. Searching help text for a word is guessing, and
 * a guess here starts a mode the binary does not understand and reads the failure as a broken
 * install.
 *
 * Bounded at five seconds because the contract bounds it: the probe contacts no daemon and touches
 * no disk, so anything slower is a binary that is not going to answer.
 *
 * An empty set is the honest answer for every failure — refused flag, malformed line, timeout. All
 * of them mean the same thing to a caller: do not use the new modes with this binary.
 */
export async function features(binary: string, timeoutMs = 5_000): Promise<Set<string>> {
  return new Promise((resolve) => {
    execFile(binary, ['ide-bridge', '--features'], { timeout: timeoutMs }, (err, stdout) => {
      if (err) return resolve(new Set());
      try {
        const got = JSON.parse(String(stdout).split('\n')[0]);
        const list: unknown = got?.features;
        if (!Array.isArray(list)) return resolve(new Set());
        resolve(new Set(list.filter((x): x is string => typeof x === 'string')));
      } catch { resolve(new Set()); }
    });
  });
}

/**
 * Why this binary cannot relay a daemon socket, or null when it can.
 *
 * ⚠ **A function rather than an `if` inside the spawn**, so it can be measured. The first version
 * was the condition written inline, guarded by a test that read the source for the words — and a
 * mutation that kept every word while disabling the check (`… && false`) sailed through it. Text
 * cannot see intent; a function can be called.
 *
 * Windows only in practice: Node reads a unix socket path as a named pipe, so the extension cannot
 * dial AF_UNIX and goes through `magi ide-bridge --raw-socket`. A core older than that flag refuses
 * it and exits 2, and everything above then reports "nothing answered" about a daemon that is
 * perfectly fine — docs/CLIENT_LIFECYCLE §2 names this gap for exactly this transport.
 */
export function whyNoRelay(has: Set<string>, binary: string): string | null {
  if (has.has('raw-socket-v1')) return null;
  return `${binary} is too old to relay a daemon socket on Windows (no raw-socket-v1). Update magi.`;
}
