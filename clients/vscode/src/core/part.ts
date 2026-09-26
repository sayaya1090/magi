import { Event } from './protocol';

/**
 * What a client reads out of one message part. Deliberately loose — every field optional, content
 * `unknown` — because the log carries whatever the core wrote, and a reader should skip what it
 * cannot use rather than trust a shape it never checked.
 *
 * The transcript fold and the output documents both read parts, and used to describe them apart:
 * this interface in one, `as any` chains and a hand-copied `toolResult` shape in the other.
 */
export interface PartLike {
  kind?: string;
  text?: string;
  /** `image` parts: where the picture is, and what it is. */
  image?: { path?: string; mime?: string };
  toolCall?: { callId?: string; name?: string; args?: unknown };
  toolResult?: { callId?: string; content?: unknown; isError?: boolean; advisory?: boolean };
  error?: string;
}

/** The part a `part.appended` event carries, or an empty one for any other event. */
export function partOf(e: Event | undefined): PartLike {
  if (!e || e.type !== 'part.appended') return {};
  const d = e.data as { part?: PartLike } | null | undefined;
  return d?.part ?? {};
}
