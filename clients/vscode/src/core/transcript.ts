import { Event } from './protocol';

/**
 * Events into rows, in ONE place.
 *
 * Invariant 0-1: the rule that builds a conversation's rows is written once. Two copies drift on
 * whichever side is not measured, and this repository has already paid that bill.
 *
 * Invariant 0-2: draw shallowly. What to say is the daemon's decision; a client that parses
 * payloads to compose sentences composes them once per client, and there are six clients.
 */

export type Who = 'user' | 'agent' | 'tool' | 'thinking' | 'council' | 'system' | 'error' | 'image';

export interface Row {
  /** The event that put this row here, so a later frame can find it again. */
  seq: number;
  /**
   * A row built from streaming chunks, not yet written as a fact.
   *
   * It exists so the conversation moves WHILE the model answers. Without it this panel showed
   * nothing at all until the whole reply landed: `part.delta` carries each chunk and the
   * `part.appended` fact is only written once the stream finishes, so a long answer on a local
   * model was a frozen panel with a status bar that said "working".
   */
  draft?: boolean;
  who: Who;
  text: string;
  /** Tool rows only: the call this row is about, so its result can land on it. */
  callId?: string;
  /**
   * Tool rows only: what the call was ASKED to do, in one line.
   *
   * The name alone is not a row. A turn that runs thirty commands drew thirty rows reading
   * `bash ✓` — same glyph, same word, nothing saying which command, which file, which edit. The
   * transcript is the place a person answers "what did it just do", and it could not.
   *
   * The JetBrains client has carried this since it grew a tool row: it draws the name, a status
   * glyph, and a one-line summary of the arguments, with the full text a fold away. This is the
   * one-line half — the same fact, in the shape this screen has room for.
   */
  args?: string;
  /** Tool rows only, once the result arrives. Absent means still running. */
  ok?: boolean;
  /**
   * Tool rows only: it DID the work and left something the agent must read.
   *
   * A post-edit hook's output, a language server's complaint about the file just written. Those set
   * `isError` on purpose — that is what makes the model stop and act on them — and `isError` is
   * also what a screen draws its glyph from, so a file that was written and then linted drew as a
   * write that FAILED. The core measured that on a live run and split the two questions
   * (`session.ToolResult.Advisory`): "did the work happen" and "is there something to read" are
   * not one question. The JetBrains client has read the field since; this one had not declared it.
   */
  note?: boolean;
  /**
   * Council rows only: which round, and how that member voted (`done | continue | abstain`).
   *
   * The decision IS the verdict — a row that carries only the prose says who spoke and what they
   * said, and leaves out the one thing a vote is. And three rounds of three members is nine rows
   * that look alike unless the round is on them. The JetBrains client carries both.
   */
  round?: number;
  decision?: string;
  /**
   * This verdict was never given — backend down, deadline, an unreadable reply.
   *
   * It rides beside `decision: "abstain"` so a surface can say "no answer" where a member never
   * spoke, instead of reporting a failure as a considered abstention (the core's own words). The
   * row TEXT already used it; the row LABEL did not, so the label said `abstain` — a claim about
   * deliberation that never happened.
   */
  silent?: boolean;
  /**
   * The judging lens this seat holds — `correctness`, `verification`, `completeness`.
   *
   * It is WHY a council has three seats. Without it three verdicts in a round are interchangeable
   * names, and "two said done" carries no information about what was and was not examined.
   */
  lens?: string;
  /**
   * The round's own threshold, from `council.convened` — "majority", "unanimous", and so on.
   *
   * Kept apart from `lens`, which belongs to a SEAT: the rule governs how the seats add up, and one
   * field for both would make a verdict look as if it carried the threshold.
   */
  rule?: string;
  /** True on the row that OPENS a round — the convened row, not a verdict. */
  opened?: boolean;
  /**
   * The fragment of the record this verdict says it rests on, or `NO-EVIDENCE`.
   *
   * The core records it because it is CHECKABLE — magi looks the fragment up in the material the
   * member was shown — and says the part a reader needs most plainly: "an empty one on a `done` is
   * itself worth seeing". A screen that drops it turns a vote standing on nothing into a vote.
   */
  cite?: string;
  /**
   * What this member says a revision must preserve.
   *
   * Emitted regardless of the decision, and the core says why: an APPROVING member's keep is
   * "precisely what a rewrite forced by another member's objection would otherwise drop".
   */
  keep?: string;
  /**
   * A prompt the core PARKED: typed while a turn was running, and it will run as its own turn when
   * this one ends (`interjection.deferred`).
   *
   * Distinct from `pending`, which means "asked and not answered yet". Both draw a bar, and without
   * this the parked message looked exactly like one the agent was already working on — so a person
   * watched a sentence sit there and could not tell whether it had been picked up or shelved. The
   * core has a whole ledger for this state (F5) and this client read none of it.
   */
  queued?: boolean;
  /**
   * Tool rows only: WHY it failed, as the tool said it.
   *
   * A row that draws ✗ and nothing else tells a person the shape of the trouble and none of its
   * content — and the transcript is where they go to find out. Kept only for a real failure: an
   * advisory result's text is what the AGENT must act on, and putting it here would draw a
   * successful write in the colours of a broken one.
   */
  out?: string;
  /** A row whose prompt has no answer yet — the screen draws a bar beside it. */
  pending?: boolean;
  /**
   * A prompt whose turn was cancelled.
   *
   * ⚠ Without this the bar never comes down. `pending` is cleared by an assistant part or by
   * `turn.finished`, and an interrupted prompt gets neither — so the row kept claiming "waiting for
   * an answer" for the life of the window, about a request that had been stopped. A standing claim
   * that has gone false is the defect shape this tree names "sentences that age".
   */
  abandoned?: boolean;
  /**
   * The prompt's own id, so a later event can find its row.
   *
   * ⚠ `prompt.submitted` spells it `messageId` and `prompt.abandoned` spells it `msgId` — the same
   * id under two names. Reading either one for the other finds nothing and marks nothing, silently.
   */
  msgId?: string;
  /** Council rows: which member said it. Only three names ever get a colour. */
  member?: string;
  /** Rows that are folded shut by default (reasoning, tool bodies). */
  folded?: boolean;
}

