import * as vscode from 'vscode';
import { Companion } from './workspace';
import { Status } from './status';
import { Chat } from './chat';
import { found, start, NO_BINARY, offerToStart } from './start';

export function activate(ctx: vscode.ExtensionContext): void {
  const folder = vscode.workspace.workspaceFolders?.[0];
  if (!folder) return; // No folder, no workspace, no companion. Nothing to draw and nothing to say.
  const workdir = folder.uri.fsPath;

  const companion = new Companion(workdir);
  const status = new Status();
  const chat = new Chat(companion, ctx.extensionUri);

  ctx.subscriptions.push(
    companion, status, chat,
    companion.onChanged((a) => status.draw(a)),
    vscode.window.registerWebviewViewProvider(Chat.viewId, chat, {
      // The conversation is worth keeping when the panel is minimised — which the guidelines say
      // happens often. Rebuilding it on every reveal would re-stream the whole transcript.
      webviewOptions: { retainContextWhenHidden: true },
    }),
    vscode.commands.registerCommand('magi.focusChat', () => chat.reveal()),
    vscode.commands.registerCommand('magi.start', () => {
      const bin = found();
      if (!bin) { void vscode.window.showWarningMessage(NO_BINARY); return; }
      start(bin, workdir);
    }),
    vscode.commands.registerCommand('magi.interrupt', () => void companion.ask('interrupt')),
  );

  // Start one if none is listening and the person left that on. Silent either way: a window that
  // opened is not a moment worth a notification, and the guidelines are firm that the bar for one
  // is "absolutely necessary".
  void offerToStart(workdir);
  companion.watch();
}

export function deactivate(): void { /* everything is on ctx.subscriptions */ }
