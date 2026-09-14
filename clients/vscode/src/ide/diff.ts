import * as vscode from 'vscode';
import { Ask } from '../core/touched';
import {
  ApprovalSnapshots,
  extractEditSides,
  approvalDiffUri,
  approvalDiffTitle,
} from '../core/diff';

/**
 * Provides read-only content for virtual diff and patch documents.
 *
 * Scheme: `magi-diff`
 *
 * Keeps immutable snapshots of the approval state at the moment requested, without reading
 * the live disk or mutating files.
 */
export class DiffProvider implements vscode.TextDocumentContentProvider, vscode.Disposable {
  static readonly scheme = 'magi-diff';
  private readonly snapshots = new ApprovalSnapshots();

  provideTextDocumentContent(uri: vscode.Uri): string {
    return this.snapshots.get(uri.toString()) ?? this.snapshots.get(uri.path) ?? '';
  }

  put(uri: vscode.Uri, content: string): void {
    this.snapshots.put(uri.toString(), content);
    this.snapshots.put(uri.path, content);
  }

  dispose(): void {
    this.snapshots.clear();
  }
}

/**
 * Opens an approval change in native VS Code editor surfaces without side-effects.
 *
 * Pure inspection:
 *  - If exact before/after substitution chunks exist, opens a side-by-side diff
 *    with `vscode.diff` between two virtual documents.
 *  - If only a raw unified diff is present, opens the patch in a read-only document.
 *  - Does NOT mutate any file on disk or auto-approve the request.
 *  - Uses deterministic URIs so repeated clicks reuse the existing tab.
 */
export async function openApprovalDiff(
  provider: DiffProvider,
  companionId: string,
  sessionId: string,
  ask: Ask,
): Promise<void> {
  const sides = extractEditSides(ask.what, ask.args);
  if (sides) {
    const rawPath = sides.path;
    const basename = rawPath.split(/[/\\]/).pop() || rawPath || '변경';
    const leftUri = vscode.Uri.parse(approvalDiffUri(companionId, sessionId, ask.callId, 'before', basename));
    const rightUri = vscode.Uri.parse(approvalDiffUri(companionId, sessionId, ask.callId, 'after', basename));

    provider.put(leftUri, sides.old);
    provider.put(rightUri, sides.new);

    const title = approvalDiffTitle(basename, true);
    await vscode.commands.executeCommand('vscode.diff', leftUri, rightUri, title, { preview: true });
    return;
  }

  if (ask.diff && ask.diff.trim()) {
    let filePath = 'changes';
    if (ask.args) {
      try {
        const o = typeof ask.args === 'string' ? JSON.parse(ask.args) : ask.args;
        if (o && typeof o.path === 'string' && o.path.trim()) filePath = o.path.trim();
      } catch {}
    }
    const basename = filePath.split(/[/\\]/).pop() || filePath || 'changes';
    const patchName = `magi-승인-${basename}-${ask.callId.slice(-6)}.diff`;
    const patchUri = vscode.Uri.parse(approvalDiffUri(companionId, sessionId, ask.callId, 'patch', patchName));

    provider.put(patchUri, ask.diff);

    const doc = await vscode.workspace.openTextDocument(patchUri);
    await vscode.window.showTextDocument(doc, { preview: true });
  }
}
