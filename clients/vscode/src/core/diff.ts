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

export interface ApprovalDiffSides {
  path: string;
  old: string;
  new: string;
}

const TRUTHY = new Set(['true', 'True', 'yes', 'on', '1']);
const FALSY = new Set(['false', 'False', 'no', 'off', '0', '', undefined, null]);

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
    const rep = String(o.replaceAll);
    if (TRUTHY.has(rep) || !FALSY.has(rep)) return null;
  }

  if (typeof o.old !== 'string' || typeof o.new !== 'string') return null;
  const path = typeof o.path === 'string' && o.path.trim() ? o.path.trim() : '변경';
  return { path, old: o.old, new: o.new };
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
 * Preserves the state at the moment of approval request, without re-reading disk or
 * mutating across subsequent turns. Bounded by capacity.
 */
export class ApprovalSnapshots {
  private readonly store = new Map<string, string>();
  private readonly order: string[] = [];

  constructor(private readonly maxEntries: number = 100) {}

  put(key: string, content: string): void {
    if (!this.store.has(key)) {
      this.order.push(key);
      if (this.order.length > this.maxEntries) {
        const oldest = this.order.shift();
        if (oldest) this.store.delete(oldest);
      }
    }
    this.store.set(key, content);
  }

  get(key: string): string | undefined {
    return this.store.get(key);
  }

  has(key: string): boolean {
    return this.store.has(key);
  }

  clear(): void {
    this.store.clear();
    this.order.length = 0;
  }
}