interface PartLike {
  kind?: string;
  text?: string;
  /** `image` parts: where the picture is, and what it is. */
  image?: { path?: string; mime?: string };
  toolCall?: { callId?: string; name?: string; args?: unknown };
  toolResult?: { callId?: string; content?: unknown; isError?: boolean; advisory?: boolean };
  error?: string;
}

/**
 * Fold a log into rows.
 *
 * Not every event becomes a row, and the ones that do not are as deliberate as the ones that do:
 * `context.usage` and `todos.changed` are facts for other screens, and putting them in the
 * transcript would make the conversation a log file.
 */
export function rows(events: Event[]): Row[] {
  const out: Row[] = [];
  const answered = new Set<number>();
  // Streaming chunks, keyed by the message and kind they belong to. A draft is REPLACED by the fact
  // when it arrives rather than added to — the appended part carries the whole text, so keeping both
  // would show the answer twice.
  const drafts = new Map<string, Row>();
  const dropDraft = (key: string): void => {
    const row = drafts.get(key);
    if (!row) return;
    drafts.delete(key);
    const at = out.indexOf(row);
    if (at >= 0) out.splice(at, 1);
  };
  for (const e of events) {
    const d = (e.data ?? {}) as Record<string, unknown>;
    switch (e.type) {
      case 'part.delta': {
        const kind = String(d.kind ?? 'text');
        const text = String(d.text ?? '');
        if (!text || (kind !== 'text' && kind !== 'reasoning')) break;
        const key = `${String(d.messageId ?? '')}:${kind}`;
        let row = drafts.get(key);
        if (!row) {
          row = { seq: e.seq, who: kind === 'reasoning' ? 'thinking' : 'agent', text: '',
            draft: true, folded: kind === 'reasoning' };
          drafts.set(key, row);
          out.push(row);
          // A chunk is an answer to whatever is above it, the same as any assistant part. Without
          // this the person's own row keeps its pending bar for the whole of a streamed reply.
          for (const r of out) if (r.pending && !answered.has(r.seq)) { r.pending = false; answered.add(r.seq); }
        }
        row.text += text;
        break;
      }
      case 'prompt.submitted': {
        const parts = (d.parts as PartLike[] | undefined) ?? [];
        const text = parts.map((p) => p.text ?? '').join('').trim();
        if (!text) break;
        /**
         * ⚠ **Not every prompt is the person's.** The core signs each one, and this client read
         * none of it — so anything the daemon submitted came out wearing the person's name.
         *
         * Measured by streaming a real conversation off a live daemon (2026-09-10): two
         * `prompt.submitted` events, one `actor.kind: "user"` and one
         * `actor.kind: "system", id: "orchestrator"` carrying "You stopped without saying you are
         * finished". This transcript showed the person saying that. They never typed it.
         *
         * - `agent` — a subagent's report, injected back. The body belongs to that child's own
         *   transcript; repeating it here is noise the terminal also swallows.
         * - `system` — a planner or council note. Worth a line, because without it this window
         *   shows LESS than the headless printer does, and that was measured on the terminal. One
         *   line only: the whole of it is in the log, and a note must not push the conversation out.
         *
         * The JetBrains shaper has split these three since it was written; this one had one branch.
         */
        const kind = String(e.actor?.kind ?? '');
        if (kind === 'agent') break;
        if (kind === 'system') {
          const who = String(e.actor?.id ?? 'system');
          out.push({ seq: e.seq, who: 'system', text: `⟳ ${who} note: ${text.split('\n')[0]}` });
          break;
        }
        const id = String(d.messageId ?? '');
        /**
         * ⚠ **A resurfaced interjection is the SAME question, not a second one.**
         *
         * Something typed while a turn was running is queued; the drain later re-runs it as its own
         * turn by emitting a FRESH prompt with a new id, and `resurfacedFrom` carries the id of the
         * original. The core wrote that field for this reader — its words: it "lets the display
         * layer pair the query with its answer — dropping the stranded original on replay and
         * pulling the live bubble down to just above the answer".
         *
         * This client read neither. So the question stayed stranded far up the transcript wearing a
         * queued mark that never cleared, a second copy of it appeared at the bottom, and nothing
         * said they were one thing. The JetBrains client has done this since the field landed.
         *
         * MOVED, not deleted and re-pushed: the original row carries its own history (queued, and
         * whatever else marked it) and re-creating it throws that away.
         */
        const from = String(d.resurfacedFrom ?? '');
        if (from) {
          const i = lastUserRow(out, from);
          if (i >= 0) {
            const moved = { ...out[i], text, queued: false, pending: true, msgId: id || from, seq: e.seq };
            out.splice(i, 1);
            out.push(moved);
            break;
          }
        }
        out.push({ seq: e.seq, who: 'user', text, pending: true, msgId: id });
        break;
      }
      case 'part.appended': {
        const p = (d.part ?? {}) as PartLike;
        const role = String(d.role ?? '');
        // The fact replaces its own draft. Keyed by message AND kind, because one message streams
        // reasoning and text as two drafts and only one of them is being written here.
        if (p.kind === 'text' || p.kind === 'reasoning') dropDraft(`${String(d.messageId ?? '')}:${p.kind}`);
        // Any assistant part answers the prompts above it. The mark travels on the ROW rather than
        // being recomputed at draw time: a screen that re-derived it would have to hold the whole
        // log to draw one row.
        if (role === 'assistant' || role === 'tool') {
          for (const r of out) if (r.pending && !answered.has(r.seq)) { r.pending = false; answered.add(r.seq); }
        }
        /**
         * ⚠ **An inline answer pulls its question down to it.**
         *
         * A queued or mid-turn message can be answered INLINE — no fresh prompt is emitted, so
         * `inReplyTo` is, in the core's own words, "the only link the display layer has to pair the
         * answer with its question and pull the stranded question bubble down just above the
         * answer". Without it the question sits where it was typed, minutes of transcript above the
         * reply, still marked queued.
         */
        const replyTo = String(d.inReplyTo ?? '');
        if (role === 'assistant' && replyTo) {
          const q = lastUserRow(out, replyTo);
          if (q >= 0) out.push({ ...out.splice(q, 1)[0], queued: false });
        }
        if (p.kind === 'text' && (p.text ?? '').trim()) {
          out.push({ seq: e.seq, who: 'agent', text: p.text!.trim() });
        } else if (p.kind === 'reasoning' && (p.text ?? '').trim()) {
          out.push({ seq: e.seq, who: 'thinking', text: p.text!.trim(), folded: true });
        } else if (p.kind === 'tool-call' && p.toolCall) {
          out.push({ seq: e.seq, who: 'tool', text: p.toolCall.name ?? 'tool',
            callId: p.toolCall.callId, args: askedFor(p.toolCall.args) });
        } else if (p.kind === 'tool-result' && p.toolResult) {
          // The result lands ON the call's row rather than starting a new one — one call, one line.
          const call = p.toolResult.callId;
          const row = [...out].reverse().find((r) => r.who === 'tool' && r.callId === call);
          if (row) {
            // Two questions, not one — see Row.note. An advisory result DID the work.
            const advisory = p.toolResult.advisory === true;
            row.ok = !p.toolResult.isError || advisory;
            if (advisory) row.note = true;   // set only when true, like every other row flag
            // The reason travels with the failure. Read the VALUE, not its rendering: `content` is
            // often a JSON string, and stringifying it again leaves the escapes on the screen.
            if (p.toolResult.isError && !advisory) row.out = said(p.toolResult.content);
          }
        } else if (p.kind === 'image' && p.image?.path) {
          // ⚠ **A part kind this fold does not name is not an empty row — it is a row that never
          // existed, with nothing anywhere saying so.** The web console hit exactly this and its
          // comment says so: an image and an error both reached the log and neither reached the
          // page. `error` was handled here; `image` was not, so a tool that answered with a picture
          // produced nothing at all on this screen.
          //
          // The path, not the picture. A webview cannot read a file off disk without being handed a
          // URI for it, and drawing nothing while we work that out is the defect being fixed. A path
          // a person can open is the honest minimum.
          out.push({ seq: e.seq, who: 'image', text: p.image.path });
        } else if (p.kind === 'error' && (p.error ?? '').trim()) {
          out.push({ seq: e.seq, who: 'error', text: p.error!.trim() });
        }
        break;
      }
      case 'error': {
        const msg = String(d.message ?? '').trim();
        // A recovered error is not an ending, and the payload says which. Dropping that distinction
        // would make every retry look like a failure.
        if (msg) out.push({ seq: e.seq, who: 'error', text: d.recovered ? `${msg} (recovered)` : msg });
        break;
      }
      /**
       * The gate's own answer — what the council DECIDED, not what one member said.
       *
       * Without it the transcript showed three people voting and never what came of it: the row
       * for the conclusion simply did not exist. `note` is the outcome in words, `decision` is
       * `done | continue`, and `feedback` is what is holding a `continue` up — which is the one
       * thing a person reads a stalled gate to find out.
       */
      case 'council.decided': {
        out.push({
          seq: e.seq, who: 'council',
          text: String(d.note ?? '').trim() || String(d.feedback ?? '').trim(),
          round: Number(d.round) || undefined,
          decision: String(d.decision ?? '').trim() || undefined,
        });
        break;
      }
      /**
       * The core parked this prompt — see Row.queued. Not a row of its own: it is a fact ABOUT a
       * row that is already there, and a second row would say the person typed twice.
       */
      case 'interjection.deferred': {
        /**
         * ⚠ **One event type carries BOTH ends of this state.**
         *
         * The core writes this fact twice for the same message: `resolved:false` when the prompt is
         * queued as an interjection, and `resolved:true` when it later LEAVES the queue — absorbed
         * inline, routed, or abandoned. Five of the seven call sites write `true`.
         *
         * This client read the type and ignored the field, so every un-parking marked the row
         * PARKED. The bar arrived at the moment it should have gone, and because it arrives after
         * whatever cleared it (`interjection.answered`, an inline answer), the wrong word is the
         * last one — a standing claim that has gone false, which is the shape this tree keeps
         * paying for.
         *
         * ⚠ `Resolved` is a Go bool with `omitempty`: FALSE NEVER GOES ON THE WIRE. The parked case
         * is the one with no field at all, so the test is "not true", never "is false".
         *
         * Only `queued` moves. Leaving the queue is not being answered — `interjection.answered`
         * and the reply itself own `pending`, and clearing it here would call an abandoned
         * interjection answered.
         */
        const id = String(d.messageId ?? '');
        const parked = d.resolved !== true;
        for (const r of out) if (r.who === 'user' && r.msgId === id) r.queued = parked;
        break;
      }
      /**
       * The agent says its answer already covered the parked message. The bar comes down —
       * "in its place" and "still waiting" are not the same thing, which the terminal measured.
       */
      case 'interjection.answered': {
        const id = String(d.messageId ?? '');
        for (const r of out) if (r.who === 'user' && r.msgId === id) { r.queued = false; r.pending = false; }
        break;
      }
      case 'council.convened': {
        // ⚠ **This was skipped, and the reason given for skipping it was not true.** The note said
        // the round "announces itself through the verdicts it produces, and the evidence it carries
        // is the plan panel's" — and nothing in this client reads `council.convened` at all: not the
        // transcript, and not the plan panel the reason points at. Measured by folding a live
        // conversation through both clients' shapers: 54 rows here against 59 there, and every one of
        // the five missing was this event.
        //
        // What is dropped with it is the round's THRESHOLD. Three verdicts arrive — two `continue`,
        // one `done` — and without the rule a reader cannot tell whether that outcome needed a
        // majority or all three. The event carries `task`, `members`, `plan` and `changes` too; those
        // stay unread, and the exemption note now says so honestly instead of naming a reader that
        // does not exist.
        const members = Array.isArray(d.members) ? d.members.map(String) : [];
        out.push({
          seq: e.seq, who: 'council', opened: true,
          text: String(d.task ?? '').trim() || (members.length ? members.join(', ') : 'a round opened'),
          round: Number(d.round) || undefined,
          rule: String(d.rule ?? '').trim() || undefined,
        });
        break;
      }
      case 'council.verdict': {
        // ⚠ **A verdict always makes a row.** This used to push one only when there was prose, so a
        // member who voted with nothing to add vanished — and a council of three drew as a council
        // of two, with nothing saying a seat was missing. The core has a word for the empty case
        // (`silent`: nobody gave this verdict — backend down, deadline, unreadable reply), and that
        // is a fact worth a row of its own, not an absence.
        //
        // The prose that DID arrive is never dropped. The JetBrains client's comment records the
        // live report behind that rule — a `silent: true` verdict arrived with a full rationale and
        // its shaper drew the fallback words instead of the ones that came. So: rationale first
        // whenever it is there, and the fallback only for a genuinely empty one.
        const text = String(d.feedback ?? d.rationale ?? '').trim()
          || (d.silent === true ? 'no answer came back' : '');
        out.push({
          seq: e.seq, who: 'council', text, member: String(d.member ?? ''),
          round: Number(d.round) || undefined,
          decision: String(d.decision ?? '').trim() || undefined,
          silent: d.silent === true || undefined,
          lens: String(d.lens ?? '').trim() || undefined,
          cite: String(d.cite ?? '').trim() || undefined,
          keep: String(d.keep ?? '').trim() || undefined,
        });
        break;
      }
      /**
       * The conversation was folded here.
       *
       * ⚠ Without this row the transcript just STOPS earlier than a person remembers, with nothing
       * saying why. The fold replaces everything up to a point with a summary, and a reader scrolling
       * back finds a gap and no explanation of it.
       *
       * The size is stated the way the core states it, `SizeNote`'s own form — including the case
       * that reads backwards: a summary can come out LARGER than what it replaced, and saying
       * "−0, −0%" there would hide the one outcome worth noticing.
       */
      /**
       * That prompt's turn was cancelled.
       *
       * The bar comes down and the row says so. Nothing else ever clears it: `pending` goes when an
       * assistant part answers or the turn finishes, and an interrupted prompt gets neither — so the
       * row went on claiming it was waiting, about a request that had been stopped.
       */
      case 'prompt.abandoned': {
        // `msgId` here, `messageId` on the prompt. Same id, two spellings, no error either way.
        const id = String(d.msgId ?? '');
        if (!id) break;
        for (const r of out) {
          if (r.who === 'user' && r.msgId === id) { r.pending = false; r.queued = false; r.abandoned = true; }
        }
        break;
      }
      case 'compaction': {
        const before = Number(d.tokensBefore ?? 0);
        const after = Number(d.tokensAfter ?? 0);
        out.push({ seq: e.seq, who: 'system', text: `↯ folded the conversation: ~${before}→${after} tok (${sizeNote(before, after)})` });
        break;
      }
      case 'turn.finished':
        /**
         * ⚠ **A turn that could not be verified is not a turn that finished.**
         *
         * The core sets `unverified` when the execution-evidence gate could not confirm the outcome:
         * a top-level turn changed a deliverable and no independent run passed for the CURRENT
         * version, so the declared result — success OR "impossible" — is not backed by execution.
         * Its own words for why the flag exists: the turn is "labeled UNVERIFIED rather than
         * laundered into a confident success".
         *
         * This client laundered it. The turn ended, the pending mark cleared, and a finish nobody
         * could confirm drew exactly like one that was — which is the whole thing the flag is for.
         * The terminal has surfaced it since the flag landed (`⚠ Unverified`, `model_view.go`).
         *
         * A row rather than a mark on the last row: the fact belongs to the TURN, and the last row
         * may be a tool call or a council seat that had nothing to do with the deliverable. `reason`
         * carries the short cause and is written when there is one — without it the receipt says
         * something is wrong and gives no handle on what.
         */
        if (d.unverified === true) {
          const why = String(d.reason ?? '').trim();
          out.push({ seq: e.seq, who: 'system',
            text: '⚠ Unverified — nothing ran to confirm this' + (why ? `: ${why}` : '') });
        }
        // Not a row. It ends the turn, and the screen reads that from the LAST row's pending mark.
        for (const r of out) r.pending = false;
        // ⚠ **Sweep the orphan drafts.** There are several paths where the core streams chunks and
        // never writes the fact — a reply the spin guard discarded, a tool call that arrived as
        // text, an interrupt or a provider error, a failed interjection mini-turn. The JetBrains
        // client's own comment counts five. Left standing, a half-answer sits on the screen of the
        // window that happened to be attached and nowhere else, which is the very split this is
        // meant to prevent.
        for (const key of [...drafts.keys()]) dropDraft(key);
        break;
      default:
        break; // facts for other screens (context.usage, todos.changed, labels.changed, …)
    }
  }
  return out;
}

