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

  /** Reload what changed. Nothing here writes; it only asks the editor to read again. */
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
  }

  /** Clear the marks. The turn is over; the highlight was about the turn. */
  clear(): void {
    this.seen.clear();
    for (const ed of vscode.window.visibleTextEditors) ed.setDecorations(this.mark, []);
  }

  dispose(): void { this.mark.dispose(); }
}
