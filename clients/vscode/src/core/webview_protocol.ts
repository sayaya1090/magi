import * as v from 'valibot';
import { Row } from './transcript';
import { Ask } from './touched';
import { Activity, progressOf } from './activity';

export type PaintedRow = Row & { label: string; speaker?: string; badge?: string };

export interface PanelNoteInfo {
  text: string;
  offerStart: boolean;
}

// ── Shared Schema Atoms ──

export const NonEmptyStringSchema = v.pipe(
  v.string(),
  v.check((s) => s.trim().length > 0, 'Must not be blank')
);

export const PositiveIntegerSchema = v.pipe(
  v.number(),
  v.integer(),
  v.minValue(1)
);

export const NonNegativeIntegerSchema = v.pipe(
  v.number(),
  v.integer(),
  v.minValue(0)
);

// ── Webview to Host Schemas ──

export const ReadyMessageSchema = v.object({
  kind: v.literal('ready'),
});

export const StartMessageSchema = v.object({
  kind: v.literal('start'),
});

export const DropMessageSchema = v.object({
  kind: v.literal('drop'),
});

export const SayMessageSchema = v.object({
  kind: v.literal('say'),
  text: v.string(),
  creationTaskId: v.optional(NonEmptyStringSchema),
});

export const RunMessageSchema = v.object({
  kind: v.literal('run'),
  command: v.pipe(v.string(), v.minLength(1)),
});

export const DiffMessageSchema = v.object({
  kind: v.literal('diff'),
  session: NonEmptyStringSchema,
  callId: NonEmptyStringSchema,
});

export const OpenMessageSchema = v.object({
  kind: v.literal('open'),
  session: NonEmptyStringSchema,
  callId: NonEmptyStringSchema,
  seq: v.optional(
    v.pipe(
      v.unknown(),
      v.transform((val) => (typeof val === 'number' ? val : undefined)),
    ),
  ),
});

export const CopyMessageSchema = v.object({
  kind: v.literal('copy'), session: NonEmptyStringSchema, text: v.string(),
});

export const OutputMessageSchema = v.object({
  kind: v.literal('output'),
  session: NonEmptyStringSchema,
  outputId: NonEmptyStringSchema,
});

export const AnswerMessageSchema = v.object({
  kind: v.literal('answer'),
  callId: NonEmptyStringSchema,
  decision: NonEmptyStringSchema,
});

export const ReplyMessageSchema = v.object({
  kind: v.literal('reply'),
  callId: NonEmptyStringSchema,
  text: v.string(),
  attemptId: PositiveIntegerSchema,
  companionKey: NonEmptyStringSchema,
  session: NonEmptyStringSchema,
  generation: NonNegativeIntegerSchema,
  webviewId: NonEmptyStringSchema,
});

export const MentionMessageSchema = v.object({
  kind: v.literal('mention'),
  text: v.string(),
  reqId: v.custom<number>((input) => typeof input === 'number'),
  target: v.string(),
});

export const SuggestMessageSchema = v.object({
  kind: v.literal('suggest'),
  text: v.string(),
  reqId: v.custom<number>((input) => typeof input === 'number'),
  target: v.string(),
});

/** The person's word on a call the last stop left uncertain, or a retry of a recovery that failed. Records only; runs nothing. */
export const SettleMessageSchema = v.object({
  kind: v.literal('settle'),
  callId: v.optional(v.string()),
  verdict: v.optional(v.picklist(['ran', 'not_ran', 'rerun_ok', 'deferred'])),
  retry: v.optional(v.literal(true)),
});

export const WebviewToHostMessageSchema = v.union([
  ReadyMessageSchema,
  StartMessageSchema,
  DropMessageSchema,
  SayMessageSchema,
  RunMessageSchema,
  DiffMessageSchema,
  OpenMessageSchema,
  OutputMessageSchema,
  CopyMessageSchema,
  AnswerMessageSchema,
  ReplyMessageSchema,
  MentionMessageSchema,
  SuggestMessageSchema,
  SettleMessageSchema,
]);

// ── Inferred Type ──

export type WebviewToHostMessage = v.InferOutput<typeof WebviewToHostMessageSchema>;

/**
 * Runtime validation of messages received from the webview boundary using Valibot schemas.
 * Returns parsed message if valid according to schema, otherwise undefined.
 */