/** The three council seats that get a colour. Nobody else does — a fourth name is not a seat. */
export const SEATS = ['melchior', 'balthasar', 'casper'];

export function seat(member: string | undefined): string | null {
  const m = (member ?? '').toLowerCase();
  return SEATS.includes(m) ? m : null;
}

/**
 * How many turns back a given prompt is — what the `rewind` door wants.
 *
 * ⚠ **The door counts TURNS, not sequence numbers.** It reads `n` and passes it to
 * `App.Rewind(sid, n)`, whose own comment is about counting genuine user prompts. This client sent
 * the picked row's `seq` in a field called `since`, which the door does not read at all: the daemon
 * got n=0 and the person's chosen point had nothing to do with what happened. The JetBrains client
 * sends `n = 1` and has always been right.
 *
 * "Back to just before this prompt" drops that prompt AND everything after it, so the count includes
 * the picked one: the newest prompt is 1, the one before it is 2.
 *
 * 0 means the seq is not one of these rows — a caller must not send that as "rewind nothing", because
 * the door would take it as its own default.
 */
export function turnsBack(asked: Row[], seq: number): number {
  const i = asked.findIndex((r) => r.seq === seq);
  return i < 0 ? 0 : asked.length - i;
}

/** One item of the agent's plan. */
export interface Todo {
  content: string;
  status: string;
}

