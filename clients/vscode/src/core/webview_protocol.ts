import * as v from 'valibot';
import { Row } from './transcript';
import { Ask } from './touched';
import { Activity } from './activity';

export type PaintedRow = Row & { label: string };

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

export const WebviewToHostMessageSchema = v.union([
  ReadyMessageSchema,
  StartMessageSchema,
  DropMessageSchema,
  SayMessageSchema,
  RunMessageSchema,
  DiffMessageSchema,
  OpenMessageSchema,
  OutputMessageSchema,
  AnswerMessageSchema,
  ReplyMessageSchema,
  MentionMessageSchema,
  SuggestMessageSchema,
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


// ── Host to Webview Messages ──

export type HostToWebviewMessage =
  | { kind: 'state'; state: Activity; note: PanelNoteInfo }
  | {
      kind: 'info';
      state: string;
      label: string;
      version: string;
      model?: string;
      backend?: string;
      permission?: string;
      council?: string;
      socket?: string;
    }
  | {
      kind: 'rows';
      session: string;
      rows: PaintedRow[];
      ask: Ask | null;
      refs: string[];
      companionKey?: string;
      generation?: number;
      webviewId?: string;
    }
  | { kind: 'compose'; text: string }
  | { kind: 'note'; text: string }
  | {
      kind: 'sessionCreated';
      companionKey: string;
      session: string;
      creationTaskId: string;
      webviewId: string;
    }
  | {
      kind: 'sessionCreationFailed';
      companionKey: string;
      creationTaskId: string;
      webviewId: string;
      error?: string;
    }
  | {
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
    }
  | { kind: 'mentions'; files: string[]; reqId: number; target: string }
  | { kind: 'suggestion'; text: string; reqId: number; target: string };

