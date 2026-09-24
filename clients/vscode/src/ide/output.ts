import * as vscode from 'vscode';
import {
  OutputSnapshots,
  resolveOutputItem,
  outputUri,
  defaultOutputFilename,
} from '../core/output';
import { Event } from '../core/protocol';

import { ProviderLifecycle } from './provider_lifecycle';

/**
 * Provides read-only content for virtual output documents.
 *
 * Scheme: `magi-output`
 *
 * Keeps immutable snapshots of finalized assistant answers and tool results at the
 * moment requested, without reading the live disk or executing commands.
 *
 * Protects documents currently open in editor tabs from eviction (insertion-order FIFO).
 * Never degrades missing or expired snapshots to empty documents.
 */
export class OutputProvider implements vscode.TextDocumentContentProvider, vscode.Disposable {
  static readonly scheme = 'magi-output';
  private readonly snapshots: OutputSnapshots;
  private readonly lifecycle: ProviderLifecycle;

  constructor(maxEntries: number = 100) {
    this.lifecycle = new ProviderLifecycle({
      scheme: OutputProvider.scheme,
      onPrune: () => this.prune(),
      supportsDiffTabs: false,
    });
    this.snapshots = new OutputSnapshots(maxEntries, (key: string) => this.isOpen(key));
  }

  isOpen(key: string): boolean {
    return this.lifecycle.isOpen(key);
  }

  provideTextDocumentContent(uri: vscode.Uri): string {
    const key = uri.toString();
    const content = this.snapshots.get(key);
    if (content === undefined) {
      throw new Error(`자료를 더 이상 열 수 없습니다: ${key}`);
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

  prune(): number {
    return this.snapshots.evictExcess();
  }

  protectTemp(uris: (vscode.Uri | string)[]): () => void {
    const keys = uris.map((u) => (typeof u === 'string' ? u : u.toString()));
    return this.snapshots.protectTemp(keys);
  }

  dispose(): void {
    this.lifecycle.dispose();
    this.snapshots.clear();
  }
}

export interface OpenOutputOptions {
  provider: OutputProvider;
  companionKey: string;
  session: string;
  outputId: string;
  events: Event[];
  preserveFocus?: boolean;
}

export interface OpenOutputResult {
  opened: boolean;
  error?: string;
  warning?: string;
  uri?: vscode.Uri;
}

/**
 * Opens a finalized assistant answer or tool result in native VS Code editor tabs without side-effects.
 *
 * Pure inspection:
 *  - Resolves verbatim raw content and metadata from confirmed events.
 *  - Does NOT mutate any file on disk, auto-approve, or re-run tools.
 *  - Uses deterministic URIs so repeated clicks reuse the existing tab.
 *  - Protects snapshot during tab opening via try ... finally so in-flight eviction never occurs.
 *  - Catches IDE API errors into { opened: false, error } rather than throwing uncaught rejections.
 *  - Uses the updated TextDocument returned by setTextDocumentLanguage.
 *  - If language setting fails, retains raw document inspection and returns warning without masking failure.
 */
export async function openOutputDocument(options: OpenOutputOptions): Promise<OpenOutputResult> {
  const { provider, companionKey, session, outputId, events, preserveFocus } = options;
  if (!session || !outputId) {
    return { opened: false, error: '세션 또는 자료 ID가 지정되지 않았습니다.' };
  }

  const item = resolveOutputItem(events, outputId);
  if (!item) {
    return { opened: false, error: '자료를 더 이상 열 수 없음' };
  }

  const filename = defaultOutputFilename(item);
  const uriStr = outputUri(companionKey, session, item.kind, outputId, filename);
  const uri = vscode.Uri.parse(uriStr);

  const unprotect = provider.protectTemp([uri]);
  try {
    provider.put(uri, item.content);
    let doc = await vscode.workspace.openTextDocument(uri);
    let warning: string | undefined;
    if (item.language && vscode.languages?.setTextDocumentLanguage) {
      try {
        const updatedDoc = await vscode.languages.setTextDocumentLanguage(doc, item.language);
        if (updatedDoc) {
          doc = updatedDoc;
        }
      } catch (langErr: any) {
        warning = langErr?.message ? `언어 모드 설정 실패: ${langErr.message}` : '언어 모드 설정 실패';
      }
    }
    await vscode.window.showTextDocument(doc, { preview: true, preserveFocus: preserveFocus ?? false });
    return { opened: true, uri, ...(warning ? { warning } : {}) };
  } catch (err: any) {
    return {
      opened: false,
      error: err?.message ? `편집창 열기 실패: ${err.message}` : '편집창 열기 실패',
    };
  } finally {
    unprotect();
  }
}
