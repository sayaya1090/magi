import * as vscode from 'vscode';
import * as fs from 'fs';
import { Companion } from './workspace';
import { Status } from './status';
import { Chat } from './chat';
import { Plan } from './plan';
import { Looking } from './look';
import { inlineCompletion } from './complete';
import { entryPoints } from './entrypoints';
import { chooseCommands } from './choose';
import { found, start, NO_BINARY, offerToStart } from './start';

export function activate(ctx: vscode.ExtensionContext): void {
  const folder = vscode.workspace.workspaceFolders?.[0];
  // No folder, no workspace, no companion. A window with nothing open has nothing to draw and
  // nothing honest to say, so it says nothing.
  if (!folder) return;
  const workdir = folder.uri.fsPath;

  const companion = new Companion(workdir);
  const status = new Status();
  const chat = new Chat(companion, ctx.extensionUri);
  const plan = new Plan(companion);
  const looking = new Looking(companion);

  ctx.subscriptions.push(
    companion, status, chat, plan, looking,
    companion.onChanged((a) => status.draw(a)),

    vscode.window.registerWebviewViewProvider(Chat.viewId, chat, {
      // Kept when hidden, because the guidelines say plainly that people minimise the panel, and
      // rebuilding on every reveal would re-stream the whole conversation.
      webviewOptions: { retainContextWhenHidden: true },
    }),
    vscode.window.registerWebviewViewProvider(Plan.viewId, plan, {
      webviewOptions: { retainContextWhenHidden: true },
    }),

    inlineCompletion(companion),
    ...entryPoints(companion, chat, looking),
    ...chooseCommands(companion, chat),

    vscode.commands.registerCommand('magi.focusChat', () => chat.reveal()),
    vscode.commands.registerCommand('magi.start', () => {
      const bin = found();
      if (!bin) { void vscode.window.showWarningMessage(NO_BINARY); return; }
      start(bin, workdir);
    }),
    vscode.commands.registerCommand('magi.interrupt', () => void companion.ask('interrupt')),
  );

  // Only when asked for by environment. It is a test surface, not a feature, and a command in the
  // palette that runs a self-check is a thing to press by accident.
  if (process.env.MAGI_VSCODE_SELFCHECK) {
    ctx.subscriptions.push(vscode.commands.registerCommand('magi.selfCheck', async () => {
      const { selfCheck } = await import('../live/selfcheck');
      const fail = await selfCheck();
      const out = fail.length ? 'FAIL\n' + fail.join('\n') : 'OK';
      fs.writeFileSync(process.env.MAGI_VSCODE_SELFCHECK!, out);
      return out;
    }));
  }

  // Silently. A window opening is not a moment worth a notification, and the bar the guidelines
  // set for one is "absolutely necessary".
  void offerToStart(workdir);
  companion.watch();
}

export function deactivate(): void { /* everything is on ctx.subscriptions */ }
