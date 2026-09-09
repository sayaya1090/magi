/**
 * What the companion said after looking over a buffer, split into what hangs on a line and what
 * does not.
 *
 * The contract is `<line><TAB><what is wrong>`, one per line: the client sends the buffer with
 * absolute line numbers prefixed and the reply keeps that shape (`internal/app/git.go`, LookOver).
 *
 * **But replies that do not keep it also arrive** — a single sentence about the whole file, or a
 * remark with no line to hang on. There is no number to invent for those, so they stay loose and
 * the screen puts them somewhere else (a banner rather than an inlay).
 */
export interface Look {
  /** [line, what] — one-based, as the model was shown. */
  anchored: [number, string][];
  /** Everything with no line to hang on, joined. */
  loose: string;
}

/**
 * ⚠ **The separator is not only a tab.** The contract says tab, and the JetBrains client measured
 * models sending a space or a colon instead — live, `5broken link missing colon` was read as a
 * loose remark and drew a banner where an inlay belonged, and a person reported it as "it shows up
 * in the same blue box".
 *
 * Reading widely is safe here for a reason worth stating: a number read wrongly is a number past
 * the end of the file, and the side that hangs these gives such a remark BACK as loose. Generous
 * in, never invented out.
 */
const HEAD = /^\s*(\d{1,6})\s*[\t:.)\-]?\s*(.*)$/;

/**
 * Move findings whose line is not in the file into the loose text, instead of losing them.
 *
 * ⚠ **They were dropped.** The placement step skipped any number past the end of the buffer, with a
 * comment calling that what makes generous parsing safe — and a dropped finding is a finding the
 * person never sees. Three findings come back at most; losing one leaves a screen identical to
 * "nothing worth saying", which is the reading this whole path is built to avoid.
 *
 * The JetBrains client resolves the same tension the other way and says so: a number outside the
 * file "거는 쪽이 못 걸고 그 말을 그대로 띠로 돌려보낸다 — 관대하게 읽되 지어내지는 않는 자리다".
 * Generous in, nothing invented out, and nothing lost either.
 *
 * `lines` is the buffer's line count. A finding at line 0 or below is out too — the numbers the
 * prompt asks for start at 1.
 */
export function place(found: Look, lines: number): Look {
  const anchored: [number, string][] = [];
  const stray: string[] = [];
  for (const [n, text] of found.anchored) {
    if (n >= 1 && n <= lines) anchored.push([n, text]);
    // Kept WITH its number: the person can still find the place the model meant, and a bare clause
    // with no line would read as being about the file as a whole.
    else stray.push(`${n}: ${text}`);
  }
  const loose = [found.loose, ...stray].filter(Boolean).join('\n');
  return { anchored, loose };
}

export function split(out: string): Look {
  const anchored: [number, string][] = [];
  const loose: string[] = [];
  for (const raw of out.split('\n')) {
    const line = raw.trim();
    if (!line) continue;
    const m = HEAD.exec(line);
    const n = m ? Number.parseInt(m[1], 10) : NaN;
    const text = m ? m[2].trim() : '';
    if (Number.isFinite(n) && n > 0 && text) anchored.push([n, text]);
    else loose.push(line);
  }
  return { anchored, loose: loose.join('\n') };
}

/**
 * How much of the open buffer travels as ambient context.
 *
 * ⚠ **The whole buffer used to.** `open-file` goes out on every pause in typing, always — it is
 * ambient context, not a review, so nobody presses anything for it. The core keeps only the HEAD of
 * it (`ambientCap`, 8KB) and its comment says why in the memory it saves: *"holding the whole of a
 * 40MB buffer per session for the daemon's life is memory for nothing."* It clamps on STORE, so its
 * own memory was safe — and the socket still carried the whole file every 900ms while somebody typed.
 *
 * Cutting here changes nothing the model sees: the core keeps the head, and this is the head.
 * Counted in CHARACTERS against a byte cap on purpose — a character is never fewer than a byte, so
 * this always carries at least the bytes the core would keep, and the kept slice is identical.
 */
export const AMBIENT = 8 * 1024;

/** The head of a buffer, for the ambient slot. Never more than the core will keep. */
export function ambient(text: string): string { return text.slice(0, AMBIENT); }

/**
 * The buffer as the companion is shown it: `<n><TAB><code>`, one-based, bounded.
 *
 * Numbered because the reply hangs on those numbers, and ABSOLUTE because a clipped buffer would
 * otherwise be answered about the model's own count. Bounded because a person can open a
 * 40,000-line file and this travels on every pause in typing — unbounded, it is somebody's context
 * window and their bill.
 *
 * The cut is on a line boundary, never mid-line: half a line renumbers everything after it.
 */
export function numbered(text: string, maxBytes = 64 * 1024): string {
  const lines = text.split('\n');
  const out: string[] = [];
  let used = 0;
  for (let i = 0; i < lines.length; i++) {
    const row = `${i + 1}\t${lines[i]}`;
    const cost = Buffer.byteLength(row, 'utf8') + 1;
    if (used + cost > maxBytes) break;
    out.push(row);
    used += cost;
  }
  return out.join('\n');
}
