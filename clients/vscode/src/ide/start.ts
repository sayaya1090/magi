import * as vscode from 'vscode';
import { found } from '../core/binary';
import { OwnedCompanion } from '../core/lifecycle';
export { found } from '../core/binary';

export const NO_BINARY = process.platform === 'win32'
  ? 'magi.exe is not installed. Put it on PATH, then run "magi: Start the companion for this workspace".'
  : 'magi is not installed. Put it on PATH, then run "magi: Start the companion for this workspace".';

/** What a person is told when the core they have cannot be owned by this window. */
export const UNOWNED_CORE =
  'This magi is too old for magi to tie the companion to this window (no owned-daemon-v1). '
  + 'It was started anyway and will be stopped when the window closes — but if the extension host '
  + 'is killed, the companion will keep running. Update magi to get the stronger guarantee.';

/**
 * What a person is told when this window could not take an owner pipe of its own.
 *
 * The core HAS the owned mode here — this is the other half: the pipe whose write end belongs to
 * this window rather than to `child_process`. Without it the companion is on the lifetime R2 fixed,
 * where an update or a restart ends it on Windows while the window is still open. It is started
 * anyway, because refusing over a pipe name somebody else took would hand that person the outage —
 * but what it now costs is said, with the step that failed, so the sentence is actionable rather
 * than ominous.
 *
 * The advice is to reopen rather than to press Restart: pressing Restart is the very thing this
 * fallback cannot survive, and each fresh start draws a new random pipe name, so the next one will
 * almost certainly hold.
 */
export const UNHELD_PIPE = (why: string): string =>
  'magi could not take its own owner pipe for this companion — ' + why + '. '
  + 'It was started anyway and this window will still stop it on close, but updating or restarting '
  + 'it will END it, and the window will have to start a fresh one a few seconds later. '
  + 'Close and reopen this window to try again on a new pipe name.';

export async function offerToStart(owner: OwnedCompanion): Promise<void> {
  if (!vscode.workspace.getConfiguration('magi').get<boolean>('startCompanion', true)) return;
  const bin = found();
  if (!bin) return;
  await owner.start(bin);
  // ⚠ **Said out loud, once.** docs/CLIENT_LIFECYCLE §4: never fall back silently. Starting a
  // companion this window cannot own is not a failure — it is the lifetime that existed before the
  // owned mode — but it is a weaker one, and a person who is never told cannot ask why their
  // companion survived a crash.
  if (owner.unowned()) void vscode.window.showWarningMessage(UNOWNED_CORE);
  // The other weaker lifetime, and the one that is NOT about the core's age: the window has the
  // owned mode and could not take a pipe of its own. Same rule, same once-per-start (issue #189).
  const unpiped = owner.unpiped();
  if (unpiped) void vscode.window.showWarningMessage(UNHELD_PIPE(unpiped));
}
