/**
 * Approval diff extraction and virtual document identity.
 *
 * For approval requests (permission.requested), inspections should use native IDE diff/editor
 * surfaces rather than squeezing code into the chat webview.
 *
 * Two modes:
 *  1. Side-by-side comparison: When exact before/after chunks exist (edit tool without anchors or replaceAll).
 *     Labelled as substitution chunks ('치환 전/후 조각'), not whole files.
 *  2. Raw patch document: When only a unified diff is present (write, bash, or patch).
 *     Opened as a read-only document without inventing missing base files.
 *
 * Neither mode mutates disk files or auto-approves requests.
 */

import type { Ask } from './touched';

export interface ApprovalDiffSides {
  path: string;
  old: string;
  new: string;
}

export type ApprovalDiffKind = 'sides' | 'patch' | 'none';

const TRUTHY = new Set(['true', 'yes', 'on', '1']);
const FALSY = new Set(['false', 'no', 'off', '0', '', undefined, null]);

/**
 * Extracts before/after substitution chunks from tool arguments.
 *
 * Mirrors core/JetBrains logic (Rows.EditSides.of):
 *  - Tool must be exactly 'edit'.
 *  - Must not be anchored ('at' parameter must be absent or empty).
 *  - Must not be full replacement ('replaceAll' must be falsy).
 *  - Both 'old' and 'new' must be strings.
 */
export function extractEditSides(tool: string | undefined, args: unknown): ApprovalDiffSides | null {
  if (!tool || tool.toLowerCase() !== 'edit') return null;
  let o: Record<string, unknown> | null = null;
  if (typeof args === 'string') {
    try { o = JSON.parse(args) as Record<string, unknown>; } catch { return null; }
  } else if (args && typeof args === 'object') {
    o = args as Record<string, unknown>;
  }
  if (!o) return null;

  if (o.at !== undefined && String(o.at).trim() !== '') return null;
  if (o.replaceAll !== undefined) {
    const rep = String(o.replaceAll).toLowerCase().trim();
    if (TRUTHY.has(rep) || !FALSY.has(rep)) return null;
  }

  if (typeof o.old !== 'string' || typeof o.new !== 'string') return null;
  const path = typeof o.path === 'string' && o.path.trim() ? o.path.trim() : '변경';
  return { path, old: o.old, new: o.new };
}

/**
 * Single source of truth for whether an approval request is eligible for native diff or patch view.
 */
export function determineApprovalDiffKind(
  ask: { what?: string; args?: unknown; diff?: string } | null | undefined
): ApprovalDiffKind {
  if (!ask) return 'none';
  if (extractEditSides(ask.what, ask.args) !== null) {
    return 'sides';
  }
  if (typeof ask.diff === 'string' && ask.diff.trim().length > 0) {
    return 'patch';
  }
  return 'none';
}

/**
 * Deterministic URI for a virtual diff document.
 *
 * Incorporates companion, session, callId, side, and filename to prevent cross-session collisions
 * while allowing tab reuse on repeated clicks of the same request.
 */
export function approvalDiffUri(
  companionId: string,
  sessionId: string,
  callId: string,
  side: 'before' | 'after' | 'patch',
  filename: string
): string {
  const encCompanion = encodeURIComponent(companionId || 'default');
  const encSession = encodeURIComponent(sessionId || 'default');
  const encCall = encodeURIComponent(callId);
  const encSide = encodeURIComponent(side);
  const encFile = encodeURIComponent(filename);
  return `magi-diff:/${encCompanion}/${encSession}/${encCall}/${encSide}/${encFile}`;
}

/**
 * Title displayed on the native diff/editor tab.
 *
 * Accurately indicates substitution chunks rather than claiming whole file diffs.
 */
export function approvalDiffTitle(filename: string, isSides: boolean): string {
  if (isSides) {
    return `${filename} (치환 전 조각 ↔ 치환 후 조각)`;
  }
  return `magi 승인 — ${filename}`;
}

/**
 * Immutable snapshot cache for virtual diff documents.
 *
 * Preserves the state at the moment of approval request without re-reading disk or
 * mutating across subsequent turns.
 *
 *  - Immutability: Once stored for a key, subsequent calls to put() with the same key
 *    preserve the initial content rather than overwriting.
 *  - Open Tab Protection: When capacity is exceeded, entries that are currently pinned
 *    (e.g., active editor tabs) are preserved rather than evicted.
 *  - Single Canonical Key: Only the canonical URI string is stored.
 */
export class ApprovalSnapshots {
  private readonly store = new Map<string, string>();
  private readonly order: string[] = [];
  private readonly tempPinned = new Map<string, number>();

