import * as vscode from 'vscode';
import { Ref, askLead } from '../core/refs';
import { Companion } from './workspace';
import { Chat } from './chat';
import { Looking } from './look';

/**
 * Buttons where the work already is.
 *
 * Every one of these exists in the JetBrains client; what changes is the surface VS Code gives.
 * The one that MOVES is "explain this output": its terminal is xterm, so a right-click there can
 * only reach a selection, never the line a stack trace is on. The place this editor puts that kind
 * of remark is a diagnostic, so the code action lives there instead. Moved, not imitated.
 */
export function entryPoints(companion: Companion, chat: Chat, looking: Looking): vscode.Disposable[] {
  /**
   * The editor these commands need, or a sentence saying why not.
   *
   * A command that returns quietly when nothing is open is the worst shape a command has: it is in
   * the palette, it is pressed, and nothing happens — which reads as broken rather than as
   * inapplicable. The `when` clauses in the manifest keep them out of the palette in the first
   * place; this is for the paths a keybinding can still reach.
   */
  const editorOr = (why: string): vscode.TextEditor | null => {
    const ed = vscode.window.activeTextEditor;
    if (!ed) { void vscode.window.showWarningMessage(`magi: ${why}`); return null; }
    return ed;
  };

  const refsOf = (ed: vscode.TextEditor): Ref[] => {
    const p = ed.document.uri.fsPath;
    return ed.selections.filter((s) => !s.isEmpty)
      .map((s) => ({ path: p, from: s.start.line + 1, to: s.end.line + 1 }));
  };

  const attach = async (refs: Ref[]) => {
    if (!refs.length) return;
    // Say so before the chips go up. Attaching to a companion that is not there looks like it
    // worked — the chips appear — and then the message goes nowhere.
    if (!(await companion.reachable())) {
      void vscode.window.showWarningMessage(
        'magi: no companion is running for this workspace.', 'Start one')
        .then((pick) => { if (pick) void vscode.commands.executeCommand('magi.start'); });
      return;
    }
    // Save first. The companion reads the disk, so attaching an unsaved buffer would point it at
    // text that is not there — the JetBrains client saves on attach for the same reason.
    await vscode.workspace.saveAll(false);
    chat.attach(refs);
    chat.reveal();
  };

  return [
    vscode.commands.registerCommand('magi.attach', async () => {
      const ed = editorOr('open a file to attach code from it.');
      if (!ed) return;
      const refs = refsOf(ed);
      await attach(refs.length ? refs : [{ path: ed.document.uri.fsPath }]);
    }),

    vscode.commands.registerCommand('magi.attachFiles', async (_one?: vscode.Uri, many?: vscode.Uri[]) => {
      const picked = (many ?? []).filter((u) => u.scheme === 'file');
      await attach(picked.map((u) => ({ path: u.fsPath })));
    }),

    vscode.commands.registerCommand('magi.askAbout', async () => {
      const ed = editorOr('open a file to ask about its code.');
      if (!ed) return;
      const refs = refsOf(ed);
      const use = refs.length ? refs : [{ path: ed.document.uri.fsPath, from: ed.selection.active.line + 1 }];
      await attach(use);
      // The lead only. The person types the question — a prompt written for them would be a guess
      // about what they wanted to ask.
      chat.compose(askLead(use));
    }),

    vscode.commands.registerCommand('magi.lookNow', async () => {
      const ed = editorOr('open a file for the companion to look over.');
      if (ed) await looking.now(ed);
    }),

    vscode.commands.registerCommand('magi.whoWrote', async () => {
      const ed = editorOr('open a file and put the cursor on the line you mean.');
      if (!ed) return;
      const line = ed.selection.active.line + 1;
      const said = chat.whoWrote(ed.document.uri.fsPath, line);
      void vscode.window.showInformationMessage(said);
    }),

    vscode.commands.registerCommand('magi.explainOutput', async (arg?: vscode.Uri | string) => {
      const text = await outputText(arg);
      if (!text) {
        void vscode.window.showWarningMessage('magi: select the output you want explained first.');
        return;
      }
      chat.compose(`What does this mean, and how do I fix it?\n\n${text}`);
      chat.reveal();
    }),

    vscode.commands.registerCommand('magi.draftCommit', async () => {
      const resp = await companion.ask('git-msg');
      if (!resp?.ok || !(resp.out ?? '').trim()) {
        void vscode.window.showWarningMessage(`magi: ${resp?.error ?? 'no commit message came back'}`);
        return;
      }
      const scm = vscode.extensions.getExtension<{ getAPI(v: number): GitLike }>('vscode.git');
      const api = scm?.isActive ? scm.exports.getAPI(1) : (await scm?.activate())?.getAPI(1);
      const repo = api?.repositories?.[0];
      if (!repo) { void vscode.window.showWarningMessage('magi: no git repository here.'); return; }
      // Never overwrite. If they started typing, the draft goes to a notification instead — the
      // box is theirs.
      if (repo.inputBox.value.trim()) void vscode.window.showInformationMessage(resp.out!.trim());
      else repo.inputBox.value = resp.out!.trim();
    }),

    // "Explain this error" where this editor keeps errors: on the diagnostic.
    vscode.languages.registerCodeActionsProvider({ pattern: '**' }, {
      provideCodeActions(doc, range, ctx) {
        const out: vscode.CodeAction[] = [];
        for (const d of ctx.diagnostics) {
          const a = new vscode.CodeAction(`magi: explain "${clip(d.message)}"`, vscode.CodeActionKind.QuickFix);
          a.command = { command: 'magi.explainOutput', title: 'explain', arguments: [d.message] };
          out.push(a);
        }
        const ask = new vscode.CodeAction('magi: ask about this code', vscode.CodeActionKind.Empty);
        ask.command = { command: 'magi.askAbout', title: 'ask' };
        out.push(ask);
        // Only where the file has nothing selected — with a selection, attaching is the useful one.
        if (range.isEmpty) {
          const look = new vscode.CodeAction('magi: look over this file', vscode.CodeActionKind.Empty);
          look.command = { command: 'magi.lookNow', title: 'look' };
          out.push(look);
        } else {
          const add = new vscode.CodeAction('magi: add this code to the conversation', vscode.CodeActionKind.Empty);
          add.command = { command: 'magi.attach', title: 'attach' };
          out.push(add);
        }
        void doc;
        return out;
      },
    }),
  ];
}

interface GitLike { repositories: { inputBox: { value: string } }[] }

function clip(s: string): string { return s.length > 40 ? s.slice(0, 40) + '…' : s; }

/** Whatever the person meant by "this output": a diagnostic's text, or the terminal selection. */
async function outputText(arg?: vscode.Uri | string): Promise<string> {
  if (typeof arg === 'string' && arg.trim()) return arg.trim();
  const term = vscode.window.activeTerminal;
  if (term) {
    await vscode.commands.executeCommand('workbench.action.terminal.copySelection');
    const sel = (await vscode.env.clipboard.readText()).trim();
    if (sel) return sel.slice(0, 64 * 1024); // the same 64KB ceiling the JetBrains client uses
  }
  const ed = vscode.window.activeTextEditor;
  if (ed && !ed.selection.isEmpty) return ed.document.getText(ed.selection);
  return '';
}
