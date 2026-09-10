import * as vscode from 'vscode';
import { Ide, inside } from '../core/hand';
import { Hand, } from '../core/mcpserver';
import { HAND_NAME } from '../core/hand';
import { Companion } from './workspace';

/**
 * What this editor can actually do, and handing it to the companion.
 *
 * The point of every tool here is that it goes THROUGH the editor. `apply_edit` uses a
 * WorkspaceEdit, so the change lands in the undo stack as one step, the open buffer updates, and the
 * language server re-checks — none of which happens when a file is rewritten from outside.
 */
export class EditorHand implements Ide, vscode.Disposable {
  private server: Hand | null = null;
  private attached = false;

  constructor(private readonly companion: Companion, private readonly workdir: string) {}

  /**
   * Start the server and tell the companion about it.
   *
   * ⚠ Refusals are NOT swallowed. Opening one workspace in two windows means the FIRST window is
   * the hand and the second is refused — and a person needs to be able to find out why the agent
   * cannot drive their editor. It goes to the log rather than a popup: nothing is broken, and a
   * notification for it would fire on every second window.
   */
  async offer(): Promise<void> {
    if (!(await this.companion.caps()).has('tool-servers')) {
      this.why = 'this companion cannot take an editor hand (no tool-servers door)';
      return;
    }
    if (!this.server) {
      try {
        this.server = await Hand.start(this);
      } catch (e) {
        // No loopback port. Its own outcome, not a kind of refusal: NOT BEING the hand and BEING
        // BROKEN are different events, and a person who cannot tell them apart cannot act on
        // either. (The JetBrains client says the same thing in its own words, and has said it
        // since it grew this button.)
        this.why = `could not open a loopback port — ${(e as Error).message}`;
        throw e;
      }
    }
    const r = await this.companion.ask('mcp-attach', {
      name: HAND_NAME, url: this.server.url, headers: this.server.headers,
    });
    this.attached = r?.ok === true;
    this.why = this.attached
      ? `attached — ${r?.tools?.join(', ') || 'this editor answers for its own files'}`
      : `refused — ${r?.error ?? 'no answer'}`;
    if (!this.attached) console.warn(`magi: the editor's tools were refused — ${r?.error ?? 'no answer'}`);
    this.told?.(this.why);
  }

  /**
   * What became of the offer, in one sentence — for a screen to draw.
   *
   * Three things can happen and, until this existed, a person could tell none of them apart: the
   * daemon does not advertise `tool-servers` and `offer` returned in silence; no loopback port was
   * free; the attach was refused because another window on this workspace is already the hand.
   * All three ended in `console.warn` at best — the extension-host log, which nobody opens — so
   * the symptom of every one of them was the same: the agent does not use the editor, and there is
   * nowhere to find out why.
   *
   * Empty until `offer` has run. Empty is "not yet", which the panel draws as such.
   */
  private why = '';
  handWhy(): string { return this.why; }
  /** Called when the sentence changes, so a panel can redraw without polling. */
  told?: (why: string) => void;

  /** Open a file and put the cursor on a line. */
  async show(path: string, line?: number): Promise<string> {
    const uri = this.resolve(path);
    const doc = await vscode.workspace.openTextDocument(uri);
    // One-based in, zero-based here — the person and the tool count from 1, the API from 0.
    const at = new vscode.Position(Math.max(0, (line ?? 1) - 1), 0);
    await vscode.window.showTextDocument(doc, { selection: new vscode.Range(at, at) });
    return `opened ${uri.fsPath}` + (line ? ` at line ${line}` : '');
  }

  /**
   * Replace text through the editor.
   *
   * Not found and found-many-times are told APART. Folding both into "failed" leaves the agent
   * with nothing to act on — one means the string is wrong, the other means narrow it.
   */
  async replace(path: string, old: string, text: string, all: boolean): Promise<string> {
    const uri = this.resolve(path);
    const doc = await vscode.workspace.openTextDocument(uri);
    const body = doc.getText();
    const hits = body.split(old).length - 1;
    if (hits === 0) return `that text is not in ${uri.fsPath}`;
    if (hits > 1 && !all) {
      return `that text appears ${hits} times in ${uri.fsPath} — narrow it, or pass replaceAll`;
    }
    const edit = new vscode.WorkspaceEdit();
    const next = all ? body.split(old).join(text) : body.replace(old, text);
    edit.replace(uri, new vscode.Range(doc.positionAt(0), doc.positionAt(body.length)), next);
    if (!(await vscode.workspace.applyEdit(edit))) return `the editor refused the edit to ${uri.fsPath}`;
    return `replaced ${all ? hits : 1} occurrence(s) in ${uri.fsPath} — in the editor, so undo and ` +
      'the language server see it';
  }

  /**
   * What the editor's own language servers say.
   *
   * This is the tool with no counterpart in magi: today an agent learns about compile errors by
   * running a build and reading the words. The editor already has the real diagnostics, from the
   * same servers that underline the code a person is looking at.
   */
  async problems(path?: string): Promise<string> {
    const all = path
      ? ([[this.resolve(path), vscode.languages.getDiagnostics(this.resolve(path))]] as [vscode.Uri, vscode.Diagnostic[]][])
      : vscode.languages.getDiagnostics();
    const lines: string[] = [];
    for (const [uri, list] of all) {
      for (const d of list) {
        // Hints and information are not what anybody asked for here; they are the editor's
        // suggestions, and mixing them in would bury the two that stop a build.
        if (d.severity !== vscode.DiagnosticSeverity.Error && d.severity !== vscode.DiagnosticSeverity.Warning) continue;
        const kind = d.severity === vscode.DiagnosticSeverity.Error ? 'error' : 'warning';
        const where = vscode.workspace.asRelativePath(uri);
        lines.push(`${where}:${d.range.start.line + 1}:${d.range.start.character + 1} ${kind}: ${d.message}` +
          (d.source ? ` (${d.source})` : ''));
      }
    }
    if (!lines.length) {
      // Saying nothing reads as the tool not working. And "no diagnostics" is not "no problems":
      // a language server that has not started yet also has nothing to say.
      return path
        ? `no errors or warnings for ${path} — note that a language server that has not finished ` +
          'loading also reports none'
        : 'no errors or warnings anywhere in this window right now';
    }
    // Bounded: a workspace mid-refactor can hold thousands, and this travels as a tool result.
    const cap = 200;
    const head = lines.slice(0, cap).join('\n');
    return lines.length > cap ? `${head}\n… and ${lines.length - cap} more` : head;
  }

  /** Relative paths are this workspace's. An absolute one is taken as given. */
  private resolve(path: string): vscode.Uri {
    // Refused before it becomes a Uri: the companion names this path, and the workspace is a trust
    // boundary. The sibling client keeps the same line (`find` returns null outside the project).
    if (!inside(this.workdir, path)) throw new Error(`${path} is outside this workspace`);
    return path.startsWith('/') ? vscode.Uri.file(path) : vscode.Uri.joinPath(vscode.Uri.file(this.workdir), path);
  }

  dispose(): void {
    // Detach BEFORE the server goes, or the daemon holds an address that answers nothing until it
    // next tries to call it.
    if (this.attached) void this.companion.ask('mcp-detach', { name: HAND_NAME });
    this.server?.close();
    this.server = null;
  }
}
