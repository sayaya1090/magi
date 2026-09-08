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
import { doorCommands } from './doors';
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
    companion.onSetup((s) => status.show(s)),

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
    ...doorCommands(companion, chat),

    vscode.commands.registerCommand('magi.focusChat', () => chat.reveal()),
    vscode.commands.registerCommand('magi.start', () => {
      const bin = found();
      if (!bin) { void vscode.window.showWarningMessage(NO_BINARY); return; }
      start(bin, workdir);
    }),
    vscode.commands.registerCommand('magi.interrupt', () => void companion.ask('interrupt')),
  );

  // Open the conversation by itself, if the person asked for that.
  //
  // Without this the panel is only ever reached by pressing the status bar, and a panel nobody
  // opened is a panel nobody reads — the guidelines say plainly that people minimise it. But
  // opening it in EVERY window would put a magi panel in projects that have never seen magi, so
  // `whenRunning` (the default) asks first whether this workspace actually has a companion.
  //
  // `preserveFocus` throughout: nothing the person did caused this, so it must not take the
  // keyboard out of the editor they were typing in.
  void openIfAsked(companion, chat);

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

/**
 * The `magi.openConversation` setting, applied once at startup.
 *
 * Read through a named function rather than inline so the three answers are in one place: a fourth
 * value added to the manifest and not here would silently mean `never`, which is the shape of
 * defect this tree keeps paying for — an enum with a home for every case except the new one.
 */
async function openIfAsked(companion: Companion, chat: Chat): Promise<void> {
  const how = vscode.workspace.getConfiguration('magi').get<string>('openConversation', 'whenRunning');
  if (how === 'never') return;
  if (how === 'whenRunning' && !(await companion.reachable())) return;
  chat.reveal(true);
}
