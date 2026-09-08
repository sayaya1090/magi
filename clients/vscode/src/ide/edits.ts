import * as vscode from 'vscode';
import { Touched } from '../core/touched';

/**
 * Making the editor see what the companion changed, and marking the lines while the turn lasts.
 *
 * ⚠ **Dirty buffers are left alone.** Reloading over somebody's unsaved work to show them a
 * companion's edit trades a real loss for a cosmetic gain. Stale-on-screen is recoverable; their
 * typing is not.
 */
export class Edits implements vscode.Disposable {
  private readonly mark = vscode.window.createTextEditorDecorationType({
    isWholeLine: true,
    backgroundColor: new vscode.ThemeColor('diffEditor.insertedLineBackground'),
    overviewRulerColor: new vscode.ThemeColor('editorOverviewRuler.addedForeground'),
    overviewRulerLane: vscode.OverviewRulerLane.Left,
  });
  private seen = new Set<string>();

  /** Reload what changed, then mark it. Nothing here writes; it only asks the editor to read again. */
  async refresh(t: Touched): Promise<void> {
    for (const p of t.named) {
      if (this.seen.has(p)) continue;
      this.seen.add(p);
      const uri = vscode.Uri.file(p);
      const doc = vscode.workspace.textDocuments.find((d) => d.uri.fsPath === p);
      if (doc?.isDirty) continue; // theirs wins
      try { await vscode.commands.executeCommand('workbench.action.files.revert', uri); } catch { /* not open */ }
    }
    // Unnamed means bash may have written anything. There is no file to name, so the honest move
    // is to let the watcher do its work and say nothing — inventing a path would be worse.
    this.paint(t);
  }

  /**
   * Colour the lines an edit put there.
   *
   * After the revert, because the marks are ranges into the buffer the editor now holds — painting
   * first would mark the old text's positions and the revert would slide them.
   *
   * Located by searching for the inserted text rather than by a line number, because the
   * transcript has no line numbers to give: the edit tools address text by content. Text that
   * cannot be found is not marked and nothing is guessed — an edit whose result was itself edited
   * again is exactly the case where a guessed range would highlight somebody else's code.
   */
  private paint(t: Touched): void {
    if (!vscode.workspace.getConfiguration('magi').get<boolean>('showEditMarks', true)) return;
    for (const ed of vscode.window.visibleTextEditors) {
      const mine = t.inserts.filter((i) => i.path === ed.document.uri.fsPath);
      if (!mine.length) continue;
      const body = ed.document.getText();
      const ranges: vscode.Range[] = [];
      for (const { text } of mine) {
        const at = body.indexOf(text);
        if (at < 0) continue;
        ranges.push(new vscode.Range(
          ed.document.positionAt(at),
          ed.document.positionAt(at + text.length),
        ));
      }
      if (ranges.length) ed.setDecorations(this.mark, ranges);
    }
  }

  /** Clear the marks. The turn is over; the highlight was about the turn. */
  clear(): void {
    this.seen.clear();
    for (const ed of vscode.window.visibleTextEditors) ed.setDecorations(this.mark, []);
  }

  dispose(): void { this.mark.dispose(); }
}