export function parseWebviewToHostMessage(raw: unknown): WebviewToHostMessage | undefined {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return undefined;
  const res = v.safeParse(WebviewToHostMessageSchema, raw);
  if (!res.success) return undefined;
  const out = res.output;

  // Preserve exact legacy property contract for open (seq is explicitly undefined when omitted or non-number)
  if (out.kind === 'open') {
    return {
      kind: 'open',
      session: out.session,
      callId: out.callId,
      seq: typeof out.seq === 'number' ? out.seq : undefined,
    };
  }

  // Preserve exact legacy property contract for say (omit key if undefined)
  if (out.kind === 'say') {
    return out.creationTaskId !== undefined
      ? { kind: 'say', text: out.text, creationTaskId: out.creationTaskId }
      : { kind: 'say', text: out.text };
  }

  return out;
}


// ── Host to Webview Schemas ──

export const PaintedRowSchema = v.custom<PaintedRow>((r) => {
  if (!r || typeof r !== 'object') return false;
  const rowObj = r as Record<string, unknown>;
  return (
    typeof rowObj.who === 'string' &&
    typeof rowObj.label === 'string' &&
    typeof rowObj.text === 'string' &&
    (rowObj.outputId === undefined || typeof rowObj.outputId === 'string')
  );
});

export function isAsk(val: unknown): val is Ask {
  if (!val || typeof val !== 'object' || Array.isArray(val)) return false;
  const o = val as Record<string, unknown>;
  if (typeof o.callId !== 'string' || !o.callId) return false;
  if (typeof o.what !== 'string') return false;
  if (o.kind !== 'permission' && o.kind !== 'question') return false;
  if (
    o.options !== undefined &&
    (!Array.isArray(o.options) || !o.options.every((opt) => typeof opt === 'string'))
  ) {
    return false;
  }
  if (
    o.report !== undefined &&
    (!Array.isArray(o.report) ||
      !o.report.every(
        (item) =>
          item &&
          typeof item === 'object' &&
          typeof (item as any).key === 'string' &&
          typeof (item as any).text === 'string'
      ))
  ) {
    return false;
  }
  if (o.args !== undefined && typeof o.args !== 'string') return false;
  if (o.reason !== undefined && typeof o.reason !== 'string') return false;
  if (o.diff !== undefined && typeof o.diff !== 'string') return false;
  if (
    o.diffKind !== undefined &&
    o.diffKind !== 'sides' &&
    o.diffKind !== 'patch' &&
    o.diffKind !== 'none'
  ) {
    return false;
  }
  if (o.filePath !== undefined && typeof o.filePath !== 'string') return false;
  if (o.index !== undefined && typeof o.index !== 'number') return false;
  if (o.total !== undefined && typeof o.total !== 'number') return false;
  if (o.since !== undefined && typeof o.since !== 'string') return false;
  return true;
}

export const NullableAskSchema = v.pipe(
  v.unknown(),
  v.transform((val) => (val === undefined || val === null ? null : val)),
  v.custom<Ask | null>((val) => val === null || isAsk(val)),
);

export const StringArrayRefSchema = v.custom<string[]>((arr) => {
  return Array.isArray(arr) && arr.every((r) => typeof r === 'string');
});

export const RowsMessageSchema = v.pipe(
  v.object({
    kind: v.literal('rows'),
    session: v.string(),
    rows: v.array(PaintedRowSchema),
    ask: v.optional(NullableAskSchema),
    refs: StringArrayRefSchema,
    companionKey: v.optional(v.unknown()),
    generation: v.optional(v.unknown()),
    webviewId: v.optional(v.unknown()),
  }),
  v.transform((m) => {
    const res: {
      kind: 'rows';
      session: string;
      rows: PaintedRow[];
      ask: Ask | null;
      refs: string[];
      companionKey?: string;
      generation?: number;
      webviewId?: string;
    } = {
      kind: 'rows',
      session: m.session,
      rows: m.rows,
      ask: m.ask ?? null,
      refs: m.refs,
    };
    if (typeof m.companionKey === 'string') res.companionKey = m.companionKey;
    if (typeof m.generation === 'number') res.generation = m.generation;
    if (typeof m.webviewId === 'string') res.webviewId = m.webviewId;
    return res;
  }),
);

export const ComposeMessageSchema = v.object({
  kind: v.literal('compose'),
  text: v.string(),
});

