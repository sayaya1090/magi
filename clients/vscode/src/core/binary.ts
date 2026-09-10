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

