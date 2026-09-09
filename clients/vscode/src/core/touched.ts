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
  /**
   * The text each edit put into a file, so a screen can find WHERE it landed.
   *
   * The paths alone say which files moved; they do not say which lines, and the transcript carries
   * no line numbers — the edit tools address text by its content (`old`/`new`), not by position.
   * So the only honest way to mark lines is to look for the text that was inserted, which means
   * carrying it this far. A file written whole has no insert to look for and gets none: the whole
   * file is new, and the screen decides what that means.
   */
  inserts: { path: string; text: string }[];
}

const NAMES_A_PATH = new Set(['edit', 'write', 'multiedit']);
const MAY_WRITE_ANYTHING = new Set(['bash', 'bash_output', 'bash_kill']);

/**
 * Successful results only. A tool that failed did not change the file, and reloading on a failure
 * would throw away whatever the person had unsaved for nothing.
 */
export function touched(events: Event[]): Touched {
  const calls = new Map<string, { name: string; path?: string; inserts: string[] }>();
  const named = new Set<string>();
  const inserts: { path: string; text: string }[] = [];
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
        inserts: inserted(args),
      });
    } else if (p.kind === 'tool-result' && p.toolResult?.callId) {
      if (p.toolResult.isError) continue;
      const call = calls.get(p.toolResult.callId);
      if (!call) continue;
      if (NAMES_A_PATH.has(call.name) && call.path) {
        named.add(call.path);
        for (const text of call.inserts) inserts.push({ path: call.path, text });
      }
      else if (MAY_WRITE_ANYTHING.has(call.name)) unnamed = true;
    }
  }
  return { named: [...named], unnamed, inserts };
}

/**
 * What an edit put in, read off its arguments.
 *
 * Field names copied from the tools themselves (`internal/adapter/tool/builtin`): `edit` takes
 * `new`, `multiedit` takes a list of hunks that each do, and `write` takes `content` — but a whole
 * file is not an insert to search for, so it yields none. A guessed name here fails the quiet way:
 * JSON hands back undefined, the list is empty, and nothing is ever marked while nothing errors.
 */
function inserted(args: Record<string, unknown>): string[] {
  const out: string[] = [];
  if (typeof args.new === 'string' && args.new) out.push(args.new);
  if (Array.isArray(args.edits)) {
    for (const h of args.edits) {
      const t = (h as { new?: unknown })?.new;
      if (typeof t === 'string' && t) out.push(t);
    }
  }
  return out;
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
/** What the companion is blocked on: a verdict to give, or a sentence to write. */
export interface Ask {
  /**
   * `permission` or `question` — the core's own two words, and the screen draws two different
   * things from them.
   *
   * ⚠ **It was never set.** The screen tests `kind === 'permission'` to put up allow/deny/always,
   * and this function returned an object without the field — so every permission prompt fell
   * through to the question branch and drew the TOOL NAME with a free-text box, while the three
   * buttons sat in the code and never ran once. Measured 2026-09-09.
   */
  kind: 'permission' | 'question';
  callId: string;
  what: string;
  /**
   * A permission's SUBJECT — what is actually being decided.
   *
   * ⚠ The screen drew `magi wants to run: bash` and nothing else, so a person pressed allow without
   * the command, or approved an edit without what it changes. The core wrote the field for exactly
   * this and said why: the rest of the request rides along "so a viewer draws the prompt rather
   * than a description of it" — a tool NAME is the description, not the request. The JetBrains
   * client carries it and its comment calls the gap by its name: you press without knowing what
   * you are allowing, and the treatment was inverted against the stakes — the one place where the
   * most is riding on it was the quiet one.
   *
   * `args` is the thing itself and is shown verbatim; `reason` is prose about why the policy
   * stopped here. Kept apart, because a screen that merges them cannot say which it is drawing.
   */
  args?: string;
  reason?: string;
  /** What approving would change, computed once by the core and never recomputed by a viewer. */
  diff?: string;
  /**
   * The GROUNDS a question was asked on — what the person is meant to decide from.
   *
   * The decision-report skill gathers these and asks in their order, and the core carries them for
   * the same reason it carries the options: a console in another process draws the prompt, and "a
   * prompt whose grounds stayed behind is the one this exists to stop". It also says why they are
   * recorded rather than transient — a decision's reasons are the part somebody comes back to a
   * month later asking why the fleet went the way it did.
   *
   * Both IDE clients dropped them, so a question raised with its grounds arrived as a bare
   * sentence and three buttons.
   */
  report?: { key: string; text: string }[];
  /** A question's shortcuts to an answer (`QuestionRequestedData.Options`). */
  options?: string[];
  /** Where this question sits in the run its call is asking: 3 of 5. */
  index?: number;
  total?: number;
}

/**
 * What is standing unanswered, from the log.
 *
 * ⚠ **Both kinds.** This read `permission.requested` alone, so a question — the `ask_user` tool,
 * with its options — never raised an ask box at all: the core wrote `question.requested` and
 * nothing here looked at it. The two are answered through different doors (`permission` takes a
 * verdict, `answer` takes a sentence), which is exactly why the KIND has to travel with them.
 */
export function pendingAsk(events: Event[]): Ask | null {
  let open: Ask | null = null;
  for (const e of events) {
    const d = (e.data ?? {}) as Record<string, unknown>;
    if (e.type === 'permission.requested') {
      // `name` is the tool, as PermissionRequestedData spells it. Guessed field names are the
      // silent kind of wrong here: JSON hands back undefined and the row says "a tool" for ever.
      const raw = d.args;
      open = {
        kind: 'permission', callId: String(d.callId ?? ''), what: String(d.name ?? 'a tool'),
        // The value, not its rendering: `args` is often a JSON string, and stringifying it twice
        // leaves the escapes on screen.
        args: raw === undefined || raw === null ? undefined
          : (typeof raw === 'string' ? raw : JSON.stringify(raw)) || undefined,
        reason: String(d.reason ?? '').trim() || undefined,
        diff: String(d.diff ?? '').trim() || undefined,
      };
    } else if (e.type === 'question.requested') {
      open = {
        kind: 'question', callId: String(d.callId ?? ''), what: String(d.question ?? ''),
        options: Array.isArray(d.options) ? d.options.map(String) : undefined,
        report: Array.isArray(d.report)
          ? (d.report as Record<string, unknown>[])
              .map((r) => ({ key: String(r?.key ?? ''), text: String(r?.text ?? '') }))
              .filter((r) => r.text.trim())
          : undefined,
        index: Number(d.index) || undefined,
        total: Number(d.total) || undefined,
      };
    } else if (e.type === 'permission.decided' || e.type === 'question.answered' || e.type === 'turn.finished') {
      open = null;
    }
  }
  return open;
}
