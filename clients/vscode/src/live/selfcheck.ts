import * as vscode from 'vscode';
import { socketPath, socketThere } from '../core/workspace';

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
  say(!!views['magi-side']?.some((v) => v.id === 'magi.plan'), 'the plan is not in the magi-side container');
  // WHERE that container hangs, not merely that it exists. The move to the Secondary Side Bar is a
  // one-word change in the manifest, and a build that silently went back to the activity bar would
  // pass every check above it — the views and their ids do not change.
  const where = (ext?.packageJSON?.contributes?.viewsContainers ?? {}) as Record<string, { id: string }[]>;
  say(!!where['secondarySidebar']?.some((c) => c.id === 'magi-side'),
    `the plan container is not in the secondary sidebar (found: ${Object.keys(where).join(', ')})`);

  // Settings are real settings, not a webview.
  //
  // ⚠ **No count here.** This said "expected four settings" and there were six by 2026-09-10 — a
  // sentence that ages, which is the failure this repository keeps paying for. What only a running
  // editor can tell us is that the settings reached it at all; whether the declared set matches the
  // set the code reads is measured exactly, in `manifest.test.ts`, against the source. Repeating a
  // number here would be a second copy of a fact that already has an owner.
  const props = Object.keys(ext?.packageJSON?.contributes?.configuration?.properties ?? {});
  say(props.length > 0, 'the running extension exposes no settings at all');

  // The panel opens. This is the one that catches a view whose provider throws on resolve.
  try {
    await vscode.commands.executeCommand('magi.chat.focus');
  } catch (e) {
    fail.push(`the conversation view would not open: ${(e as Error).message}`);
  }

  // The editor's own tools reach the companion. This is the one that catches a hand that starts and
  // is refused, or a daemon too old to take one — neither of which any unit test can see.
  try {
    const { Hand } = await import('../core/mcpserver');
    const { handTools } = await import('../core/hand');
    const probe = await Hand.start({
      show: async () => 'x', replace: async () => 'x', problems: async () => 'x',
    });
    try {
      const r = await fetch(probe.url, {
        method: 'POST',
        headers: { 'content-type': 'application/json', ...probe.headers },
        body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/list' }),
      });
      const body = await r.json() as { result?: { tools?: unknown[] } };
      say((body.result?.tools ?? []).length === handTools().length,
        'the editor hand does not serve its tools over HTTP');
    } finally { probe.close(); }
  } catch (e) {
    fail.push(`the editor hand would not start: ${(e as Error).message}`);
  }

  // And the socket this window computes is the one the daemon actually made.
  //
  // ⚠ **Two Windows-only defects hid behind this one line**, and only a real editor could show
  // either (both measured 2026-09-12, Windows 11): the fsPath VS Code hands over spells the drive
  // letter lowercase and the key is a hash of the string, so the window looked for a different
  // socket than its daemon made; and `fs.existsSync` — what this asked with — answers false about a
  // live AF_UNIX socket on Windows, because it stats and Windows refuses to stat one. Either alone
  // makes every window on the platform say "not running" about a workspace that has a companion.
  const dir = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath ?? '';
  const p = socketPath(dir);
  say(socketThere(p), `no daemon socket at the path this window computes: ${p}`);

  return fail;
}