/**
 * The plan, read off the same stream the rows come from.
 *
 * ⚠ **The panel is called Plan and had no plan in it.** The UI design promises "todo and its state"
 * and the panel drew now/context/jobs/scheduled/fleet — five dials and no list. The JetBrains client
 * reads `todos.changed` for exactly this; this one ignored the event, so nothing failed and nothing
 * appeared.
 *
 * The LAST event wins rather than deltas being replayed: `todos.changed` carries the whole list every
 * time, and a reader that accumulated would be wrong from the first event. It is a FACT type, so it
 * rides the replay — a window that reattaches learns the current plan without a door of its own,
 * which is why no door was ever needed.
 */
export function todos(events: Event[]): Todo[] {
  let out: Todo[] = [];
  for (const e of events) {
    if (e.type !== 'todos.changed') continue;
    const list = ((e.data ?? {}) as { todos?: unknown[] }).todos ?? [];
    out = list
      .map((t) => (t ?? {}) as { content?: unknown; status?: unknown })
      .filter((t) => typeof t.content === 'string' && t.content)
      .map((t) => ({ content: String(t.content), status: String(t.status ?? '') }));
  }
  return out;
}

/** The plan as lines, with the mark a person reads the state by. */
export function planLines(list: Todo[]): string {
  const mark = (s: string): string =>
    s === 'completed' ? '✓' : s === 'in_progress' ? '◐' : '☐';
  return list.map((t) => `${mark(t.status)} ${t.content}`).join('\n');
}

