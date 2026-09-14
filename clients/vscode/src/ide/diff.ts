import * as vscode from 'vscode';
import { Ask } from '../core/touched';
import {
  ApprovalSnapshots,
  extractEditSides,
  determineApprovalDiffKind,
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
 *
 * Protects documents currently open in editor tabs from LRU eviction.
 * Never degrades missing or expired snapshots to empty documents.
 */
export class DiffProvider implements vscode.TextDocumentContentProvider, vscode.Disposable {
  static readonly scheme = 'magi-diff';
  private readonly snapshots: ApprovalSnapshots;

  constructor(maxEntries: number = 100) {
    this.snapshots = new ApprovalSnapshots(maxEntries, (key: string) => {
      return vscode.workspace.textDocuments.some(
        (doc) => doc.uri.scheme === DiffProvider.scheme && doc.uri.toString() === key
      );
    });
  }

  provideTextDocumentContent(uri: vscode.Uri): string {
    const key = uri.toString();
    const content = this.snapshots.get(key);
    if (content === undefined) {
      throw new Error(`승인 스냅샷이 만료되었거나 존재하지 않습니다: ${key}`);
    }
    return content;
  }

  put(uri: vscode.Uri, content: string): boolean {
    return this.snapshots.put(uri.toString(), content);
  }

  get(uri: vscode.Uri | string): string | undefined {
    const key = typeof uri === 'string' ? uri : uri.toString();
    return this.snapshots.get(key);
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
 *  - Returns true if opened, false if not eligible for diff view.
 */
export async function openApprovalDiff(
  provider: DiffProvider,
  companionId: string,
  sessionId: string,
  ask: Ask,
): Promise<boolean> {
  const kind = ask.diffKind ?? determineApprovalDiffKind(ask);
  if (kind === 'sides') {
    const sides = extractEditSides(ask.what, ask.args);
    if (!sides) return false;
    const rawPath = sides.path;
    const basename = rawPath.split(/[/\\]/).pop() || rawPath || '변경';
    const leftUri = vscode.Uri.parse(approvalDiffUri(companionId, sessionId, ask.callId, 'before', basename));
    const rightUri = vscode.Uri.parse(approvalDiffUri(companionId, sessionId, ask.callId, 'after', basename));

    provider.put(leftUri, sides.old);
    provider.put(rightUri, sides.new);

    const title = approvalDiffTitle(basename, true);
    await vscode.commands.executeCommand('vscode.diff', leftUri, rightUri, title, { preview: true });
    return true;
  }

  if (kind === 'patch' && ask.diff && ask.diff.trim()) {
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
    return true;
  }

  return false;
}
