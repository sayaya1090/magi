import * as vscode from 'vscode';
import * as fs from 'fs';
import { socketPath } from '../core/workspace';

/**
 * What this extension can only find out from inside a running VS Code.
 *
 * The unit tests run in bare node and prove the rules; they cannot prove that the manifest
 * registers what it claims, that the views resolve, or that the commands are really there. A
 * manifest typo passes every one of them and then does nothing in the editor — silently, because
 * a view nobody registered simply does not appear.
 *
 * Run by `magi.selfCheck`, which the packaged extension exposes only when MAGI_VSCODE_SELFCHECK is
 * set, so it is not a command a person can trip over.
 */
export async function selfCheck(): Promise<string[]> {
  const fail: string[] = [];
  const say = (ok: boolean, what: string) => { if (!ok) fail.push(what); };

  const ext = vscode.extensions.getExtension('sayaya1090.magi');
  say(!!ext, 'the extension is not installed under sayaya1090.magi');
  if (!ext) return fail;
  // Wait for it rather than race it. The activation event is `onStartupFinished`, which lands
  // after this entry point runs — a check that read `isActive` here would fail on timing and say
  // "did not activate" about an extension that was about to.
  if (!ext.isActive) {
    try { await ext.activate(); } catch (e) { fail.push(`activate threw: ${(e as Error).message}`); }
  }
  say(ext.isActive, 'the extension did not activate');

  // Every command the manifest promises is really registered. A `contributes.commands` entry with
  // no registerCommand behind it shows in the palette and throws when pressed.
  const promised = (ext?.packageJSON?.contributes?.commands ?? []) as { command: string }[];
  say(promised.length >= 10, `the manifest promises only ${promised.length} commands`);
  const live = new Set(await vscode.commands.getCommands(true));
  for (const c of promised) say(live.has(c.command), `command promised and not registered: ${c.command}`);

  // The two views the design allows, and no more.
  const views = (ext?.packageJSON?.contributes?.views ?? {}) as Record<string, { id: string; type?: string }[]>;
  const all = Object.values(views).flat();
  say(all.length === 2, `expected two views, found ${all.length}`);
  say(all.every((v) => v.type === 'webview'), 'a view is not a webview');
  say(!!views['magi']?.some((v) => v.id === 'magi.chat'), 'the conversation is not in the panel container');
  say(!!views['magi-side']?.some((v) => v.id === 'magi.plan'), 'the plan is not in the activity bar container');

  // Settings are real settings, not a webview.
  const props = Object.keys(ext?.packageJSON?.contributes?.configuration?.properties ?? {});
  say(props.length >= 4, `expected four settings, found ${props.length}`);

  // The panel opens. This is the one that catches a view whose provider throws on resolve.
  try {
    await vscode.commands.executeCommand('magi.chat.focus');
  } catch (e) {
    fail.push(`the conversation view would not open: ${(e as Error).message}`);
  }

  // And the socket this window computes is the one the daemon actually made.
  const dir = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath ?? '';
  const p = socketPath(dir);
  say(fs.existsSync(p), `no daemon socket at the path this window computes: ${p}`);

  return fail;
}
