import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';

const IDE = path.join(__dirname, '..', '..', 'src', 'ide');

/** Comments stripped, so a rule about code is not answered by prose that mentions it. */
function code(body: string): string {
  return body.replace(/\/\*[\s\S]*?\*\//g, ' ').replace(/(^|[^:])\/\/[^\n]*/g, '$1 ');
}

/** The HTML each webview builds, pulled out of its template literal. */
function templates(): { file: string; body: string; src: string }[] {
  const out: { file: string; body: string; src: string }[] = [];
  for (const f of fs.readdirSync(IDE).filter((n) => n.endsWith('.ts'))) {
    const src = fs.readFileSync(path.join(IDE, f), 'utf8');
    const i = src.indexOf('`<!DOCTYPE');
    if (i < 0) continue;
    const j = src.indexOf('</html>`', i);
    assert.ok(j > i, `${f}: a webview template opens and never closes`);
    out.push({ file: f, body: src.slice(i + 1, j), src });
  }
  return out;
}

/**
 * A backtick inside a webview's script closes the template literal that holds it.
 *
 * This cost an hour. A comment written as an @name in backticks — ordinary prose everywhere else
 * in this repository — ended the template early, and the compiler pointed at a line ten below with
 * "';' expected". The webview would have been broken in a way the type checker describes
 * misleadingly and no unit test touches.
 */
test('no webview script contains a backtick', () => {
  const found = templates();
  assert.ok(found.length >= 2, `only ${found.length} webview templates found — the scan is broken`);
  for (const { file, body } of found) {
    assert.ok(!body.includes('`'), `${file}: a backtick inside the webview template closes it early`);
  }
});

/** And every interpolation in there is one we meant — a stray ${ is a hole, not a value. */
test('every interpolation in a webview is a named one', () => {
  const allowed = new Set(['nonce', 'w.cspSource', 'csp']);
  for (const { file, body } of templates()) {
    for (const m of body.matchAll(/\$\{([^}]*)\}/g)) {
      assert.ok(allowed.has(m[1].trim()), `${file}: unexpected interpolation \${${m[1]}}`);
    }
  }
});

/**
 * The webview never writes model text as HTML.
 *
 * Everything in the transcript was written by a model or by a tool it ran. `innerHTML` there is a
 * hole with a stranger's text in it, and a strict CSP does not close it — the script is ours, and
 * ours would be the one injecting.
 */
test('the transcript is written as text, never as HTML', () => {
  for (const { file, body } of templates()) {
    // The comments are stripped first. One of them says "textContent, never innerHTML", and a
    // guard that read prose would fail on the sentence explaining why it passes.
    const js = code(body);
    assert.ok(!/\.innerHTML\s*=/.test(js), `${file}: innerHTML in a webview that draws model output`);
    assert.ok(!/\bdocument\.write\s*\(/.test(js), `${file}: document.write in a webview`);
    assert.ok(!/insertAdjacentHTML\s*\(/.test(js), `${file}: insertAdjacentHTML in a webview`);
    // And it does put model text somewhere safe, so "no innerHTML" is not passing on an empty view.
    assert.ok(/\.textContent\s*=/.test(js), `${file}: draws nothing at all — this asserts nothing`);
  }
});

/** And the policy that lets our own script run is there, with a nonce rather than unsafe-inline. */
test('every webview carries a strict content policy', () => {
  for (const { file, body, src } of templates()) {
    assert.ok(body.includes('Content-Security-Policy'), `${file}: no content policy`);
    // The policy itself is built above the template and interpolated in, so it is read from the
    // FILE. Looking only in the template would have found the interpolation and called it a policy.
    // To the end of that line, not to the first quote: the policy is full of quotes, and a
    // pattern that stopped at one would read only its opening clause and call the rest missing.
    const csp = /default-src 'none';.*/.exec(src)?.[0] ?? '';
    assert.ok(csp, `${file}: the policy does not start closed`);
    assert.ok(/script-src 'nonce-/.test(csp), `${file}: scripts are not nonce-gated`);
    assert.ok(!/script-src[^;]*unsafe-inline/.test(csp), `${file}: inline script is allowed`);
  }
});

/**
 * A send that did not land is said, and the words come back.
 *
 * The composer empties itself the instant Enter is pressed — deliberately, because the row for the
 * message only arrives on the stream a moment later and until then the empty box is the only sign
 * anything happened. That makes a dropped refusal invisible in the worst way: the sentence is
 * already off the screen, so silence reads as "sent". Measured across this client, six `ask` call
 * sites threw their answer away; the two on the composer's path were the ones a person can feel.
 *
 * The chips are part of it. They are cleared BEFORE the round trip so one cannot outlive its
 * message — and a message that never went has nothing to outlive. The JetBrains client clears its
 * box only after `ok` and its comment names the same trap ("the core keeps its promise that no
 * attachment vanishes; the client was where that broke").
 *
 * Read off the source: the seam is a webview message handler and there is no daemon in this
 * process, so what can be checked here is that the refusal is looked at and acted on.
 */
test('a refusal on the composer path is said, not swallowed', () => {
  const chat = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'chat.ts'), 'utf8');

  const say = chat.slice(chat.indexOf("case 'say'"), chat.indexOf("case 'start'"));
  assert.ok(say.length > 100, 'the send branch was not found — this guard is reading nothing');
  assert.ok(/!r\?\.ok|!r\.ok/.test(say), 'the send never looks at whether the door said yes');
  assert.ok(/giveBack\(/.test(say), 'a refused send says nothing and the words are gone with it');

  const reply = chat.slice(chat.indexOf("case 'reply'"), chat.indexOf("case 'mention'"));
  assert.ok(reply.length > 100, 'the reply branch was not found — this guard is reading nothing');
  assert.ok(/giveBack\(/.test(reply), 'a refused answer says nothing — it is the same box, same rule');

  // And giveBack really does all three things. Any one of them missing is a silent half-fix.
  const back = chat.slice(chat.indexOf('private giveBack('));
  const body = back.slice(0, back.indexOf('\n  }'));
  assert.ok(/this\.refs\.push/.test(body), 'the chips are not given back — the attachment vanished');
  assert.ok(/kind: 'note'/.test(body), 'nothing is said to the person');
  assert.ok(/this\.compose\(/.test(body), 'the words are not put back in the box');
});
