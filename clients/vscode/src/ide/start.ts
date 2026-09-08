import * as vscode from 'vscode';
import * as cp from 'child_process';
import * as fs from 'fs';
import * as path from 'path';
import { configDir } from '../core/workspace';

/**
 * Finding the magi binary, and starting a companion with it.
 *
 * Stage 1 does NOT download anything. The design has a whole section on doing that across windows
 * without asking three times, and it turns on a lock file that is worth building on its own rather
 * than as a corner of this one. Until then: use what is on PATH or already fetched, and when there
 * is neither, say so with the command that installs it.
 */

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
 * Start the companion for this workspace, detached.
 *
 * Detached and with its streams let go: a daemon tied to the extension host dies when the window
 * closes, and the whole point is that it outlives the window the way the terminal one does.
 */
export function start(bin: string, workdir: string): void {
  const child = cp.spawn(bin, ['--daemon', '--detach'], {
    cwd: workdir, detached: true, stdio: 'ignore', env: process.env,
  });
  child.unref();
}

/** What to tell somebody who has no binary. One sentence, and the way to fix it. */
export const NO_BINARY =
  'magi is not installed. Install it (curl -fsSL https://raw.githubusercontent.com/sayaya1090/magi/main/scripts/install.sh | sh) ' +
  'or put it on your PATH, then run "magi: Start the companion for this workspace".';

export async function offerToStart(workdir: string): Promise<boolean> {
  if (!vscode.workspace.getConfiguration('magi').get<boolean>('startCompanion', true)) return false;
  const bin = found();
  if (!bin) return false;
  start(bin, workdir);
  return true;
}
