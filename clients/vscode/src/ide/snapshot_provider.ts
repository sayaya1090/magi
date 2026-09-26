import * as vscode from 'vscode';
import type { ImmutableSnapshotStore } from '../core/snapshot';
import { ProviderLifecycle } from './provider_lifecycle';

/**
 * What differs between two read-only virtual-document providers that are otherwise the same.
 *
 * The approval diff (`magi-diff`) and the output document (`magi-output`) each serve immutable
 * snapshots from a bounded store that will not evict a document somebody still has open. Their code
 * was the same forty-odd lines twice — 35 of 42 identical — and the four facts below were the only
 * lines that differed. A fix to eviction, protection or disposal made in one of them was a fix the
 * other did not get.
 */
export interface SnapshotProviderOptions<S> {
  /** The URI scheme this provider answers for. */
  scheme: string;
  /** Whether an open diff tab counts as "open" for eviction (the diff view opens two documents). */
  supportsDiffTabs: boolean;
  maxEntries: number;
  /** Builds the store, handing it the provider's own "is this document open?" answer. */
  makeStore: (maxEntries: number, isOpen: (key: string) => boolean) => S;
  /** The sentence a person sees when the document is gone. Each surface says it in its own words. */
  missing: (key: string) => string;
}

/**
 * A `TextDocumentContentProvider` over a bounded, open-aware snapshot store.
 *
 * Subclasses keep their own name, static `scheme` and constructor signature, so nothing that builds
 * or type-checks against `DiffProvider` / `OutputProvider` changes.
 */
export abstract class SnapshotContentProvider<S extends ImmutableSnapshotStore<string>>
  implements vscode.TextDocumentContentProvider, vscode.Disposable
{
  /** Named `snapshots` on purpose: tests reach it to check that in-flight protection is released. */
  protected readonly snapshots: S;
  private readonly lifecycle: ProviderLifecycle;
  private readonly missing: (key: string) => string;

  protected constructor(opts: SnapshotProviderOptions<S>) {
    // Order matters and is the order both copies had: the lifecycle first, because the store asks it
    // whether a document is open, and pruning (driven by the lifecycle) goes back to the store.
    this.lifecycle = new ProviderLifecycle({
      scheme: opts.scheme,
      onPrune: () => this.prune(),
      supportsDiffTabs: opts.supportsDiffTabs,
    });
    this.snapshots = opts.makeStore(opts.maxEntries, (key: string) => this.isOpen(key));
    this.missing = opts.missing;
  }

  isOpen(key: string): boolean {
    return this.lifecycle.isOpen(key);
  }

  provideTextDocumentContent(uri: vscode.Uri): string {
    const key = uri.toString();
    const content = this.snapshots.get(key);
    if (content === undefined) {
      throw new Error(this.missing(key));
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