export const MentionsMessageSchema = v.pipe(
  v.object({
    kind: v.literal('mentions'),
    files: v.array(v.unknown()),
    reqId: v.custom<number>((input) => typeof input === 'number'),
    target: v.string(),
  }),
  v.transform((m) => ({
    kind: 'mentions' as const,
    files: m.files.filter((f): f is string => typeof f === 'string'),
    reqId: m.reqId,
    target: m.target,
  })),
);

export const SuggestionMessageSchema = v.object({
  kind: v.literal('suggestion'),
  text: v.string(),
  reqId: v.custom<number>((input) => typeof input === 'number'),
  target: v.string(),
});

export const SessionCreatedMessageSchema = v.object({
  kind: v.literal('sessionCreated'),
  companionKey: NonEmptyStringSchema,
  session: NonEmptyStringSchema,
  creationTaskId: NonEmptyStringSchema,
  webviewId: NonEmptyStringSchema,
});

export const SessionCreationFailedMessageSchema = v.pipe(
  v.object({
    kind: v.literal('sessionCreationFailed'),
    companionKey: NonEmptyStringSchema,
    creationTaskId: NonEmptyStringSchema,
    webviewId: NonEmptyStringSchema,
    error: v.optional(v.unknown()),
  }),
  v.transform((m): {
    kind: 'sessionCreationFailed';
    companionKey: string;
    creationTaskId: string;
    webviewId: string;
    error?: string;
  } => ({
    kind: 'sessionCreationFailed',
    companionKey: m.companionKey,
    creationTaskId: m.creationTaskId,
    webviewId: m.webviewId,
    error: typeof m.error === 'string' ? m.error : undefined,
  })),
);

export const ReplyResultMessageSchema = v.pipe(
  v.object({
    kind: v.literal('replyResult'),
    callId: NonEmptyStringSchema,
    attemptId: PositiveIntegerSchema,
    ok: v.boolean(),
    companionKey: NonEmptyStringSchema,
    session: NonEmptyStringSchema,
    generation: NonNegativeIntegerSchema,
    webviewId: NonEmptyStringSchema,
    error: v.optional(v.unknown()),
    text: v.optional(v.unknown()),
  }),
  v.transform((m): {
    kind: 'replyResult';
    callId: string;
    attemptId: number;
    ok: boolean;
    companionKey: string;
    session: string;
    generation: number;
    webviewId: string;
    error?: string;
    text?: string;
  } => ({
    kind: 'replyResult',
    callId: m.callId,
    attemptId: m.attemptId,
    ok: m.ok,
    companionKey: m.companionKey,
    session: m.session,
    generation: m.generation,
    webviewId: m.webviewId,
    error: typeof m.error === 'string' ? m.error : undefined,
    text: typeof m.text === 'string' ? m.text : undefined,
  })),
);

export const StateMessageSchema = v.pipe(
  v.object({
    kind: v.literal('state'),
    state: v.pipe(
      v.object({
        state: v.string(),
        asking: v.optional(v.unknown()),
        doing: v.optional(v.unknown()),
        progress: v.optional(v.unknown()),
      }),
      v.transform((s): Activity => {
        const a: Activity = {
          state: s.state as Activity['state'],
          asking: typeof s.asking === 'string' ? s.asking : undefined,
          doing: typeof s.doing === 'string' ? s.doing : undefined,
        };
        // Only when there is one: a message with no count keeps exactly the shape it always had.
        const progress = typeof s.doing === 'string' && s.doing ? progressOf(s.progress) : undefined;
        if (progress) a.progress = progress;
        return a;
      }),
    ),
    note: v.pipe(
      v.object({
        text: v.string(),
        offerStart: v.optional(v.unknown()),
      }),
      v.transform((n): PanelNoteInfo => ({
        text: n.text,
        offerStart: Boolean(n.offerStart),
      })),
    ),
  }),
  v.transform((m): {
    kind: 'state';
    state: Activity;
    note: PanelNoteInfo;
  } => ({
    kind: 'state',
    state: m.state,
    note: m.note,
  })),
);

