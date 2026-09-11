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
}