  constructor(
    private readonly maxEntries: number = 100,
    private readonly isPinned?: (key: string) => boolean
  ) {}

  /**
   * Temporarily protects keys (e.g. both before and after sides of an impending diff)
   * from eviction until the editor opens the document or fails.
   *
   * Supports nested/concurrent protections of the same keys using reference counting,
   * ensuring that the completion/failure of one operation does not prematurely unprotect
   * the other operation's documents.
   *
   * Returns an idempotent release function safe against multiple calls.
   */
  protectTemp(keys: string[]): () => void {
    for (const k of keys) {
      const current = this.tempPinned.get(k) ?? 0;
      this.tempPinned.set(k, current + 1);
    }
    let released = false;
    return () => {
      if (released) return; // Idempotent: multiple calls do nothing
      released = true;
      for (const k of keys) {
        const count = this.tempPinned.get(k) ?? 0;
        if (count <= 1) {
          this.tempPinned.delete(k);
        } else {
          this.tempPinned.set(k, count - 1);
        }
      }
      this.evictExcess();
    };
  }

  put(key: string, content: string): boolean {
    if (this.store.has(key)) {
      // Truly immutable: keep initial content, reject/ignore overwrite
      return false;
    }
    this.order.push(key);
    this.store.set(key, content);

    while (this.order.length > this.maxEntries) {
      const evictIndex = this.order.slice(0, -1).findIndex(k => !this.isProtected(k));
      if (evictIndex < 0) {
        // All older entries are currently protected; allow limit to be exceeded
        break;
      }
      const [evicted] = this.order.splice(evictIndex, 1);
      this.store.delete(evicted);
    }
    return true;
  }

  /**
   * Evaluates protection status and prunes unpinned entries if cache size exceeds maxEntries.
   * Called when documents/tabs close, or when temporary protection is released.
   */
  evictExcess(): number {
    let count = 0;
    while (this.order.length > this.maxEntries) {
      const evictIndex = this.order.findIndex(k => !this.isProtected(k));
      if (evictIndex < 0) {
        // All tracked entries are currently pinned (open in tabs or in-flight diff); protect them
        break;
      }
      const [evicted] = this.order.splice(evictIndex, 1);
      this.store.delete(evicted);
      count++;
    }
    return count;
  }

  private isProtected(key: string): boolean {
    if (this.tempPinned.has(key)) return true;
    return this.isPinned ? this.isPinned(key) : false;
  }

  get(key: string): string | undefined {
    return this.store.get(key);
  }

  has(key: string): boolean {
    return this.store.has(key);
  }

  delete(key: string): boolean {
    const idx = this.order.indexOf(key);
    if (idx >= 0) this.order.splice(idx, 1);
    this.tempPinned.delete(key);
    return this.store.delete(key);
  }

  get size(): number {
    return this.store.size;
  }

  get tempPinnedSize(): number {
    return this.tempPinned.size;
  }

  clear(): void {
    this.store.clear();
    this.order.length = 0;
    this.tempPinned.clear();
  }
}

/**
 * Preserved approval request associated with the workspace companion and session
 * at the moment the request was received.
 */
export interface StoredAsk {
  ask: Ask;
  companionId: string;
  sessionId: string;
}

/**
 * Bounded store of recent approval asks keyed by callId.
 *
 * Pins companion and session identity at ask arrival time so that late diff clicks
 * (even after companion or session switch) open the diff under the original session.
 */
export class AskStore {
  private readonly store = new Map<string, StoredAsk>();
  private readonly order: string[] = [];

  constructor(private readonly maxEntries: number = 50) {}

  record(ask: Ask, companionId: string, sessionId: string): void {
    const key = ask.callId;
    if (!key) return;
    if (!this.store.has(key)) {
      this.order.push(key);
      if (this.order.length > this.maxEntries) {
        const oldest = this.order.shift();
        if (oldest) this.store.delete(oldest);
      }
    }
    this.store.set(key, {
      ask,
      companionId: companionId || 'default',
      sessionId: sessionId || 'default',
    });
  }

  get(callId: string): StoredAsk | undefined {
    return this.store.get(callId);
  }

  has(callId: string): boolean {
    return this.store.has(callId);
  }

  delete(callId: string): boolean {
    const idx = this.order.indexOf(callId);
    if (idx >= 0) this.order.splice(idx, 1);
    return this.store.delete(callId);
  }

  get size(): number {
    return this.store.size;
  }

  clear(): void {
    this.store.clear();
    this.order.length = 0;
  }
}
