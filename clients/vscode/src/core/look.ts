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