/**
 * How much a fold shed, in the core's own words (`CompactionData.SizeNote`).
 *
 * The larger-than-before case is stated rather than clamped: a summary that came out bigger than
 * what it replaced is the one outcome a person should see, and rendering it as "−0, −0%" would hide
 * it. The percent rounds to nearest the way the core rounds it.
 */
export function sizeNote(before: number, after: number): string {
  if (after > before) return `+${after - before}, the summary is LARGER than what it replaced`;
  const freed = Math.max(0, before - after);
  const pct = before > 0 ? Math.round((freed * 100) / before) : 0;
  return `−${freed}, −${pct}%`;
}

/**
 * Is a turn open right now — the fact the composer needs to pick its door.
 *
 * `submit` and `steer` are not two spellings of one door. `submit` is a NEW top-level request, so
 * the core runs `resetForNewTopLevel`: the plan is emptied, the turn notes and the completion gate
 * are wound back. Doing that to a turn already running means a person who typed one clarifying
 * sentence has just deleted that turn's plan.
 *
 * Read off the transcript this client is already streaming, because that is where the fact is.
 * The `status` door has no field meaning "a turn is running" — `waiting` means blocked on a person
 * and `doing` is a minutes-long tool's progress note, which exactly one builtin tool file out of
 * fifty ever writes (`wait_for`, measured 2026-09-09). Asking it would answer "idle" for nearly
 * every running turn.
 *
 * The rule is the one the screen already draws with: a user row still marked `pending` is a
 * question with no answer under an unfinished turn.
 */
