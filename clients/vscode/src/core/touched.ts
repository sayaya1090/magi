import { Event } from './protocol';

/**
 * Which files the companion changed, read off the transcript rather than off the disk.
 *
 * The daemon is a process outside the editor, so a file it rewrites is stale on screen until
 * something reloads it. VS Code's watcher usually does, but "usually" is not a contract: it does
 * not watch outside the workspace and it coalesces bursts. The transcript already knows, so use it.
 *
 * Two kinds, and the difference decides what can be done about them:
 *  - **named** — edit/write/multiedit carry a path. Reload exactly that file.
 *  - **unnamed** — bash and its family may have written anything. All that can be said is "the
 *    tree moved", and a screen that pretended to know which file would be inventing one.
 */
export interface Touched {
  named: string[];
  unnamed: boolean;
}

const NAMES_A_PATH = new Set(['edit', 'write', 'multiedit']);
const MAY_WRITE_ANYTHING = new Set(['bash', 'bash_output', 'bash_kill']);

/**
 * Successful results only. A tool that failed did not change the file, and reloading on a failure
 * would throw away whatever the person had unsaved for nothing.
 */
export function touched(events: Event[]): Touched {
  const calls = new Map<string, { name: string; path?: string }>();
  const named = new Set<string>();
  let unnamed = false;
  for (const e of events) {
    if (e.type !== 'part.appended') continue;
    const d = (e.data ?? {}) as Record<string, unknown>;
    const p = (d.part ?? {}) as {
      kind?: string;
      toolCall?: { callId?: string; name?: string; args?: unknown };
      toolResult?: { callId?: string; isError?: boolean };
    };
    if (p.kind === 'tool-call' && p.toolCall?.callId) {
      const args = parseArgs(p.toolCall.args);
      calls.set(p.toolCall.callId, {
        name: (p.toolCall.name ?? '').toLowerCase(),
        path: typeof args.path === 'string' ? args.path : undefined,
      });
    } else if (p.kind === 'tool-result' && p.toolResult?.callId) {
      if (p.toolResult.isError) continue;
      const call = calls.get(p.toolResult.callId);
      if (!call) continue;
      if (NAMES_A_PATH.has(call.name) && call.path) named.add(call.path);
      else if (MAY_WRITE_ANYTHING.has(call.name)) unnamed = true;
    }
  }
  return { named: [...named], unnamed };
}

/** Tool arguments arrive as an object or as the JSON text of one, depending on the tool. */
function parseArgs(args: unknown): Record<string, unknown> {
  if (args && typeof args === 'object') return args as Record<string, unknown>;
  if (typeof args === 'string') {
    try { return JSON.parse(args) as Record<string, unknown>; } catch { return {}; }
  }
  return {};
}

/** What the companion is waiting on, if anything, read off the same stream. */
export function pendingAsk(events: Event[]): { callId: string; what: string } | null {
  let open: { callId: string; what: string } | null = null;
  for (const e of events) {
    const d = (e.data ?? {}) as Record<string, unknown>;
    if (e.type === 'permission.requested') {
            // `name` is the tool, as PermissionRequestedData spells it. Guessed field names are the
      // silent kind of wrong here: JSON hands back undefined and the row says "a tool" for ever.
      open = { callId: String(d.callId ?? ''), what: String(d.name ?? 'a tool') };
    } else if (e.type === 'permission.decided' || e.type === 'turn.finished') {
      open = null;
    }
  }
  return open;
}
