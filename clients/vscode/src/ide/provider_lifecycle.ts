import * as vscode from 'vscode';

export interface ProviderLifecycleOptions {
  scheme: string;
  onPrune: () => void;
  supportsDiffTabs?: boolean;
}

/**
 * Common lifecycle coordinator for virtual document content providers.
 *
 * Coordinates:
 *  - Tracking whether documents of the specified scheme are currently open in
 *    workspace text documents or active editor tabs.
 *  - Subscribing to document close (onDidCloseTextDocument) and tab changes (onDidChangeTabs)
 *    to trigger excess cache eviction without waiting for new put() calls.
 *  - Scheme filtering: Ignores close events from unrelated schemes.
 *  - TabInputTextDiff discrimination: DiffProvider protects both original and modified sides,
 *    whereas OutputProvider only protects standard TabInputText tabs.
 *  - Idempotent disposal: Ensures listeners are disposed exactly once, and late event callbacks
 *    after disposal do not execute onPrune.
 */
export class ProviderLifecycle implements vscode.Disposable {
  readonly scheme: string;
  private readonly onPrune: () => void;
  private readonly supportsDiffTabs: boolean;
  private readonly subs: vscode.Disposable[] = [];
  private disposed = false;

  constructor(options: ProviderLifecycleOptions) {
    this.scheme = options.scheme;
    this.onPrune = options.onPrune;
    this.supportsDiffTabs = options.supportsDiffTabs ?? false;

    this.subs.push(
      vscode.workspace.onDidCloseTextDocument((doc) => {
        if (!this.disposed && doc.uri?.scheme === this.scheme) {
          this.onPrune();
        }
      })
    );

    try {
      if (vscode.window.tabGroups) {
        this.subs.push(
          vscode.window.tabGroups.onDidChangeTabs(() => {
            if (!this.disposed) {
              this.onPrune();
            }
          })
        );
      }
    } catch {}
  }

  isOpen(key: string): boolean {
    if (this.disposed) return false;
    if (
      vscode.workspace.textDocuments?.some(
        (doc) => doc.uri?.scheme === this.scheme && doc.uri.toString() === key
      )
    ) {
      return true;
    }
    try {
      if (vscode.window.tabGroups?.all) {
        for (const group of vscode.window.tabGroups.all) {
          for (const tab of group.tabs) {
            const input = tab.input;
            if (
              this.supportsDiffTabs &&
              vscode.TabInputTextDiff &&
              input instanceof vscode.TabInputTextDiff
            ) {
              if (input.original?.scheme === this.scheme && input.original.toString() === key) return true;
              if (input.modified?.scheme === this.scheme && input.modified.toString() === key) return true;
            } else if (vscode.TabInputText && input instanceof vscode.TabInputText) {
              if (input.uri?.scheme === this.scheme && input.uri.toString() === key) return true;
            }
          }
        }
      }
    } catch {}
    return false;
  }

  get isDisposed(): boolean {
    return this.disposed;
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    while (this.subs.length > 0) {
      const sub = this.subs.pop();
      try {
        sub?.dispose();
      } catch {}
    }
  }
}