/**
 * A council verdict as the other surfaces already say it.
 *
 * `council.Decision` is three words, and one of them reads as its own opposite: `continue` means
 * "not done, more work is needed", and a row that prints it says the vote let the work proceed.
 * It is the gate on ending the turn — the work cannot pass it.
 *
 * The core found this and fixed it TWICE somewhere else. The terminal has said "reject" since it
 * drew its first verdict (`internal/adapter/tui/render.go`, `councilVerdictLabel`), and the web
 * console carries a test named `TestAContinueVoteReadsAsTheRejectionItIs` whose comment reads:
 * "The page printed the raw word in a neutral colour, which reads as progress — the opposite of
 * what the vote means." This client printed the raw word.
 *
 * `silent` is a fourth OUTCOME and not a fourth decision: a verdict nobody gave arrives as
 * `abstain` so the tally does not count it, and drawing that as an abstention reports a backend
 * failure as a member weighing the work and declining.
 *
 * ⚠ An unknown decision passes through raw, with the neutral mark the terminal uses.
 */
export function verdictWord(decision: string | undefined, silent?: boolean): { icon: string; word: string } {
  if (silent) return { icon: '⋯', word: 'no answer' };
  switch ((decision ?? '').trim()) {
    case '': return { icon: '', word: '' };
    case 'done': return { icon: '✓', word: 'done' };
    case 'continue': return { icon: '✗', word: 'reject' };
    case 'abstain': return { icon: '∅', word: 'abstain' };
    default: return { icon: '·', word: (decision ?? '').trim() };
  }
}

