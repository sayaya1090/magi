/**
 * What the companion is asked for at the cursor, and what comes back.
 *
 * Prefix AND suffix, both. Sending only what is behind the cursor makes the model close a bracket
 * the file already closes — the JetBrains client sends both for exactly that reason and says so.
 */
export interface CompleteArgs {
  prefix: string;
  suffix: string;
}

/**
 * How much of the buffer travels, per side.
 *
 * One owner for the number: the provider needs it to bound its READ, and `around` needs it to bound
 * what it sends. Two copies would drift and the larger one would silently win.
 */
export const WINDOW = 4000;

/**
 * Bounded on each side. The window either side of a cursor is what helps; the file is not.
 *
 * ⚠ **Takes a READER, not the whole buffer.** It used to take the text, and the one caller obtained
 * that text with `doc.getText()` — the entire document, on every pause in typing, so a 40,000-line
 * file was copied whole and then all but 8,000 characters of it thrown away. The sentence above was
 * true of what was SENT and false of what was read. The core makes the same point about the cost
 * ("the buffer travels on every pause in typing"), and the JetBrains client had the same shape in
 * the same place.
 *
 * A reader keeps the arithmetic here, where it is tested, and lets the caller hand over only the
 * slice its editor can produce cheaply.
 */
export function around(
  read: (from: number, to: number) => string,
  offset: number,
  budget = WINDOW,
): CompleteArgs {
  const at = Math.max(0, offset);
  return {
    prefix: read(Math.max(0, at - budget), at),
    suffix: read(at, at + budget),
  };
}

/**
 * What of a completion is worth showing.
 *
 * A model that answers with the line it was given is answering nothing, and a ghost that repeats
 * what is already there reads as the editor being stuck. Empty means "draw nothing".
 */
export function usable(out: string, prefix: string): string {
  const t = out.replace(/\r/g, '');
  if (!t.trim()) return '';
  // It sometimes re-emits the tail of the prefix. Drop that overlap rather than drawing it twice.
  const tail = prefix.slice(-200);
  for (let n = Math.min(tail.length, t.length); n > 0; n--) {
    if (tail.endsWith(t.slice(0, n))) return t.slice(n);
  }
  return t;
}

/**
 * Why the last completion came back empty, if it did.
 *
 * The door answers ok with nothing when it has nothing to say — that is the ordinary case, so it
 * cannot be an error — and it puts the reason in `reason`. Saying it on every keystroke would be
 * noise; saying it nowhere makes "why is completion silent" unanswerable, which is the state this
 * client was in. So it is REMEMBERED, and the one screen a person opens when something is not
 * working reads it.
 *
 * ⚠ **A completion that produced text clears it.** A reason left standing while things work is a
 * sentence that has aged — the failure mode this tree keeps paying for.
 */
let lastEmpty = '';

export function noteCompletion(text: string, reason: string | undefined): void {
  lastEmpty = text.trim() ? '' : (reason ?? '').trim();
}

export function whyNoCompletion(): string { return sayWhyEmpty(lastEmpty); }

/** The raw code, for a caller that wants to branch on it rather than read it. */
export function whyCodeNoCompletion(): string { return lastEmpty; }

/**
 * The reason as a sentence, because `reason` is an ENUM and not prose.
 *
 * `CompleteReason` in `internal/app/complete.go` is four tokens — `off`, `unrouted`,
 * `nothing-asked`, `no-answer` — and this client was putting the token itself on screen:
 * "Completion said nothing: unrouted". That is a protocol word shown to a person, in the one row
 * they open when completion is silent, under a description that calls it "the companion's own
 * reason". It is not the companion's reason; it is the companion's constant.
 *
 * `unrouted` is the one that costs something. The core's own comment says it is "the one most
 * worth surfacing: it is indistinguishable from a model with nothing to say, and unlike that model
 * it will never have anything to say" — a configuration mistake that never fixes itself. The word
 * "unrouted" does not tell anyone to go and pick a code profile. The JetBrains client translates
 * all four; this one translated none.
 *
 * ⚠ **An unknown code passes through raw.** A daemon newer than this build may name a fifth kind of
 * empty, and showing its word is better than inventing a sentence for it or swallowing it — the
 * same rule this tree applies to permission modes it does not know.
 */
export function sayWhyEmpty(code: string): string {
  switch (code.trim()) {
    case '': return '';
    case 'off': return 'code completion is switched off';
    case 'unrouted': return 'no code profile is routed — pick one in the companion\'s settings';
    case 'nothing-asked': return 'there was nothing around the cursor to complete';
    case 'no-answer': return 'the completer was asked and offered nothing';
    default: return code.trim();
  }
}
