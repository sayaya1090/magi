/**
 * Reading ids out of doors that answer prose.
 *
 * `jobs` and `cron` do not answer structured rows — they answer text meant for a person to read.
 * A client that wants to act on one of those things (stop that job, remove that schedule) has to
 * find its name in the sentence.
 *
 * ⚠ **So this recognises, and offers nothing when it recognises nothing.** Plucking words out of a
 * sentence and sending them back as ids would have the daemon refuse an id that never existed, and
 * a refusal reads to a person as the job declining to stop. An honest "nothing to stop" is better.
 *
 * In `core` because it is a rule about the protocol's text, not about an editor — which is also the
 * only way it can be tested: `node --test` cannot load `vscode`.
 */

/** Background job ids, as the `jobs` door writes them. */
export function jobIds(out: string): string[] {
  const ids = new Set<string>();
  for (const line of out.split('\n')) {
    const m = /(?:^|\s)(job[_-][A-Za-z0-9_-]+|bg[_-][A-Za-z0-9_-]+)/.exec(line);
    if (m) ids.add(m[1]);
  }
  return [...ids];
}

/** Schedule names, as the `cron` door writes them: a name then columns, or a name then a colon. */
export function cronNames(out: string): string[] {
  const names = new Set<string>();
  for (const line of out.split('\n')) {
    const m = /^\s*[-*]?\s*([A-Za-z][A-Za-z0-9_-]{0,63})\s{2,}|^\s*([A-Za-z][A-Za-z0-9_-]{0,63})\s*:/.exec(line);
    const n = m?.[1] ?? m?.[2];
    if (n) names.add(n);
  }
  return [...names];
}
