import * as vscode from 'vscode';
import { found } from '../core/binary';
import { OwnedCompanion } from '../core/lifecycle';
export { found } from '../core/binary';

export const NO_BINARY = process.platform === 'win32'
  ? 'magi.exe is not installed. Put it on PATH, then run "magi: Start the companion for this workspace".'
  : 'magi is not installed. Put it on PATH, then run "magi: Start the companion for this workspace".';

export async function offerToStart(owner: OwnedCompanion): Promise<void> {
  if (!vscode.workspace.getConfiguration('magi').get<boolean>('startCompanion', true)) return;
  const bin = found();
  if (bin) await owner.start(bin);
}