export const InfoMessageSchema = v.pipe(
  v.object({
    kind: v.literal('info'),
    state: v.string(),
    label: v.string(),
    version: v.string(),
    model: v.optional(v.unknown()),
    backend: v.optional(v.unknown()),
    permission: v.optional(v.unknown()),
    council: v.optional(v.unknown()),
    socket: v.optional(v.unknown()),
  }),
  v.transform((m): {
    kind: 'info';
    state: string;
    label: string;
    version: string;
    model?: string;
    backend?: string;
    permission?: string;
    council?: string;
    socket?: string;
  } => ({
    kind: 'info',
    state: m.state,
    label: m.label,
    version: m.version,
    model: typeof m.model === 'string' ? m.model : undefined,
    backend: typeof m.backend === 'string' ? m.backend : undefined,
    permission: typeof m.permission === 'string' ? m.permission : undefined,
    council: typeof m.council === 'string' ? m.council : undefined,
    socket: typeof m.socket === 'string' ? m.socket : undefined,
  })),
);

/** The composer's ↑/↓ list — the `prompts` door's texts, newest first. */
export const PromptsMessageSchema = v.pipe(
  v.object({
    kind: v.literal('prompts'),
    texts: v.array(v.unknown()),
  }),
  v.transform((m) => ({
    kind: 'prompts' as const,
    texts: m.texts.filter((t): t is string => typeof t === 'string'),
  })),
);

export const NoteMessageSchema = v.object({
  kind: v.literal('note'),
  text: v.string(),
});

/** The plan (TodoWrite) for the strip at the top of the conversation — the whole list, last wins. */
export const PlanMessageSchema = v.pipe(
  v.object({
    kind: v.literal('plan'),
    todos: v.array(v.unknown()),
  }),
  v.transform((m) => ({
    kind: 'plan' as const,
    todos: m.todos
      .map((t) => (t ?? {}) as { content?: unknown; status?: unknown })
      .filter((t) => typeof t.content === 'string' && t.content)
      .map((t) => ({ content: String(t.content), status: String(t.status ?? '') })),
  })),
);

/**
 * The restart strip: what the last stop (a restart or a cut) left in this conversation, as the daemon's
 * recovery-state door says it. Null when there is nothing, or the daemon cannot say. A call without an id
 * is dropped - a verdict on it could not be sent anywhere.
 */
/** The restart state as the webview holds it: every field present, calls without an id dropped. */
export type RestartWire = {
  kind: string;
  reason: string;
  uncertain: { callId: string; tool: string; target: string; verdict: string }[];
};

export const RestartMessageSchema = v.pipe(
  v.object({ kind: v.literal('restart'), state: v.unknown() }),
  v.transform((m): { kind: 'restart'; state: RestartWire | null } => {
    const s = (m.state ?? null) as { kind?: unknown; reason?: unknown; uncertain?: unknown } | null;
    if (!s || typeof s !== 'object') return { kind: 'restart' as const, state: null };
    const str = (x: unknown): string => (typeof x === 'string' ? x : '');
    const calls = Array.isArray(s.uncertain) ? s.uncertain : [];
    return {
      kind: 'restart' as const,
      state: {
        kind: str(s.kind) || 'none',
        reason: str(s.reason),
        uncertain: calls
          .map((c) => (c ?? {}) as { callId?: unknown; tool?: unknown; target?: unknown; verdict?: unknown })
          .filter((c) => str(c.callId) !== '')
          .map((c) => ({ callId: str(c.callId), tool: str(c.tool), target: str(c.target), verdict: str(c.verdict) })),
      },
    };
  }),
);

export const HostToWebviewMessageSchema = v.union([
  RowsMessageSchema,
  ComposeMessageSchema,
  MentionsMessageSchema,
  SuggestionMessageSchema,
  SessionCreatedMessageSchema,
  SessionCreationFailedMessageSchema,
  ReplyResultMessageSchema,
  StateMessageSchema,
  InfoMessageSchema,
  NoteMessageSchema,
  PromptsMessageSchema,
  PlanMessageSchema,
  RestartMessageSchema,
]);

export type HostToWebviewMessage = v.InferOutput<typeof HostToWebviewMessageSchema>;

/**
 * Validates raw payload from host boundary into typed HostToWebviewMessage using Valibot schemas.
 * Returns parsed message if valid according to schema, otherwise undefined.
 */
export function parseHostToWebviewMessage(raw: unknown): HostToWebviewMessage | undefined {
  if (!raw || typeof raw !== 'object') return undefined;
  const res = v.safeParse(HostToWebviewMessageSchema, raw);
  return res.success ? res.output : undefined;
}