/**
 * The last row that is this person's message with this id, or -1.
 *
 * Written out rather than `findLastIndex` because this package's lib target predates it, and
 * raising the target to reach one call would change what every other file may compile against.
 */
function lastUserRow(out: Row[], msgId: string): number {
  for (let i = out.length - 1; i >= 0; i--) {
    if (out[i].who === 'user' && out[i].msgId === msgId) return i;
  }
  return -1;
}

export function turnOpen(events: Event[]): boolean {
  return rows(events).some((r) => r.pending);
}

/**
 * A tool call's arguments as one line, for the row that names the call.
 *
 * The fields a person recognises come first — `path`, `command`, `pattern`, `id` are what every
 * builtin's schema calls the thing it acts on — and anything else falls back to the raw JSON,
 * clipped. Clipped rather than dropped: a call whose argument is a whole file's contents still has
 * a first line worth reading, and "no summary" is the state this exists to end.
 *
 * Nothing is invented. An empty object summarises to nothing, and the row is then the bare name
 * again — which is the truth about a call that was given no arguments.
 */
export function askedFor(args: unknown): string | undefined {
  if (args === null || args === undefined) return undefined;
  let o: Record<string, unknown>;
  if (typeof args === 'string') {
    try { o = JSON.parse(args) as Record<string, unknown>; } catch { return clip(args); }
  } else if (typeof args === 'object') {
    o = args as Record<string, unknown>;
  } else {
    return clip(String(args));
  }
  for (const k of ['path', 'command', 'pattern', 'query', 'id', 'name']) {
    const v = o[k];
    if (typeof v === 'string' && v.trim()) return clip(v.trim());
  }
  const rest = JSON.stringify(o);
  return rest && rest !== '{}' ? clip(rest) : undefined;
}

/** One line, bounded. A row is a line — a summary that wraps is not a summary. */
function clip(s: string): string {
  const line = s.split('\n')[0].trim();
  return line.length > 100 ? line.slice(0, 100) + '…' : line;
}

/** A tool result's content as words. A JSON string is its own text; anything else is its JSON. */
function said(content: unknown): string | undefined {
  if (content === null || content === undefined) return undefined;
  const t = typeof content === 'string' ? content : JSON.stringify(content);
  return t && t.trim() ? clip(t) : undefined;
}
