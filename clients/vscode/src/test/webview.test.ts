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

/**
 * Every door a person PRESSES looks at the answer. Swept, not listed.
 *
 * The list form of this guard would rot: a new button is added, nobody adds its name here, and the
 * rule passes while the defect ships. So it reads the source, finds the `ask(...)` calls itself,
 * and asks of each one whether its answer is READ.
 *
 * ⚠ Read, not bound. The first cut of this guard accepted "the answer is assigned to a name", and
 * a mutation walked straight through it: `const v = await ask(...); void v;` binds the answer,
 * looks at nothing, and the defect is fully back. It survived, so the guard was blind — the rule
 * is now that the bound name is tested (`.ok`), returned, or handed to something else.
 *
 * Three names are exempt, each with its reason written down. An exemption is a decision — the
 * JetBrains client's comment on the same defect calls a swallowed answer "a window where nothing
 * happens when you press it" — so it has to be argued, not assumed.
 */
test('every door a person presses looks at what came back', () => {
  const excused: Record<string, string> = {
    'mcp-detach': 'teardown on dispose; nobody is watching and "there was nothing to remove" is the wanted answer',
    'open-file': 'a background note about which file is open — not a thing a person did',
    'tool': 'the @-mention glob; an empty list IS the failure the caller already handles',
    'suggest': 'ghost text; no suggestion is a normal answer and drawing a warning for it would nag',
  };
  const files = ['chat.ts', 'extension.ts', 'hand.ts', 'look.ts', 'choose.ts', 'handoff.ts', 'complete.ts'];
  const swallowed: string[] = [];
  let seen = 0;
  for (const f of files) {
    const src = fs.readFileSync(path.join(IDE, f), 'utf8');
    for (const m of src.matchAll(/(.{0,60})\.(?:ask|exchange)\(\s*(door|'[a-z-]+')/g)) {
      seen++;
      const door = m[2].replace(/'/g, '');
      if (door in excused) continue;
      const before = m[1].replace(/(?:await\s+)?[\w.]*$/, '').trimEnd();
      // Used in place — a condition, an argument, a return. Nothing to follow up.
      if (/[(?:,[]$|\breturn$|&&$|\|\|$/.test(before)) continue;
      // Bound to a name: then that name has to be READ before the branch ends.
      const bind = before.match(/(?:const|let|var)\s+([\w]+)\s*=$/);
      if (bind) {
        const after = src.slice(m.index! + m[0].length, m.index! + m[0].length + 500);
        const used = new RegExp(`\\b${bind[1]}\\s*(?:\\?\\.|\\.|\\))|\\b${bind[1]}\\b\\s*[,)]`).test(after);
        if (used) continue;
      }
      swallowed.push(`${f}: ${door}`);
    }
  }
  // The scanner is checked before its verdict is believed: reading nothing reports nothing.
  assert.ok(seen >= 15, `only ${seen} door calls found — the scan is broken, not the code clean`);
  assert.deepEqual(swallowed, [],
    'these doors can refuse and nobody would ever know: ' + swallowed.join(', ') +
    ' — either read the answer, or add the name to `excused` with the reason it cannot fail visibly.');
});

/**
 * The info card: what this companion runs on, and the handles that change it.
 *
 * Asked for by the user, and built mostly out of what was already there — the model, approval and
 * fold commands existed but were scattered in the palette. Two were new, and both of their doors
 * were already on the wire and knocked on by nobody: `update` and `restart`.
 *
 * ⚠ **A webview may not name a command.** It is a page; anything that got script into it could
 * otherwise run whatever the extension host can. So the card asks by name and the extension checks
 * that name against a list — and this pins that the list exists, that it is a list and not a
 * pass-through, and that every command the card offers is on it.
 */
test('the info card can only ask for the commands it offers', () => {
  const chat = fs.readFileSync(path.join(IDE, 'chat.ts'), 'utf8');

  const at = chat.indexOf("case 'run':");
  assert.ok(at > 0, 'the card asks the extension to run commands and nothing receives it');
  const branch = chat.slice(at, chat.indexOf('\n      }', at));
  assert.ok(/new Set\(\[/.test(branch), 'the command name is taken from the page and not checked against a list');
  assert.ok(/if \(!allowed\.has\(name\)\) break;/.test(branch),
    'the list is built and not consulted — a page could name any command');

  // Every command the card draws a button for must be on the list, or pressing it does nothing.
  // The card names its commands in two shapes: `['model', info.model, 'magi.chooseModel']` for the
  // rows and `['restart', 'magi.restartDaemon']` for the buttons. Read them where the card DRAWS,
  // so a command added to one shape and not the other is still counted.
  const draw = chat.slice(chat.indexOf('function drawInfo('), chat.indexOf('let pendingQuestion'));
  const offered = [...draw.matchAll(/'(magi\.[a-zA-Z]+)'/g)].map((m) => m[1]);
  assert.ok(offered.length >= 6, `only ${offered.length} commands offered by the card — the scan is reading nothing`);
  for (const c of new Set(offered)) {
    assert.ok(branch.includes(`'${c}'`), `the card offers ${c} and the allowlist does not have it — the button does nothing`);
  }

  // And each one is a real command of this extension, or the button throws when pressed.
  const manifest = JSON.parse(fs.readFileSync(path.join(IDE, '..', '..', 'package.json'), 'utf8'));
  const declared = new Set((manifest.contributes?.commands ?? []).map((c: { command: string }) => c.command));
  for (const c of new Set(offered)) {
    assert.ok(declared.has(c), `the card offers ${c} and the manifest does not declare it`);
  }
});

/**
 * The traffic light is a class, not a colour decided here.
 *
 * The console does it this way — the state word IS the class name and the stylesheet paints it —
 * so the mapping from word to colour is written once instead of once per screen. Every state the
 * core can be in needs a rule, or a companion in that state draws with the default grey and reads
 * as "we could not ask" when it is nothing of the sort.
 */
test('every state the card can show has a light', () => {
  const chat = fs.readFileSync(path.join(IDE, 'chat.ts'), 'utf8');
  const style = chat.slice(chat.indexOf('<style>'), chat.indexOf('</style>'));
  const src = fs.readFileSync(path.join(IDE, '..', 'core', 'activity.ts'), 'utf8');
  const states = [...src.matchAll(/^\s{2}[A-Z]\w*\s*=\s*'([a-z-]+)',/gm)].map((m) => m[1]);
  assert.ok(states.length >= 4, `only ${states.length} states read from core — the scan is broken`);
  for (const st of states) {
    assert.ok(style.includes(`#info .${st} .dot`),
      `a companion that is "${st}" draws the default grey, which is what "could not ask" looks like`);
  }
});

/**
 * ★ A verdict's evidence is a fragment of the RECORD, and must be drawn as one.
 *
 * Measured by streaming a real conversation off a live daemon (95 events, 12 verdicts) through this
 * client's own shaper: nine of the twelve `cite` values were diffs — leading minus/plus/space and
 * all. The row drew them in the proportional reading font, in italic.
 *
 * This file states the rule for that kind of text a few lines above the one at fault: "Monospace
 * and scrollable: it is a command or a patch, and a wrapped one is a different command to read."
 * The cite is exactly that, and the core keeps it because it is CHECKABLE — magi looks the fragment
 * up in what the member was shown. A reader can only check what is drawn as it is.
 *
 * `keep` is the member's own prose and stays in the reading font, so the two must not share a rule.
 */
test('a verdict cite is drawn as the record it quotes', () => {
  const chat = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'chat.ts'), 'utf8');
  const style = chat.slice(chat.indexOf('<style>'), chat.indexOf('</style>'));

  const cite = /\.cite \{[^}]*\}/.exec(style);
  assert.ok(cite, 'the cite has no style of its own — it cannot be told from the prose beside it');
  assert.match(cite![0], /editor-font-family/,
    'the cite is drawn in the reading font; a diff whose leading marks carry the meaning needs the ' +
    'editor font, which this file already says a few rules up');
  assert.match(cite![0], /max-height/, 'an unbounded cite pushes the round off screen');

  // Shared with `keep` is how it got the wrong font: one rule for prose and for a patch.
  assert.ok(!/\.cite,\s*\.keep/.test(style) && !/\.keep,\s*\.cite/.test(style),
    'cite and keep share a rule — one is a fragment of the record and the other is prose');
  const keep = /\.keep \{[^}]*\}/.exec(style);
  assert.ok(keep, 'the keep lost its style when the two were split');
  assert.ok(!/editor-font-family/.test(keep![0]), 'the keep is prose and is drawn as code');

  // Newlines survive: the rows carry pre-wrap and the cite inherits it. Pinned because the
  // fix would be invisible if a later rule set `white-space: normal` here.
  assert.match(style, /\.row \{[^}]*white-space:pre-wrap/,
    'rows no longer preserve newlines, so a multi-line cite collapses into one line');
  assert.ok(!/\.cite \{[^}]*white-space:\s*normal/.test(style), 'the cite overrides pre-wrap away');
});

/**
 * ★ Every field of `Ask` reaches the prompt card.
 *
 * The card is where the most is riding on what is drawn, and this file already records two things
 * that went missing there: the permission SUBJECT (a person pressed allow knowing only a tool name)
 * and the GROUNDS a question was asked on. Both were carried across the wire, declared in the type,
 * and drawn by nothing. A third joined them — `since`, the time the prompt went up.
 *
 * So the list is DERIVED from the type rather than remembered here: a field added to `Ask` and not
 * drawn fails this, which is the exact shape of every one of those three defects. `callId` is read
 * where the buttons post back, so the scan is the whole function, not the header.
 */
test('every field the ask carries is drawn on the card', () => {
  const core = fs.readFileSync(path.join(IDE, '..', 'core', 'touched.ts'), 'utf8');
  const decl = core.slice(core.indexOf('export interface Ask {'));
  const body = decl.slice(0, decl.indexOf('\n}'));
  const fields = [...body.matchAll(/^  (\w+)\??:/gm)].map((m) => m[1]);
  assert.ok(fields.length >= 8, `only ${fields.length} Ask fields read — the scan is dead`);

  const chat = fs.readFileSync(path.join(IDE, 'chat.ts'), 'utf8');
  const at = chat.indexOf('function drawAsk(a) {');
  assert.ok(at > 0, 'the prompt card is not where this guard looks for it');
  const draw = chat.slice(at, chat.indexOf('\nconst moreEl', at));
  for (const f of fields) {
    assert.ok(new RegExp(`a\\.${f}\\b`).test(draw),
      `the ask carries ${f} and the card never reads it — the field crosses the wire and dies here`);
  }
});

/**
 * ★ The children list actually CALLS the word-maker.
 *
 * A lesson this session paid for twice, in the other client and then here: a function existing and
 * being tested is not the screen using it. `originWord` can be unit-tested all day while the picker
 * builds its rows without it, and nothing fails — the list simply goes on drawing a meeting room and
 * a subagent identically.
 *
 * `doors.ts` imports `vscode`, so no test can load it. Read as text, like the JetBrains client reads
 * its own untestable module. Scoped to the children command, so another command's use of the word
 * cannot vouch for this one.
 */
test('the children list draws who opened each child', () => {
  const src = fs.readFileSync(path.join(IDE, 'doors.ts'), 'utf8');
  const at = src.indexOf("reg('magi.children'");
  assert.ok(at > 0, 'the children command is not where this guard looks for it');
  const block = src.slice(at, src.indexOf('\n    }),', at));
  assert.ok(/originWord\(\s*s\.origin\s*\)/.test(block),
    'the children list does not put the opener on the row — a meeting room and a subagent draw the same');
  // And not from `agent`, which the core says tells them apart not at all: every child records the
  // same word, and a live run brought a meeting room back as agent="spawn".
  assert.ok(!/\bs\.agent\b/.test(block),
    'the children list keys on `agent`, which is the same word for every child — it discriminates nothing');
});

/**
 * And the conversation list actually calls it.
 *
 * The third time this session that a helper was written, unit-tested, and could still have been
 * left uncalled by the screen it was written for. `doors.ts` imports `vscode`, so it is read as text.
 */
test('the conversation list stamps its rows in local time', () => {
  const src = fs.readFileSync(path.join(IDE, 'doors.ts'), 'utf8');
  const at = src.indexOf("reg('magi.resume'");
  assert.ok(at > 0, 'the resume command is not where this guard looks for it');
  const block = src.slice(at, src.indexOf('\n    }),', at));
  assert.ok(/localStamp\(\s*s\.lastActivity\s*\)/.test(block),
    'the conversation list draws the wire timestamp raw — UTC, with the T and the Z');
});

/**
 * ★ The completion provider reads the WINDOW, not the document.
 *
 * A mutation proved this needs its own guard: putting `() => doc.getText()` back — the whole
 * document, on every pause in typing — compiles and passes every other test. `around` is handed a
 * reader now, and a reader that ignores its bounds is indistinguishable from the old code by any
 * test that only looks at the strings that come back.
 *
 * `complete.ts` imports `vscode`, so no test can load it. Read as text, the way the JetBrains client
 * reads its own untestable module for the same fix in the same wave.
 */
test('the completion provider reads only the window', () => {
  const src = fs.readFileSync(path.join(IDE, 'complete.ts'), 'utf8')
    .split('\n').filter((l) => !l.trim().startsWith('//') && !l.trim().startsWith('*')).join('\n');
  assert.ok(/around\(/.test(src), 'the provider no longer builds its window here — re-read this guard');
  // A reader that uses its bounds. `getText()` with nothing in the parentheses is the whole buffer.
  assert.ok(!/doc\.getText\(\s*\)/.test(src),
    'the whole document is read on every keystroke, and all but the window thrown away');
  assert.ok(/doc\.positionAt\(from\)/.test(src) && /doc\.positionAt\(to\)/.test(src),
    'the reader does not use the offsets it is given — its bounds are ignored');
});

/**
 * And the ambient push actually cuts. `look.ts` imports `vscode`, so it is read as text.
 *
 * The same shape as the completion window one file over: a helper that exists, is tested, and is not
 * called by the one place it was written for. That gap has been the finding four times this session.
 */
test('the ambient push sends only the head of the buffer', () => {
  const src = fs.readFileSync(path.join(IDE, 'look.ts'), 'utf8')
    .split('\n').filter((l) => !l.trim().startsWith('//') && !l.trim().startsWith('*')).join('\n');
  const at = src.indexOf("'open-file'");
  assert.ok(at > 0, 'the ambient push is not where this guard looks for it');
  const line = src.slice(src.lastIndexOf('\n', at), src.indexOf('\n', at));
  assert.ok(/ambient\(\s*doc\.getText\(\)\s*\)/.test(line),
    `the whole buffer goes out on every pause in typing: ${line.trim()}`);
});
