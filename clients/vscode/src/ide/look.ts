import * as vscode from 'vscode';
import { split, numbered, Look, ambient, place } from '../core/look';
import { Companion } from './workspace';

/**
 * Looking over the buffer somebody is typing in, and drawing what came back.
 *
 * Three surfaces for one answer, and which one a remark lands on is decided by whether it has a
 * line to hang on (core/look):
 *  - anchored → an inlay hint at the end of that line
 *  - loose    → a decoration above the first line, because it is about the file
 *  - nothing  → nothing at all. A look with no findings draws no chrome.
 */
export class Looking implements vscode.Disposable {
  private readonly notes = new Map<string, Look>();
  private readonly changed = new vscode.EventEmitter<void>();
  private readonly subs: vscode.Disposable[] = [];
  private timer: NodeJS.Timeout | null = null;
  private busy = false;

  /** The banner. A decoration rather than a notification: this belongs to the file, not to the app. */
  private readonly banner = vscode.window.createTextEditorDecorationType({
    isWholeLine: true,
    after: { margin: '0 0 0 1em', color: new vscode.ThemeColor('editorCodeLens.foreground') },
  });

  constructor(private readonly companion: Companion) {
    this.subs.push(
      vscode.languages.registerInlayHintsProvider({ pattern: '**' }, {
        onDidChangeInlayHints: this.changed.event,
        provideInlayHints: (doc, range) => this.hints(doc, range),
      }),
      vscode.workspace.onDidChangeTextDocument((e) => this.typed(e.document)),
      vscode.window.onDidChangeActiveTextEditor((ed) => ed && this.paint(ed)),
    );
  }

  /** Look now, whatever the automatic switch says — the person asked. */
  async now(ed: vscode.TextEditor): Promise<void> { await this.look(ed.document, true); }

  private typed(doc: vscode.TextDocument): void {
    if (doc.uri.scheme !== 'file') return;
    // Only the file they are looking at. A change in a background document — a formatter, another
    // extension, a search-and-replace — is not somebody typing, and sending it would spend a model
    // call on a buffer nobody is reading.
    if (vscode.window.activeTextEditor?.document !== doc) return;
    // The open buffer travels on every pause, always — the companion should know what is on screen
    // even when nobody asked it to look. That is ambient context, not a review.
    if (this.timer) clearTimeout(this.timer);
    this.timer = setTimeout(() => {
      // The HEAD only. This goes out on every pause in typing and the core keeps 8KB of it; sending
      // the rest put a whole file on the socket every 900ms for nothing (see `ambient`).
      void this.companion.ask('open-file', { name: doc.uri.fsPath, text: ambient(doc.getText()) });
      if (vscode.workspace.getConfiguration('magi').get<boolean>('lookWhileTyping', false)) {
        void this.look(doc, false);
      }
    }, 900);
  }

  private async look(doc: vscode.TextDocument, asked: boolean): Promise<void> {
    if (doc.uri.scheme !== 'file') return;
    // One at a time. A look that overtakes its predecessor would paint the older answer last.
    // A press while one is running is dropped rather than queued: by the time the queued one
    // answered, the buffer it read would be two edits old.
    if (this.busy) {
      if (asked) void vscode.window.setStatusBarMessage('magi: already looking…', 2000);
      return;
    }
    this.busy = true;
    try {
      const resp = await this.companion.ask('look-over', {
        name: doc.uri.fsPath, text: numbered(doc.getText()),
      });
      if (!resp?.ok) {
        // Only say so when somebody asked. An automatic look that could not run is not news.
        if (asked) void vscode.window.showWarningMessage(`magi: ${resp?.error ?? 'could not look over this file'}`);
        return;
      }
      // Anchored where it can be anchored; the rest kept as words rather than dropped. The line
      // count is read here because this is where the document is — `place` decides nothing about
      // the editor, only about which findings have a line in this buffer.
      const found = place(split(resp.out ?? ''), doc.lineCount);
      this.notes.set(doc.uri.toString(), found);
      this.changed.fire();
      const ed = vscode.window.visibleTextEditors.find((e) => e.document === doc);
      if (ed) this.paint(ed);
      if (asked && !found.anchored.length && !found.loose) {
        // Nothing to say is an answer, and the person who pressed the button is owed it. Silence
        // here reads as a button that did nothing.
        void vscode.window.setStatusBarMessage('magi: nothing worth saying about this file', 4000);
      }
    } finally {
      this.busy = false;
    }
  }

  private hints(doc: vscode.TextDocument, range: vscode.Range): vscode.InlayHint[] {
    const found = this.notes.get(doc.uri.toString());
    if (!found) return [];
    const out: vscode.InlayHint[] = [];
    for (const [line, text] of found.anchored) {
      const i = line - 1;
      // A number past the end of the file is a number the model got wrong. Dropping it here is
      // what makes reading the separator generously safe: generous in, never invented out.
      if (i < 0 || i >= doc.lineCount) continue;
      const at = doc.lineAt(i).range.end;
      if (!range.contains(at)) continue;
      const hint = new vscode.InlayHint(at, `  ${text}`, vscode.InlayHintKind.Type);
      hint.paddingLeft = true;
      out.push(hint);
    }
    return out;
  }

  private paint(ed: vscode.TextEditor): void {
    const found = this.notes.get(ed.document.uri.toString());
    const loose = found?.loose ?? '';
    if (!loose) { ed.setDecorations(this.banner, []); return; }
    ed.setDecorations(this.banner, [{
      range: new vscode.Range(0, 0, 0, 0),
      renderOptions: { after: { contentText: `  magi: ${loose.split('\n')[0]}` } },
    }]);
  }

  dispose(): void {
    if (this.timer) clearTimeout(this.timer);
    this.banner.dispose();
    this.changed.dispose();
    for (const s of this.subs) s.dispose();
  }
}
