import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';

import { parseWebviewToHostMessage, WebviewToHostMessage } from '../core/webview_protocol';
import {
  createWebviewActionAdapter,
  dispatchHostMessage,
  parseHostToWebviewMessage,
  createWebviewReceiveHandlers,
  createWebviewInputAdapter,
  createSuggestController,
  classifyDiffLines,
  renderMarkdown,
  WebviewBridge,
} from '../web/chat_adapter';
import { createAnswerState } from '../core/answer_state';
import { State, notRunning, panelNote } from '../core/activity';
import type { Ask } from '../core/touched';

const IDE = path.join(__dirname, '..', '..', 'src', 'ide');
const WEB = path.join(__dirname, '..', '..', 'src', 'web');

/** Comments stripped, so a rule about code is not answered by prose that mentions it. */
function code(body: string): string {
  return body.replace(/\/\*[\s\S]*?\*\//g, ' ').replace(/(^|[^:])\/\/[^\n]*/g, '$1 ');
}

/** The HTML each webview builds, pulled out of its template literal. */
function templates(): { file: string; body: string; src: string }[] {
  const out: { file: string; body: string; src: string }[] = [];
  for (const dir of [IDE, WEB]) {
    for (const f of fs.readdirSync(dir).filter((n) => n.endsWith('.ts'))) {
      const src = fs.readFileSync(path.join(dir, f), 'utf8');
      const i = src.indexOf('`<!DOCTYPE');
      if (i < 0) continue;
      const j = src.indexOf('</html>`', i);
      assert.ok(j > i, `${f}: a webview template opens and never closes`);
      out.push({ file: f, body: src.slice(i + 1, j), src });
    }
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
  const allowed = new Set(['nonce', 'w.cspSource', 'csp', 'scriptUri', 'adapterUri']);
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
  assert.ok(/replyResult/.test(reply), 'a refused answer returns its replyResult to the webview with its callId');
  assert.ok(/kind: 'note'/.test(reply), 'a refused answer says so with a note');

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
  const chatHtml = fs.readFileSync(path.join(WEB, 'chat_html.ts'), 'utf8');

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
  const draw = chatHtml.slice(chatHtml.indexOf('function drawInfo('), chatHtml.indexOf('let pendingQuestion'));
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
  const chat = fs.readFileSync(path.join(WEB, 'chat_html.ts'), 'utf8');
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
  const chat = fs.readFileSync(path.join(WEB, 'chat_html.ts'), 'utf8');
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

  const chat = fs.readFileSync(path.join(WEB, 'chat_html.ts'), 'utf8');
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
  // ⚠ The WHOLE argument, not merely a call somewhere in it. A mutation proved the difference:
  // `ambient(doc.getText()) + doc.getText()` contains the call and sends the whole buffer anyway.
  assert.ok(/text:\s*ambient\(doc\.getText\(\)\)\s*[,}]/.test(line),
    `the whole buffer goes out on every pause in typing: ${line.trim()}`);
});

/**
 * And the look-over path actually places before storing.
 *
 * `look.ts` imports `vscode`, so it is read as text — the fifth time this session that a helper was
 * written, tested, and could still have been left uncalled by the one place it was written for.
 */
test('the look-over reply is placed before it is stored', () => {
  const src = fs.readFileSync(path.join(IDE, 'look.ts'), 'utf8')
    .split('\n').filter((l) => !l.trim().startsWith('//') && !l.trim().startsWith('*')).join('\n');
  assert.ok(/place\(split\([^)]*\),\s*doc\.lineCount\)/.test(src),
    'the reply is stored unplaced — a finding past the end of the file is lost silently');
});

/**
 * ★ A lead-in does not delete what the person was typing.
 *
 * `compose` carries two things: a lead-in for a question the person is about to type, and their own
 * words handed back after a send that did not land. It ASSIGNED, so somebody mid-sentence who
 * reached for "ask about this code" lost the sentence — the very thing the caller's own comment says
 * they are meant to write ("The lead only. The person types the question").
 *
 * The same rule this repository applies to the commit message box one client over: the box is theirs.
 * After a send the box is already empty, so prepending is what assigning was for that caller — one
 * shape serves both, and neither loses anything.
 *
 * The page is a string of script, so this is read as text.
 */
test('a lead-in is prepended to the composer, never assigned over it', () => {
  const adapterSrc = fs.readFileSync(path.join(IDE, '..', 'web', 'chat_adapter.ts'), 'utf8');
  const at = adapterSrc.indexOf('function handleCompose(text: string)');
  assert.ok(at > 0, 'the compose handler is not where this guard looks for it');
  const branch = adapterSrc.slice(at, adapterSrc.indexOf('function handleMentions', at))
    .split('\n').filter((l) => !l.trim().startsWith('/*') && !l.trim().startsWith('*')).join('\n');
  assert.ok(/say\.value\s*=\s*lead\s*\+\s*say\.value/.test(branch),
    'the composer is assigned over — a sentence being typed is destroyed by a lead-in');
  assert.ok(!/say\.value\s*=\s*text/.test(branch) && !/say\.value\s*=\s*m\.text/.test(branch), 'the old assignment is still there');
  // The caret lands at the end of the LEAD, so typing continues after it rather than before.
  assert.ok(/setSelectionRange\(lead\.length, lead\.length\)/.test(branch),
    'the caret is not put after the lead — the person types in front of it');
});

/**
 * And the hand's own resolver refuses before it builds a Uri.
 *
 * `hand.ts` imports `vscode`, so it is read as text. The decision is pure and tested next door; what
 * is checked here is that the door actually consults it.
 */
test('the editor hand refuses a path outside the workspace', () => {
  const src = fs.readFileSync(path.join(IDE, 'hand.ts'), 'utf8')
    .split('\n').filter((l) => !l.trim().startsWith('//') && !l.trim().startsWith('*')).join('\n');
  const at = src.indexOf('private resolve(');
  assert.ok(at > 0, 'the resolver is not where this guard looks for it');
  const fn = src.slice(at, src.indexOf('\n  }', at));
  assert.ok(/inside\(this\.workdir, path\)/.test(fn),
    'the resolver opens whatever path the companion names — the workspace boundary is not kept here');
  assert.ok(/throw/.test(fn), 'the check is made and not acted on');
});

/**
 * ★ And the page draws the round's threshold, not just carries it.
 *
 * A mutation proved this needs its own line: emptying the opened-row label leaves the fold's tests
 * green — they measure the Row, and the Row was right. The sixth time this session that a fact
 * reached a shaper and stopped there.
 */
test('an opened council round draws its rule', () => {
  const chat = fs.readFileSync(path.join(IDE, 'chat.ts'), 'utf8');
  const at = chat.indexOf('const vote =');
  assert.ok(at > 0, 'the council label is not where this guard looks for it');
  const branch = chat.slice(at, chat.indexOf(';\n', chat.indexOf('r.round', at)));
  assert.ok(/r\.opened\s*\?/.test(branch), 'an opened round is labelled like a verdict');
  assert.ok(/r\.rule/.test(branch),
    'the round opens without its threshold — two continue and one done mean different things under majority and unanimous');
});

test('parseWebviewToHostMessage parses valid messages according to schema', () => {
  assert.deepEqual(parseWebviewToHostMessage({ kind: 'ready' }), { kind: 'ready' });
  assert.deepEqual(parseWebviewToHostMessage({ kind: 'start' }), { kind: 'start' });
  assert.deepEqual(parseWebviewToHostMessage({ kind: 'drop' }), { kind: 'drop' });

  assert.deepEqual(parseWebviewToHostMessage({ kind: 'say', text: 'hello' }), {
    kind: 'say',
    text: 'hello',
  });

  assert.deepEqual(parseWebviewToHostMessage({ kind: 'run', command: 'magi.compact' }), {
    kind: 'run',
    command: 'magi.compact',
  });

  assert.deepEqual(
    parseWebviewToHostMessage({ kind: 'diff', session: 's1', callId: 'c1' }),
    { kind: 'diff', session: 's1', callId: 'c1' },
  );

  assert.deepEqual(
    parseWebviewToHostMessage({ kind: 'open', session: 's1', callId: 'c1', seq: 42 }),
    { kind: 'open', session: 's1', callId: 'c1', seq: 42 },
  );

  assert.deepEqual(
    parseWebviewToHostMessage({ kind: 'open', session: 's1', callId: 'c1' }),
    { kind: 'open', session: 's1', callId: 'c1', seq: undefined },
  );

  assert.deepEqual(
    parseWebviewToHostMessage({ kind: 'answer', callId: 'c1', decision: 'allow' }),
    { kind: 'answer', callId: 'c1', decision: 'allow' },
  );

  assert.deepEqual(
    parseWebviewToHostMessage({
      kind: 'reply',
      callId: 'c1',
      text: 'my answer',
      attemptId: 3,
      companionKey: '/work/ws',
      session: 'sess1',
      generation: 1,
      webviewId: 'view-1',
    }),
    {
      kind: 'reply',
      callId: 'c1',
      text: 'my answer',
      attemptId: 3,
      companionKey: '/work/ws',
      session: 'sess1',
      generation: 1,
      webviewId: 'view-1',
    },
  );

  assert.deepEqual(
    parseWebviewToHostMessage({ kind: 'mention', text: 'foo', reqId: 7, target: 'composer' }),
    { kind: 'mention', text: 'foo', reqId: 7, target: 'composer' },
  );

  assert.deepEqual(
    parseWebviewToHostMessage({ kind: 'suggest', text: 'let x', reqId: 8, target: 'composer' }),
    { kind: 'suggest', text: 'let x', reqId: 8, target: 'composer' },
  );
});

test('parseWebviewToHostMessage strictly rejects malformed or incomplete messages at the boundary', () => {
  // Non-object
  assert.equal(parseWebviewToHostMessage(null), undefined);
  assert.equal(parseWebviewToHostMessage(undefined), undefined);
  assert.equal(parseWebviewToHostMessage('not an object'), undefined);
  assert.equal(parseWebviewToHostMessage(123), undefined);

  // Unknown kind
  assert.equal(parseWebviewToHostMessage({ kind: 'unknown' }), undefined);

  // Missing required fields on open
  assert.equal(parseWebviewToHostMessage({ kind: 'open', callId: 'c1' }), undefined, 'missing session');
  assert.equal(parseWebviewToHostMessage({ kind: 'open', session: 's1' }), undefined, 'missing callId');

  // Missing required fields on diff
  assert.equal(parseWebviewToHostMessage({ kind: 'diff', callId: 'c1' }), undefined, 'missing session');
  assert.equal(parseWebviewToHostMessage({ kind: 'diff', session: 's1' }), undefined, 'missing callId');

  // Missing or invalid required fields on reply
  assert.equal(
    parseWebviewToHostMessage({ kind: 'reply', callId: 'c1', text: 'abc' }),
    undefined,
    'missing attemptId',
  );
  assert.equal(
    parseWebviewToHostMessage({
      kind: 'reply',
      text: 'abc',
      attemptId: 1,
      companionKey: '/ws',
      session: 's1',
      generation: 0,
      webviewId: 'v1',
    }),
    undefined,
    'missing callId',
  );
  assert.equal(
    parseWebviewToHostMessage({
      kind: 'reply',
      callId: 'c1',
      text: 'abc',
      attemptId: 0,
      companionKey: '/ws',
      session: 's1',
      generation: 0,
      webviewId: 'v1',
    }),
    undefined,
    'non-positive attemptId',
  );
  assert.equal(
    parseWebviewToHostMessage({
      kind: 'reply',
      callId: 'c1',
      text: 'abc',
      attemptId: 1,
      companionKey: '',
      session: 's1',
      generation: 0,
      webviewId: 'v1',
    }),
    undefined,
    'missing companionKey',
  );
  assert.equal(
    parseWebviewToHostMessage({
      kind: 'reply',
      callId: 'c1',
      text: 'abc',
      attemptId: 1,
      companionKey: '/ws',
      session: '',
      generation: 0,
      webviewId: 'v1',
    }),
    undefined,
    'missing session',
  );
  assert.equal(
    parseWebviewToHostMessage({
      kind: 'reply',
      callId: 'c1',
      text: 'abc',
      attemptId: 1,
      companionKey: '/ws',
      session: 's1',
      generation: -1,
      webviewId: 'v1',
    }),
    undefined,
    'negative generation',
  );
  assert.equal(
    parseWebviewToHostMessage({
      kind: 'reply',
      callId: 'c1',
      text: 'abc',
      attemptId: 1,
      companionKey: '/ws',
      session: 's1',
      generation: 0,
      webviewId: '',
    }),
    undefined,
    'missing webviewId',
  );

  // Missing required fields on run
  assert.equal(parseWebviewToHostMessage({ kind: 'run' }), undefined, 'missing command');
  assert.equal(parseWebviewToHostMessage({ kind: 'run', command: '' }), undefined, 'empty command');

  // Missing required fields on answer
  assert.equal(parseWebviewToHostMessage({ kind: 'answer', callId: 'c1' }), undefined, 'missing decision');
  assert.equal(parseWebviewToHostMessage({ kind: 'answer', decision: 'allow' }), undefined, 'missing callId');
});

test('createWebviewActionAdapter formats and guards outbound messages', () => {
  const posted: WebviewToHostMessage[] = [];
  const bridge: WebviewBridge = {
    postMessage(msg) { posted.push(msg); },
  };
  const adapter = createWebviewActionAdapter(bridge);

  // Guard checks
  assert.equal(adapter.openFile('', 'c1'), false);
  assert.equal(adapter.openFile('sess', ''), false);
  assert.equal(adapter.openDiff('', 'c1'), false);
  assert.equal(adapter.answer('', 'allow'), false);
  assert.equal(adapter.answer('c1', ''), false);
  assert.equal(adapter.reply('', 'txt', 1, { companionKey: '/ws', session: 's1', generation: 0, webviewId: 'v1' }), false);
  assert.equal(adapter.reply('c1', 'txt', 0, { companionKey: '/ws', session: 's1', generation: 0, webviewId: 'v1' }), false);
  assert.equal(adapter.reply('c1', 'txt', 1, { companionKey: '', session: 's1', generation: 0, webviewId: 'v1' }), false);
  assert.equal(adapter.reply('c1', 'txt', 1, { companionKey: '/ws', session: '', generation: 0, webviewId: 'v1' }), false);
  assert.equal(adapter.reply('c1', 'txt', 1, { companionKey: '/ws', session: 's1', generation: 0, webviewId: '' }), false);
  assert.equal(adapter.say('   '), false);
  assert.equal(adapter.act('   '), false);

  // Successful dispatches
  assert.equal(adapter.openFile('sess1', 'call1'), true);
  assert.deepEqual(posted.pop(), { kind: 'open', session: 'sess1', callId: 'call1' });

  assert.equal(adapter.openFile('sess1', 'call1', 42), true);
  assert.deepEqual(posted.pop(), { kind: 'open', session: 'sess1', callId: 'call1', seq: 42 });

  assert.equal(adapter.openDiff('sess1', 'call1'), true);
  assert.deepEqual(posted.pop(), { kind: 'diff', session: 'sess1', callId: 'call1' });

  assert.equal(adapter.answer('call1', 'allow'), true);
  assert.deepEqual(posted.pop(), { kind: 'answer', callId: 'call1', decision: 'allow' });

  assert.equal(adapter.reply('call1', 'answer text', 2, {
    companionKey: '/ws',
    session: 'sess1',
    generation: 1,
    webviewId: 'view-1',
  }), true);
  assert.deepEqual(posted.pop(), {
    kind: 'reply',
    callId: 'call1',
    text: 'answer text',
    attemptId: 2,
    companionKey: '/ws',
    session: 'sess1',
    generation: 1,
    webviewId: 'view-1',
  });

  assert.equal(adapter.say('hello companion'), true);
  assert.deepEqual(posted.pop(), { kind: 'say', text: 'hello companion' });

  assert.equal(adapter.act('magi.restartDaemon'), true);
  assert.deepEqual(posted.pop(), { kind: 'run', command: 'magi.restartDaemon' });

  assert.equal(adapter.suggest('typing...', 5, 'general'), true);
  assert.deepEqual(posted.pop(), { kind: 'suggest', text: 'typing...', reqId: 5, target: 'general' });

  assert.equal(adapter.mention('app', 6, 'general'), true);
  assert.deepEqual(posted.pop(), { kind: 'mention', text: 'app', reqId: 6, target: 'general' });

  adapter.drop();
  assert.deepEqual(posted.pop(), { kind: 'drop' });

  adapter.start();
  assert.deepEqual(posted.pop(), { kind: 'start' });

  adapter.ready();
  assert.deepEqual(posted.pop(), { kind: 'ready' });
});

test('parseHostToWebviewMessage validates schema and rejects malformed payloads', () => {
  // Non-objects / primitives
  assert.equal(parseHostToWebviewMessage(null), undefined);
  assert.equal(parseHostToWebviewMessage(undefined), undefined);
  assert.equal(parseHostToWebviewMessage(123), undefined);
  assert.equal(parseHostToWebviewMessage('string'), undefined);
  assert.equal(parseHostToWebviewMessage({}), undefined);
  assert.equal(parseHostToWebviewMessage({ kind: 'unknown' }), undefined);

  // rows
  assert.equal(parseHostToWebviewMessage({ kind: 'rows' }), undefined, 'missing rows array rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'rows', rows: 'not-an-array' }), undefined, 'non-array rows rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'rows', rows: [] }), undefined, 'missing session/refs rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'rows', rows: [], session: 's1' }), undefined, 'missing refs rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'rows', rows: [], refs: [] }), undefined, 'missing session rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'rows', rows: [], session: 's1', refs: [123] }), undefined, 'non-string refs rejected');
  assert.equal(
    parseHostToWebviewMessage({ kind: 'rows', rows: [], session: 's1', refs: [], ask: { callId: 'c1', prompt: 'hi' } }),
    undefined,
    'ask with prompt instead of what rejected',
  );
  assert.equal(
    parseHostToWebviewMessage({ kind: 'rows', rows: [], session: 's1', refs: [], ask: { callId: 'c1', what: 'hi' } }),
    undefined,
    'ask with missing kind rejected',
  );
  assert.equal(
    parseHostToWebviewMessage({ kind: 'rows', rows: [], session: 's1', refs: [], ask: { callId: 'c1', what: 'hi', kind: 'other' } }),
    undefined,
    'ask with unknown kind rejected',
  );
  assert.equal(
    parseHostToWebviewMessage({ kind: 'rows', rows: [], session: 's1', refs: [], ask: { callId: 'c1', what: 'hi', kind: 'question', options: [1] } }),
    undefined,
    'ask with non-string options [1] rejected',
  );
  assert.equal(
    parseHostToWebviewMessage({ kind: 'rows', rows: [], session: 's1', refs: [], ask: { callId: 'c1', what: 'hi', kind: 'question', report: [{ key: 'k', text: 123 }] } }),
    undefined,
    'ask with invalid report item rejected',
  );
  assert.deepEqual(parseHostToWebviewMessage({ kind: 'rows', session: '', rows: [], refs: [] }), {
    kind: 'rows',
    session: '',
    rows: [],
    ask: null,
    refs: [],
  });
  assert.deepEqual(parseHostToWebviewMessage({
    kind: 'rows',
    session: 's1',
    rows: [{ who: 'agent', label: 'magi', text: 'hi' }],
    ask: {
      kind: 'question',
      callId: 'c1',
      what: 'proceed?',
      options: ['yes', 'no'],
      report: [{ key: 'status', text: 'ready' }],
      filePath: 'src/main.ts',
    },
    refs: ['ref1'],
  }), {
    kind: 'rows',
    session: 's1',
    rows: [{ who: 'agent', label: 'magi', text: 'hi' }],
    ask: {
      kind: 'question',
      callId: 'c1',
      what: 'proceed?',
      options: ['yes', 'no'],
      report: [{ key: 'status', text: 'ready' }],
      filePath: 'src/main.ts',
    },
    refs: ['ref1'],
  });

  // state
  assert.equal(parseHostToWebviewMessage({ kind: 'state' }), undefined, 'missing state/note rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'state', state: { state: State.NotRunning } }), undefined, 'missing note rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'state', state: 'not-an-object', note: { text: 'ok', offerStart: true } }), undefined, 'string state rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'state', state: { state: State.NotRunning }, note: null }), undefined, 'null note rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'state', state: { state: State.NotRunning }, note: { offerStart: false } }), undefined, 'missing note.text rejected');
  assert.deepEqual(parseHostToWebviewMessage({ kind: 'state', state: { state: State.Attached }, note: { text: 'ok', offerStart: true } }), {
    kind: 'state',
    state: { state: State.Attached, asking: undefined, doing: undefined },
    note: { text: 'ok', offerStart: true },
  });

  // info
  assert.equal(parseHostToWebviewMessage({ kind: 'info' }), undefined, 'missing info fields rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'info', state: 'idle', label: 'idle' }), undefined, 'missing version rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'info', state: 'idle', label: 123, version: '1.0' }), undefined, 'invalid label type rejected');
  assert.deepEqual(parseHostToWebviewMessage({ kind: 'info', state: 'idle', label: 'Ready', version: '0.1.0', model: 'flash' }), {
    kind: 'info',
    state: 'idle',
    label: 'Ready',
    version: '0.1.0',
    model: 'flash',
    backend: undefined,
    permission: undefined,
    council: undefined,
    socket: undefined,
  });

  // compose
  assert.equal(parseHostToWebviewMessage({ kind: 'compose' }), undefined, 'missing compose text rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'compose', text: 42 }), undefined, 'non-string compose text rejected');
  assert.deepEqual(parseHostToWebviewMessage({ kind: 'compose', text: 'help with ' }), {
    kind: 'compose',
    text: 'help with ',
  });

  // note
  assert.equal(parseHostToWebviewMessage({ kind: 'note' }), undefined, 'missing note text rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'note', text: 99 }), undefined, 'non-string note text rejected');
  assert.deepEqual(parseHostToWebviewMessage({ kind: 'note', text: 'connected' }), {
    kind: 'note',
    text: 'connected',
  });

  // replyResult
  assert.equal(parseHostToWebviewMessage({ kind: 'replyResult' }), undefined, 'missing replyResult fields rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'replyResult', callId: 'c1' }), undefined, 'missing attemptId/ok rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'replyResult', callId: '', attemptId: 1, ok: true, companionKey: '/ws', session: 's1', generation: 0, webviewId: 'v1' }), undefined, 'empty callId rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'replyResult', callId: 'c1', attemptId: '1', ok: true, companionKey: '/ws', session: 's1', generation: 0, webviewId: 'v1' }), undefined, 'non-number attemptId rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'replyResult', callId: 'c1', attemptId: 1, ok: 'yes', companionKey: '/ws', session: 's1', generation: 0, webviewId: 'v1' }), undefined, 'non-boolean ok rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'replyResult', callId: 'c1', attemptId: 1, ok: true, companionKey: '', session: 's1', generation: 0, webviewId: 'v1' }), undefined, 'empty companionKey rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'replyResult', callId: 'c1', attemptId: 1, ok: true, companionKey: '/ws', session: '', generation: 0, webviewId: 'v1' }), undefined, 'empty session rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'replyResult', callId: 'c1', attemptId: 1, ok: true, companionKey: '/ws', session: 's1', generation: -1, webviewId: 'v1' }), undefined, 'negative generation rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'replyResult', callId: 'c1', attemptId: 1, ok: true, companionKey: '/ws', session: 's1', generation: 0, webviewId: '' }), undefined, 'empty webviewId rejected');
  assert.deepEqual(parseHostToWebviewMessage({
    kind: 'replyResult',
    callId: 'c1',
    attemptId: 1,
    ok: true,
    text: 'ans',
    companionKey: '/ws',
    session: 's1',
    generation: 0,
    webviewId: 'v1',
  }), {
    kind: 'replyResult',
    callId: 'c1',
    attemptId: 1,
    ok: true,
    error: undefined,
    text: 'ans',
    companionKey: '/ws',
    session: 's1',
    generation: 0,
    webviewId: 'v1',
  });

  // sessionCreated
  assert.equal(parseHostToWebviewMessage({ kind: 'sessionCreated' }), undefined, 'missing sessionCreated fields rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'sessionCreated', companionKey: '', session: 's1', creationTaskId: 'task-1', webviewId: 'v1' }), undefined, 'empty companionKey rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'sessionCreated', companionKey: '/ws', session: '', creationTaskId: 'task-1', webviewId: 'v1' }), undefined, 'empty session rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'sessionCreated', companionKey: '/ws', session: 's1', creationTaskId: '', webviewId: 'v1' }), undefined, 'empty creationTaskId rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'sessionCreated', companionKey: '/ws', session: 's1', webviewId: 'v1' }), undefined, 'missing creationTaskId rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'sessionCreated', companionKey: '/ws', session: 's1', creationTaskId: 'task-1' }), undefined, 'missing webviewId rejected');
  assert.deepEqual(parseHostToWebviewMessage({ kind: 'sessionCreated', companionKey: '/ws', session: 's1', creationTaskId: 'task-1', webviewId: 'v1' }), {
    kind: 'sessionCreated',
    companionKey: '/ws',
    session: 's1',
    creationTaskId: 'task-1',
    webviewId: 'v1',
  });

  // sessionCreationFailed
  assert.equal(parseHostToWebviewMessage({ kind: 'sessionCreationFailed' }), undefined, 'missing fields rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'sessionCreationFailed', companionKey: '/ws', creationTaskId: 'task-1' }), undefined, 'missing webviewId rejected');
  assert.deepEqual(parseHostToWebviewMessage({ kind: 'sessionCreationFailed', companionKey: '/ws', creationTaskId: 'task-1', webviewId: 'v1', error: 'boom' }), {
    kind: 'sessionCreationFailed',
    companionKey: '/ws',
    creationTaskId: 'task-1',
    webviewId: 'v1',
    error: 'boom',
  });

  // mentions
  assert.equal(parseHostToWebviewMessage({ kind: 'mentions' }), undefined, 'missing mentions fields rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'mentions', files: 'a.ts', reqId: 1, target: 'general' }), undefined, 'non-array files rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'mentions', files: ['a.ts'], reqId: '1', target: 'general' }), undefined, 'non-number reqId rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'mentions', files: ['a.ts'], reqId: 1, target: 123 }), undefined, 'non-string target rejected');
  assert.deepEqual(parseHostToWebviewMessage({ kind: 'mentions', files: ['a.ts', 'b.ts'], reqId: 1, target: 'general' }), {
    kind: 'mentions',
    files: ['a.ts', 'b.ts'],
    reqId: 1,
    target: 'general',
  });

  // suggestion
  assert.equal(parseHostToWebviewMessage({ kind: 'suggestion' }), undefined, 'missing suggestion fields rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'suggestion', text: 123, reqId: 1, target: 'general' }), undefined, 'non-string text rejected');
  assert.equal(parseHostToWebviewMessage({ kind: 'suggestion', text: 'hi', reqId: '1', target: 'general' }), undefined, 'non-number reqId rejected');
  assert.deepEqual(parseHostToWebviewMessage({ kind: 'suggestion', text: 'continue', reqId: 3, target: 'general' }), {
    kind: 'suggestion',
    text: 'continue',
    reqId: 3,
    target: 'general',
  });
});

test('dispatchHostMessage validates and safely dispatches inbound host messages', () => {
  const handled: string[] = [];
  const handlers = {
    onRows: () => { handled.push('rows'); },
    onState: () => { handled.push('state'); },
    onInfo: () => { handled.push('info'); },
    onCompose: () => { handled.push('compose'); },
    onNote: () => { handled.push('note'); },
    onReplyResult: () => { handled.push('replyResult'); },
    onSessionCreated: () => { handled.push('sessionCreated'); },
    onSessionCreationFailed: () => { handled.push('sessionCreationFailed'); },
    onMentions: () => { handled.push('mentions'); },
    onSuggestion: () => { handled.push('suggestion'); },
  };

  assert.equal(dispatchHostMessage(null, handlers), false);
  assert.equal(dispatchHostMessage('not-an-object', handlers), false);
  assert.equal(dispatchHostMessage({ kind: 'unknown' }, handlers), false);
  assert.equal(dispatchHostMessage({ kind: 'rows' }, handlers), false, 'malformed rows rejected');
  assert.equal(dispatchHostMessage({ kind: 'state' }, handlers), false, 'malformed state rejected');
  assert.equal(dispatchHostMessage({ kind: 'compose' }, handlers), false, 'malformed compose rejected');

  assert.equal(dispatchHostMessage({ kind: 'rows', session: 's1', rows: [], ask: null, refs: [] }, handlers), true);
  assert.equal(dispatchHostMessage({ kind: 'state', state: { state: State.Attached }, note: { text: 'ok', offerStart: false } }, handlers), true);
  assert.equal(dispatchHostMessage({ kind: 'info', state: 'idle', label: 'idle', version: '1.0' }, handlers), true);
  assert.equal(dispatchHostMessage({ kind: 'compose', text: 'prefix' }, handlers), true);
  assert.equal(dispatchHostMessage({ kind: 'note', text: 'notice' }, handlers), true);
  assert.equal(dispatchHostMessage({
    kind: 'replyResult',
    callId: 'c1',
    attemptId: 1,
    ok: true,
    companionKey: '/ws',
    session: 's1',
    generation: 0,
    webviewId: 'v1',
  }, handlers), true);
  assert.equal(dispatchHostMessage({
    kind: 'sessionCreated',
    companionKey: '/ws',
    session: 's1',
    creationTaskId: 'task-1',
    webviewId: 'v1',
  }, handlers), true);
  assert.equal(dispatchHostMessage({
    kind: 'sessionCreationFailed',
    companionKey: '/ws',
    creationTaskId: 'task-1',
    webviewId: 'v1',
    error: 'fail',
  }, handlers), true);
  assert.equal(dispatchHostMessage({ kind: 'mentions', files: ['a.ts'], reqId: 1, target: 'general' }, handlers), true);
  assert.equal(dispatchHostMessage({ kind: 'suggestion', text: 'complete', reqId: 2, target: 'general' }, handlers), true);

  assert.deepEqual(handled, ['rows', 'state', 'info', 'compose', 'note', 'replyResult', 'sessionCreated', 'sessionCreationFailed', 'mentions', 'suggestion']);
});

test('createWebviewInputAdapter controls answer mode and submits responses', () => {
  const posted: WebviewToHostMessage[] = [];
  const bridge: WebviewBridge = {
    postMessage(msg) { posted.push(msg); },
  };
  const actions = createWebviewActionAdapter(bridge);
  const state = createAnswerState();

  const listeners: Record<string, ((e?: any) => void)[]> = {};
  const createElement = (tag: string) => {
    const el = {
      tagName: tag,
      value: '',
      textContent: '',
      placeholder: '',
      hidden: false,
      focusCalled: false,
      focus() { el.focusCalled = true; },
      addEventListener(evt: string, fn: any) {
        (listeners[evt] = listeners[evt] || []).push(fn);
      },
      removeEventListener(evt: string, fn: any) {
        listeners[evt] = (listeners[evt] || []).filter((f: any) => f !== fn);
      },
    };
    return el as unknown as HTMLElement;
  };

  const sayEl = createElement('textarea') as HTMLTextAreaElement;
  const sendEl = createElement('button');
  const replyModeEl = createElement('div');
  const replyTargetEl = createElement('span');
  const replyCancelEl = createElement('button');
  const noteEl = createElement('div');
  const hintEl = createElement('div');

  const inputAdapter = createWebviewInputAdapter(
    {
      say: sayEl,
      sendBtn: sendEl,
      replyModeEl,
      replyTargetEl,
      replyCancelEl,
      noteEl,
      hintEl,
    },
    actions,
    state,
  );

  // 1. General say submission (in empty session, creationTaskId is issued)
  sayEl.value = 'hello agent';
  inputAdapter.send();
  assert.deepEqual(posted.pop(), { kind: 'say', text: 'hello agent', creationTaskId: 'create-1' });
  assert.equal(sayEl.value, '');

  // 2. Enter answer mode
  inputAdapter.enterAnswerMode('q1', 'Confirm deployment');
  assert.equal(replyModeEl.hidden, false);
  assert.equal(replyTargetEl.textContent, 'Confirm deployment');
  assert.equal(sendEl.textContent, '답변');

  // 3. Submit choice
  inputAdapter.onContextChange('/work/ws', 'sess-1', null, 0, 'view-1');
  const choiceOk = inputAdapter.submitChoice('q1', 'yes');
  assert.equal(choiceOk, true);
  assert.deepEqual(posted.pop(), {
    kind: 'reply',
    callId: 'q1',
    text: 'yes',
    attemptId: 1,
    companionKey: '/work/ws',
    session: 'sess-1',
    generation: 0,
    webviewId: 'view-1',
  });
  assert.equal(replyModeEl.hidden, true);
  assert.equal(sendEl.textContent, 'Send');

  // 4. Handle compose prepends lead without losing typed input
  sayEl.value = 'remaining question';
  let selectionStart = -1;
  let selectionEnd = -1;
  (sayEl as any).setSelectionRange = (start: number, end: number) => {
    selectionStart = start;
    selectionEnd = end;
  };
  inputAdapter.handleCompose('Review: ');
  assert.equal(sayEl.value, 'Review: remaining question');
  assert.equal(selectionStart, 8);
  assert.equal(selectionEnd, 8);

  // 5. Dispose cleans up
  inputAdapter.dispose();
});

test('createWebviewReceiveHandlers integrates all inbound messages with typed handlers', () => {
  const posted: WebviewToHostMessage[] = [];
  const bridge: WebviewBridge = {
    postMessage(msg) { posted.push(msg); },
  };
  const actions = createWebviewActionAdapter(bridge);
  const state = createAnswerState();

  const createElement = (tag: string) => ({
    tagName: tag,
    value: '',
    textContent: '',
    placeholder: '',
    hidden: false,
    focus() {},
    addEventListener() {},
    removeEventListener() {},
    setSelectionRange() {},
  });

  const sayEl = createElement('textarea') as unknown as HTMLTextAreaElement;
  const sendEl = createElement('button') as unknown as HTMLElement;
  const replyModeEl = createElement('div') as unknown as HTMLElement;
  const replyTargetEl = createElement('span') as unknown as HTMLElement;
  const replyCancelEl = createElement('button') as unknown as HTMLElement;
  const noteEl = createElement('div') as unknown as HTMLElement;
  const hintEl = createElement('div') as unknown as HTMLElement;

  const inputAdapter = createWebviewInputAdapter(
    { say: sayEl, sendBtn: sendEl, replyModeEl, replyTargetEl, replyCancelEl, noteEl, hintEl },
    actions,
    state
  );

  let currentSession = 'session-0';
  let clearedCalls = false;
  let drawnRowsCount = 0;
  let drawnAskCallId = '';
  let drawnRefsCount = 0;
  let drawnStateNote = '';
  let drawnInfoLabel = '';
  let noteText = '';

  const scrollEl = {
    scrollHeight: 500,
    scrollTop: 100,
    clientHeight: 400,
  } as unknown as HTMLElement;

  const handlers = createWebviewReceiveHandlers({
    inputAdapter,
    answerState: state,
    getCurrentAsk: () => null,
    getCurrentSession: () => currentSession,
    setCurrentSession: (s) => { currentSession = s; },
    clearExpandedCallIds: () => { clearedCalls = true; },
    drawRows: (r) => { drawnRowsCount = r.length; },
    drawAsk: (a) => { drawnAskCallId = a ? a.callId : ''; },
    drawRefs: (rs) => { drawnRefsCount = rs.length; },
    drawState: (n) => { drawnStateNote = n.text; },
    drawInfo: (i) => { drawnInfoLabel = i.label; },
    setNoteText: (t) => { noteText = t; },
    getNoteText: () => noteText,
    scrollContainer: scrollEl,
  });

  // 1. rows (different session triggers clearExpandedCallIds)
  dispatchHostMessage(
    {
      kind: 'rows',
      session: 'session-1',
      rows: [{ who: 'agent', label: 'magi', text: 'turn' }],
      ask: { kind: 'question', callId: 'ask-1', what: 'Approve?', options: [] },
      refs: ['ref.ts'],
    },
    handlers
  );
  assert.equal(currentSession, 'session-1');
  assert.equal(clearedCalls, true);
  assert.equal(drawnRowsCount, 1);
  assert.equal(drawnAskCallId, 'ask-1');
  assert.equal(drawnRefsCount, 1);

  // 2. compose
  dispatchHostMessage({ kind: 'compose', text: 'prefix ' }, handlers);
  assert.ok(sayEl.value.startsWith('prefix '));

  // 3. state
  dispatchHostMessage({ kind: 'state', state: { state: State.Working }, note: { text: 'running test', offerStart: false } }, handlers);
  assert.equal(drawnStateNote, 'running test');

  // 4. info
  dispatchHostMessage({ kind: 'info', state: 'idle', label: 'Daemon Ready', version: '2.0.0' }, handlers);
  assert.equal(drawnInfoLabel, 'Daemon Ready');

  // 5. note
  dispatchHostMessage({ kind: 'note', text: 'connecting...' }, handlers);
  assert.equal(noteText, 'connecting...');
});

class MockNode {
  nodeType: number = 1;
  textContent: string = '';
  childNodes: MockNode[] = [];
  parentNode: MockNode | null = null;
  dataset: Record<string, string> = {};
  className: string = '';
  tagName: string = '';
  attributes: Record<string, string> = {};

  appendChild(child: MockNode): MockNode {
    child.parentNode = this;
    this.childNodes.push(child);
    return child;
  }
  setAttribute(k: string, v: string) {
    this.attributes[k] = v;
  }
  getAttribute(k: string): string | undefined {
    return this.attributes[k];
  }
}

class MockTextNode extends MockNode {
  override nodeType = 3;
  constructor(text: string) {
    super();
    this.textContent = text;
  }
}

class MockElement extends MockNode {
  get href(): string { return this.attributes['href'] || ''; }
  set href(v: string) { this.attributes['href'] = v; }
  get target(): string { return this.attributes['target'] || ''; }
  set target(v: string) { this.attributes['target'] = v; }
  get rel(): string { return this.attributes['rel'] || ''; }
  set rel(v: string) { this.attributes['rel'] = v; }

  constructor(tagName: string) {
    super();
    this.tagName = tagName.toUpperCase();
  }
}

class MockDocument {
  createElement(tag: string): MockElement {
    return new MockElement(tag);
  }
  createTextNode(text: string): MockTextNode {
    return new MockTextNode(text);
  }
}

function render(markdown: string): MockElement {
  const doc = new MockDocument();
  const container = new MockElement('DIV');
  renderMarkdown(container as any, markdown, { document: doc as any });
  return container;
}

function collectText(node: MockNode): string {
  if (node.nodeType === 3) return node.textContent;
  if (node.tagName === 'BR') return '\n';
  return node.childNodes.map(collectText).join('');
}

test('renderMarkdown clears container on empty input', () => {
  const container = render('');
  assert.equal(container.childNodes.length, 0);
  assert.equal(container.textContent, '');
});

test('renderMarkdown parses headings h1 through h6', () => {
  const container = render('# Heading 1\n## Heading 2\n### Heading 3');
  assert.equal(container.childNodes.length, 3);
  assert.equal(container.childNodes[0].tagName, 'H1');
  assert.equal(collectText(container.childNodes[0]), 'Heading 1');
  assert.equal(container.childNodes[1].tagName, 'H2');
  assert.equal(collectText(container.childNodes[1]), 'Heading 2');
  assert.equal(container.childNodes[2].tagName, 'H3');
  assert.equal(collectText(container.childNodes[2]), 'Heading 3');
});

test('renderMarkdown parses closed fenced code blocks with language', () => {
  const md = '```typescript\nconst x = 1;\nconsole.log(x);\n```';
  const container = render(md);
  assert.equal(container.childNodes.length, 1);
  const pre = container.childNodes[0] as MockElement;
  assert.equal(pre.tagName, 'PRE');
  assert.equal(pre.dataset.lang, 'typescript');
  assert.equal(pre.childNodes.length, 1);
  const code = pre.childNodes[0] as MockElement;
  assert.equal(code.tagName, 'CODE');
  assert.equal(code.textContent, 'const x = 1;\nconsole.log(x);');
});

test('renderMarkdown handles streaming unclosed code fence gracefully', () => {
  const md = '```python\ndef greet():\n    return "hello"';
  const container = render(md);
  assert.equal(container.childNodes.length, 1);
  const pre = container.childNodes[0] as MockElement;
  assert.equal(pre.tagName, 'PRE');
  assert.equal(pre.dataset.lang, 'python');
  const code = pre.childNodes[0] as MockElement;
  assert.equal(code.tagName, 'CODE');
  assert.equal(code.textContent, 'def greet():\n    return "hello"');
});

test('renderMarkdown parses diff blocks with syntax highlighting classes', () => {
  const md = '```diff\n--- a/file.ts\n+++ b/file.ts\n@@ -1,3 +1,4 @@\n-const oldVal = 1;\n+const newVal = 2;\n const keep = 3;\n```';
  const container = render(md);
  assert.equal(container.childNodes.length, 1);
  const pre = container.childNodes[0] as MockElement;
  assert.equal(pre.tagName, 'PRE');
  const code = pre.childNodes[0] as MockElement;
  assert.equal(code.tagName, 'CODE');
  const spans = code.childNodes as MockElement[];
  assert.ok(spans.some(s => s.className.includes('diff-del') && s.textContent.includes('const oldVal = 1;')));
  assert.ok(spans.some(s => s.className.includes('diff-add') && s.textContent.includes('const newVal = 2;')));
  assert.ok(spans.some(s => s.className.includes('diff-hunk') && s.textContent.includes('@@ -1,3 +1,4 @@')));
});

test('renderMarkdown parses blockquotes', () => {
  const md = '> Line 1\n> Line 2 with **bold**';
  const container = render(md);
  assert.equal(container.childNodes.length, 1);
  const bq = container.childNodes[0] as MockElement;
  assert.equal(bq.tagName, 'BLOCKQUOTE');
  assert.equal(collectText(bq), 'Line 1\nLine 2 with bold');
  const strong = bq.childNodes.find(n => n.tagName === 'STRONG');
  assert.ok(strong, 'blockquote should contain strong tag for bold');
});

test('renderMarkdown parses unordered lists', () => {
  const md = '- item 1\n- item 2 with `code`\n* item 3';
  const container = render(md);
  assert.equal(container.childNodes.length, 1);
  const ul = container.childNodes[0] as MockElement;
  assert.equal(ul.tagName, 'UL');
  assert.equal(ul.childNodes.length, 3);
  assert.equal(ul.childNodes[0].tagName, 'LI');
  assert.equal(collectText(ul.childNodes[0]), 'item 1');
  const code = ul.childNodes[1].childNodes.find(n => n.tagName === 'CODE');
  assert.ok(code, 'li should contain code element');
  assert.equal(code?.textContent, 'code');
});

test('renderMarkdown parses ordered lists', () => {
  const md = '1. first\n2. second';
  const container = render(md);
  assert.equal(container.childNodes.length, 1);
  const ol = container.childNodes[0] as MockElement;
  assert.equal(ol.tagName, 'OL');
  assert.equal(ol.childNodes.length, 2);
  assert.equal(collectText(ol.childNodes[0]), 'first');
  assert.equal(collectText(ol.childNodes[1]), 'second');
});

test('renderMarkdown parses tables into thead, tbody, th, and td', () => {
  const md = '| Name | Age |\n| --- | --- |\n| Alice | 30 |\n| Bob | 25 |';
  const container = render(md);
  assert.equal(container.childNodes.length, 1);
  const table = container.childNodes[0] as MockElement;
  assert.equal(table.tagName, 'TABLE');
  assert.equal(table.childNodes.length, 2);
  const thead = table.childNodes[0] as MockElement;
  assert.equal(thead.tagName, 'THEAD');
  const tbody = table.childNodes[1] as MockElement;
  assert.equal(tbody.tagName, 'TBODY');
  assert.equal(tbody.childNodes.length, 2);
});

test('renderMarkdown parses inline styles: bold, italic, strikethrough, inline code, and links', () => {
  const md = 'This has **bold**, *italic*, ***both***, ~~strike~~, `foo()`, and [Open Magi](https://github.com/sayaya1090/magi).';
  const container = render(md);
  assert.equal(container.childNodes.length, 1);
  const p = container.childNodes[0] as MockElement;
  assert.equal(p.tagName, 'P');

  const tags = p.childNodes.map(n => n.tagName).filter(Boolean);
  assert.ok(tags.includes('STRONG'));
  assert.ok(tags.includes('EM'));
  assert.ok(tags.includes('DEL'));
  assert.ok(tags.includes('CODE'));
  assert.ok(tags.includes('A'));

  const a = p.childNodes.find(n => n.tagName === 'A') as MockElement;
  assert.equal(a.href, 'https://github.com/sayaya1090/magi');
  assert.equal(a.target, '_blank');
  assert.equal(a.rel, 'noreferrer noopener');
  assert.equal(collectText(a), 'Open Magi');
});

test('renderMarkdown rejects unsafe javascript: links and preserves them as plain text', () => {
  const md = 'Click [here](javascript:alert("pwned")) for a prize';
  const container = render(md);
  const p = container.childNodes[0] as MockElement;
  const a = p.childNodes.find(n => n.tagName === 'A');
  assert.equal(a, undefined, 'unsafe javascript: link must not create an <a> element');
  assert.ok(collectText(p).includes('javascript:alert("pwned")'));
});

test('renderMarkdown preserves raw html tags as text nodes rather than parsing them (XSS guard)', () => {
  const md = 'Here is some script: <script>alert(1)</script> and <img src=x onerror=alert(2)>';
  const container = render(md);
  const p = container.childNodes[0] as MockElement;
  const scriptTag = p.childNodes.find(n => n.tagName === 'SCRIPT');
  const imgTag = p.childNodes.find(n => n.tagName === 'IMG');
  assert.equal(scriptTag, undefined, '<script> must not become a DOM element');
  assert.equal(imgTag, undefined, '<img> must not become a DOM element');
  assert.ok(collectText(p).includes('<script>alert(1)</script>'));
  assert.ok(collectText(p).includes('<img src=x onerror=alert(2)>'));
});

test('renderMarkdown parses horizontal rule', () => {
  const md = 'Intro\n\n---\n\nOutro';
  const container = render(md);
  assert.equal(container.childNodes.length, 3);
  assert.equal(container.childNodes[0].tagName, 'P');
  assert.equal(container.childNodes[1].tagName, 'HR');
  assert.equal(container.childNodes[2].tagName, 'P');
});

test('renderMarkdown preserves 3-backtick fence inside 4-backtick fence', () => {
  const md = '````markdown\nHere is an example:\n```typescript\nconst a = 1;\n```\nDone.\n````';
  const container = render(md);
  assert.equal(container.childNodes.length, 1);
  const pre = container.childNodes[0] as MockElement;
  assert.equal(pre.tagName, 'PRE');
  assert.equal(pre.dataset.lang, 'markdown');
  const code = pre.childNodes[0] as MockElement;
  assert.equal(code.tagName, 'CODE');
  const text = code.textContent;
  assert.ok(text.includes('```typescript\nconst a = 1;\n```'));
  assert.ok(text.includes('Here is an example:'));
  assert.ok(text.includes('Done.'));
});

test('renderMarkdown preserves nested list hierarchy and ordered list start numbers', () => {
  const md = '- parent 1\n  - child 1\n  - child 2\n- parent 2\n\n3. third\n4. fourth';
  const container = render(md);
  assert.equal(container.childNodes.length, 2);

  // Unordered nested list
  const ul = container.childNodes[0] as MockElement;
  assert.equal(ul.tagName, 'UL');
  assert.equal(ul.childNodes.length, 2); // 2 parent items

  const parentLi1 = ul.childNodes[0] as MockElement;
  assert.equal(parentLi1.tagName, 'LI');
  const childUl = parentLi1.childNodes.find(n => n.tagName === 'UL') as MockElement;
  assert.ok(childUl, 'parent li must contain child ul');
  assert.equal(childUl.childNodes.length, 2);

  // Ordered list with start=3
  const ol = container.childNodes[1] as MockElement;
  assert.equal(ol.tagName, 'OL');
  assert.equal(ol.getAttribute('start'), '3');
  assert.equal(ol.childNodes.length, 2);
  assert.equal(collectText(ol.childNodes[0]), 'third');
  assert.equal(collectText(ol.childNodes[1]), 'fourth');
});

test('classifyDiffLines and renderMarkdown correctly classify hunk-internal deletions and avoid guessing diff without language', () => {
  // Test classifyDiffLines with --- inside a hunk
  const hunkLines = [
    '@@ -1,3 +1,3 @@',
    '--- a/example',
    '+++ b/example',
    '-old line',
    '+new line',
  ];
  const classified = classifyDiffLines(hunkLines);
  assert.equal(classified[0].cls, 'diff-hunk-header');
  assert.equal(classified[1].cls, 'diff-deleted', '--- a/example in hunk must be classified as diff-deleted');
  assert.equal(classified[2].cls, 'diff-added', '+++ b/example in hunk must be classified as diff-added');
  assert.equal(classified[3].cls, 'diff-deleted');
  assert.equal(classified[4].cls, 'diff-added');

  // Markdown diff rendering
  const diffMd = '```diff\n@@ -1,2 +1,2 @@\n--- a/example\n+++ b/example\n```';
  const container = render(diffMd);
  const pre = container.childNodes[0] as MockElement;
  const code = pre.childNodes[0] as MockElement;
  const spans = code.childNodes as MockElement[];
  assert.ok(spans.some(s => s.className.includes('diff-deleted') && s.textContent.includes('--- a/example')));

  // Code block without language starting with + or - must NOT be classified as diff
  const nonDiffMd = '```\n+ 1\n- 2\n```';
  const containerNonDiff = render(nonDiffMd);
  const preNonDiff = containerNonDiff.childNodes[0] as MockElement;
  assert.equal(preNonDiff.dataset.lang, undefined);
  const codeNonDiff = preNonDiff.childNodes[0] as MockElement;
  assert.equal(codeNonDiff.childNodes.length, 0); // plain text node inside code
  assert.equal(codeNonDiff.textContent, '+ 1\n- 2');
});

test('host notRunning state roundtrip delivers offerStart and note to handler', () => {
  const hostState = notRunning();
  const hostNote = panelNote(hostState);
  const rawMsg = {
    kind: 'state',
    state: hostState,
    note: hostNote,
  };

  const parsed = parseHostToWebviewMessage(rawMsg);
  assert.ok(parsed);
  assert.equal(parsed.kind, 'state');
  assert.equal(parsed.state.state, State.NotRunning);
  assert.equal(parsed.note.offerStart, true);
  assert.ok(parsed.note.text.length > 0);

  let drawnNote: any = null;
  const dummyHandlers = {
    onState: (m: any) => { drawnNote = m; },
  };
  const dispatched = dispatchHostMessage(rawMsg, dummyHandlers as any);
  assert.equal(dispatched, true);
  assert.equal(drawnNote.note.offerStart, true);
});

test('renderDiff uses common classifyDiffLines and removes obsolete fallback parser', () => {
  const chatSrc = fs.readFileSync(path.join(WEB, 'chat_html.ts'), 'utf8');
  const fnStart = chatSrc.indexOf('function renderDiff(');
  assert.ok(fnStart > 0, 'renderDiff not found');
  const fnBody = chatSrc.slice(fnStart, chatSrc.indexOf('\nfunction ', fnStart));
  assert.ok(fnBody.includes('classifyDiffLines(lines)'), 'renderDiff must delegate to classifyDiffLines');
  assert.ok(!fnBody.includes('hunkMatch'), 'obsolete duplicate fallback parser must be removed from renderDiff');
  assert.ok(!fnBody.includes('oldRemaining'), 'obsolete hunk tracking must be removed from renderDiff');
});

test('SuggestController manages debounce timer, bumps reqId on invalidate, and rejects stale/mismatched results', async () => {
  const ctrl = createSuggestController();
  const posted: any[] = [];
  const mockActions = {
    mention: (name: string, reqId: number, target: string) => posted.push({ kind: 'mention', name, reqId, target }),
    suggest: (text: string, reqId: number, target: string) => posted.push({ kind: 'suggest', text, reqId, target }),
  } as any;

  // 1. Schedule mention
  const r1 = ctrl.scheduleInput({
    text: 'hello @world',
    target: 'general',
    actions: mockActions,
    delayMs: 20,
  });
  assert.equal(r1, 1);
  assert.equal(ctrl.getReqId(), 1);
  assert.equal(ctrl.getCurrentTarget(), 'general');

  // Before timer fires, schedule suggest
  const r2 = ctrl.scheduleInput({
    text: 'some long input text',
    target: 'q1',
    actions: mockActions,
    delayMs: 20,
  });
  assert.equal(r2, 2, 'second schedule bumps reqId');
  assert.equal(ctrl.getCurrentTarget(), 'q1');

  // Wait for timer
  await new Promise((r) => setTimeout(r, 40));
  // r1 mention was cancelled by r2; only r2 suggest was posted
  assert.equal(posted.length, 1);
  assert.deepEqual(posted[0], { kind: 'suggest', text: 'some long input text', reqId: 2, target: 'q1' });

  // 2. Reject stale reqId or target
  assert.equal(ctrl.acceptSuggestion('old result', 1, 'q1'), false, 'stale reqId 1 must be rejected');
  assert.equal(ctrl.acceptSuggestion('wrong target', 2, 'general'), false, 'wrong target general must be rejected');
  assert.equal(ctrl.getSuggestion(), '', 'rejected suggestion must not be saved');

  // Accept valid suggestion
  assert.equal(ctrl.acceptSuggestion('valid suggestion', 2, 'q1'), true);
  assert.equal(ctrl.getSuggestion(), 'valid suggestion');

  // Clear suggestion
  ctrl.clearSuggestion();
  assert.equal(ctrl.getSuggestion(), '');

  // Accept valid mentions
  assert.equal(ctrl.acceptMentions(['foo.ts', 'bar.ts'], 2, 'q1'), true);
  assert.deepEqual(ctrl.getMentions(), ['foo.ts', 'bar.ts']);

  // 3. invalidate clears active state and bumps reqId
  ctrl.invalidate();
  assert.equal(ctrl.getReqId(), 3);
  assert.equal(ctrl.getSuggestion(), '');
  assert.deepEqual(ctrl.getMentions(), []);

  // 4. onSessionChange invalidates and resets context
  ctrl.scheduleInput({
    text: 'abc def ghi',
    target: 'q2',
    actions: mockActions,
    delayMs: 50,
  });
  ctrl.onSessionChange('session-xyz');
  assert.equal(ctrl.getCurrentSession(), 'session-xyz');
  assert.equal(ctrl.getCurrentTarget(), 'general');
  assert.equal(ctrl.acceptSuggestion('late suggestion', 4, 'q2'), false, 'late suggestion after session change must be rejected');

  ctrl.dispose();
});

test('WebviewInputAdapter and receiveHandlers unify autocompletion invalidation across edit, mode switch, submit, and session change', async () => {
  const elements = {
    say: { value: '', placeholder: '', focus() {}, setSelectionRange() {}, addEventListener() {}, removeEventListener() {} } as any,
    sendBtn: { textContent: '', addEventListener() {}, removeEventListener() {} } as any,
    replyModeEl: { hidden: true } as any,
    replyTargetEl: { textContent: '' } as any,
    replyCancelEl: { addEventListener() {}, removeEventListener() {} } as any,
    noteEl: { textContent: '' } as any,
    hintEl: { textContent: '' } as any,
  };
  const posted: any[] = [];
  const bridge = { postMessage: (m: any) => posted.push(m) };
  const actions = createWebviewActionAdapter(bridge);
  const answerState = createAnswerState();
  const inputAdapter = createWebviewInputAdapter(elements, actions, answerState);
  const ctrl = inputAdapter.getSuggestController();

  // Mode switch invalidates
  inputAdapter.enterAnswerMode('call-1', 'Question 1');
  const reqIdAfterEnter = ctrl.getReqId();
  assert.ok(reqIdAfterEnter > 0);

  // Send invalidates
  elements.say.value = 'My answer';
  inputAdapter.send();
  assert.ok(ctrl.getReqId() > reqIdAfterEnter, 'send must bump reqId and invalidate autocompletion');

  // Session change invalidates
  let currentSession = 'session-1';
  const handlers = createWebviewReceiveHandlers({
    inputAdapter,
    answerState,
    getCurrentAsk: () => null,
    getCurrentSession: () => currentSession,
    setCurrentSession: (s) => { currentSession = s; },
    clearExpandedCallIds: () => {},
    drawRows: () => {},
    drawAsk: () => {},
    drawRefs: () => {},
    drawState: () => {},
    drawInfo: () => {},
    setNoteText: () => {},
    getNoteText: () => '',
  });

  const reqIdBeforeSessionChange = ctrl.getReqId();
  handlers.onRows!({ session: 'session-2', rows: [], ask: null, refs: [] });
  assert.equal(currentSession, 'session-2');
  assert.ok(ctrl.getReqId() > reqIdBeforeSessionChange, 'session change in onRows must invalidate autocompletion');

  inputAdapter.dispose();
});

test('Programmatic input modifications (handleCompose and Tab) invalidate in-flight autocompletion', async () => {
  const listeners: Record<string, (e: any) => void> = {};
  const elements = {
    say: {
      value: '',
      placeholder: '',
      focus() {},
      setSelectionRange() {},
      addEventListener: (type: string, fn: any) => { listeners[type] = fn; },
      removeEventListener: () => {},
    } as any,
    sendBtn: { textContent: '', addEventListener() {}, removeEventListener() {} } as any,
    replyModeEl: { hidden: true } as any,
    replyTargetEl: { textContent: '' } as any,
    replyCancelEl: { addEventListener() {}, removeEventListener() {} } as any,
    noteEl: { textContent: '' } as any,
    hintEl: { textContent: '' } as any,
  };
  const posted: any[] = [];
  const bridge = { postMessage: (m: any) => posted.push(m) };
  const actions = createWebviewActionAdapter(bridge);
  const answerState = createAnswerState();
  const inputAdapter = createWebviewInputAdapter(elements, actions, answerState);
  const ctrl = inputAdapter.getSuggestController();

  // 1. handleCompose invalidation:
  // User types text, triggering onInput
  elements.say.value = 'hello';
  listeners['input']?.({});
  const initialReqId = ctrl.getReqId();
  assert.ok(initialReqId > 0);

  // handleCompose is called (e.g. lead-in prepended)
  inputAdapter.handleCompose('/ask ');
  assert.equal(elements.say.value, '/ask hello');
  assert.ok(ctrl.getReqId() > initialReqId, 'handleCompose must bump reqId and invalidate');
  assert.equal(elements.hintEl.textContent, '', 'hint must be cleared on compose');

  // Late suggestion arriving from the previous input before compose
  inputAdapter.handleSuggestion(' world', initialReqId, 'general');
  assert.equal(ctrl.getSuggestion(), '', 'late suggestion with old reqId must be rejected after compose');
  assert.equal(elements.hintEl.textContent, '');

  // 2. Tab acceptance invalidation:
  // Supply a valid suggestion
  const curReqId = ctrl.getReqId();
  inputAdapter.handleSuggestion(' from test', curReqId, 'general');
  assert.equal(ctrl.getSuggestion(), ' from test');
  assert.equal(elements.hintEl.textContent, 'Tab:  from test');

  // Press Tab
  let prevented = false;
  listeners['keydown']?.({ key: 'Tab', preventDefault: () => { prevented = true; } });
  assert.equal(prevented, true);
  assert.equal(elements.say.value, '/ask hello from test');
  assert.equal(elements.hintEl.textContent, '', 'hint must be cleared on Tab accept');
  assert.ok(ctrl.getReqId() > curReqId, 'Tab accept must bump reqId and invalidate');
  assert.equal(ctrl.getSuggestion(), '', 'active suggestion must be empty after Tab accept');

  // Stale suggestion arriving with curReqId is now rejected
  inputAdapter.handleSuggestion(' stale trailing', curReqId, 'general');
  assert.equal(ctrl.getSuggestion(), '');
  assert.equal(elements.hintEl.textContent, '');

  inputAdapter.dispose();
});

test('§4.5 Item 2: 식별자 검증에서 원문 보존 (Trailing whitespace preserved verbatim, whitespace-only rejected)', () => {
  const dirWithSpace = '/workspace/project dir ';
  const sessWithSpace = 'sess-42 ';
  const callIdWithSpace = 'call-99 ';
  const webviewIdWithSpace = 'view-1 ';

  // 1. parseWebviewToHostMessage: reply carries verbatim identifiers
  const parsedReply = parseWebviewToHostMessage({
    kind: 'reply',
    callId: callIdWithSpace,
    text: '내용',
    attemptId: 1,
    companionKey: dirWithSpace,
    session: sessWithSpace,
    generation: 1,
    webviewId: webviewIdWithSpace,
  });
  assert.ok(parsedReply);
  assert.equal(parsedReply?.kind, 'reply');
  if (parsedReply?.kind === 'reply') {
    assert.equal(parsedReply.companionKey, dirWithSpace, 'companionKey with trailing space must be preserved verbatim');
    assert.equal(parsedReply.session, sessWithSpace, 'session with trailing space must be preserved verbatim');
    assert.equal(parsedReply.callId, callIdWithSpace, 'callId with trailing space must be preserved verbatim');
    assert.equal(parsedReply.webviewId, webviewIdWithSpace, 'webviewId with trailing space must be preserved verbatim');
  }

  // 2. parseHostToWebviewMessage: replyResult and sessionCreated carry verbatim identifiers
  const parsedResult = parseHostToWebviewMessage({
    kind: 'replyResult',
    callId: callIdWithSpace,
    attemptId: 1,
    ok: true,
    companionKey: dirWithSpace,
    session: sessWithSpace,
    generation: 1,
    webviewId: webviewIdWithSpace,
  });
  assert.ok(parsedResult);
  if (parsedResult?.kind === 'replyResult') {
    assert.equal(parsedResult.companionKey, dirWithSpace);
    assert.equal(parsedResult.session, sessWithSpace);
    assert.equal(parsedResult.callId, callIdWithSpace);
    assert.equal(parsedResult.webviewId, webviewIdWithSpace);
  }

  const parsedCreated = parseHostToWebviewMessage({
    kind: 'sessionCreated',
    companionKey: dirWithSpace,
    session: sessWithSpace,
    creationTaskId: 'create-1 ',
    webviewId: webviewIdWithSpace,
  });
  assert.ok(parsedCreated);
  if (parsedCreated?.kind === 'sessionCreated') {
    assert.equal(parsedCreated.companionKey, dirWithSpace);
    assert.equal(parsedCreated.session, sessWithSpace);
    assert.equal(parsedCreated.creationTaskId, 'create-1 ');
    assert.equal(parsedCreated.webviewId, webviewIdWithSpace);
  }

  // 3. 공백뿐인 식별자는 엄격 거절
  assert.equal(parseWebviewToHostMessage({
    kind: 'reply',
    callId: '   ',
    text: '내용',
    attemptId: 1,
    companionKey: dirWithSpace,
    session: sessWithSpace,
    generation: 1,
    webviewId: webviewIdWithSpace,
  }), undefined, 'whitespace-only callId must be rejected');

  assert.equal(parseWebviewToHostMessage({
    kind: 'reply',
    callId: callIdWithSpace,
    text: '내용',
    attemptId: 1,
    companionKey: '   ',
    session: sessWithSpace,
    generation: 1,
    webviewId: webviewIdWithSpace,
  }), undefined, 'whitespace-only companionKey must be rejected');

  assert.equal(parseHostToWebviewMessage({
    kind: 'replyResult',
    callId: callIdWithSpace,
    attemptId: 1,
    ok: true,
    companionKey: '   ',
    session: sessWithSpace,
    generation: 1,
    webviewId: webviewIdWithSpace,
  }), undefined, 'whitespace-only companionKey in replyResult must be rejected');
});

test('§4.5 Item 1: onSessionCreated 모드 일치 검증 - 답변 모드일 때 일반 초안 삽입 금지', () => {
  const posted: any[] = [];
  const bridge = { postMessage: (m: any) => posted.push(m) };
  const actions = createWebviewActionAdapter(bridge);
  const state = createAnswerState();

  const listeners: Record<string, (e: any) => void> = {};
  const elements = {
    say: {
      value: '',
      placeholder: '',
      focus() {},
      setSelectionRange() {},
      addEventListener: (type: string, fn: any) => { listeners[type] = fn; },
      removeEventListener: () => {},
    } as any,
    sendBtn: { textContent: '', addEventListener() {}, removeEventListener() {} } as any,
    replyModeEl: { hidden: true } as any,
    replyTargetEl: { textContent: '' } as any,
    replyCancelEl: { addEventListener() {}, removeEventListener() {} } as any,
    noteEl: { textContent: '' } as any,
    hintEl: { textContent: '' } as any,
  };

  const inputAdapter = createWebviewInputAdapter(elements, actions, state);

  // 1. 빈 세션에서 초안 작성 및 생성 시작
  inputAdapter.onContextChange('/ws', '', null, 0, 'view-1');
  elements.say.value = '첫 메시지';
  inputAdapter.send();
  const sayMsg = posted.pop();
  assert.ok(sayMsg.creationTaskId);

  // 생성 진행 중 사용자가 새 일반 초안 작성
  elements.say.value = '새 작업 추가 초안';
  listeners['input']?.({});

  // 2. 답변 모드로 진입 (예: 긴급 질문 도착)
  inputAdapter.enterAnswerMode('urgent-q', '긴급 확인');
  assert.equal(elements.replyModeEl.hidden, false);
  elements.say.value = ''; // 답변 입력창이 비어 있음

  // 3. sessionCreated 이벤트 도착
  inputAdapter.onSessionCreated?.({
    companionKey: '/ws',
    session: 'sess-created',
    creationTaskId: sayMsg.creationTaskId,
    webviewId: 'view-1',
  });

  // 답변 모드이므로 일반 초안이 say.value에 삽입되어서는 안 됨! (§4.5 Item 1)
  assert.equal(elements.say.value, '', 'general draft must NOT be injected while in answer mode');
  // 그러나 상태 저장소에는 정상 바인딩되어 있어야 함
  assert.equal(state.getGeneralDraft('/ws', 'sess-created'), '새 작업 추가 초안');

  // 4. 답변 모드를 종료하고 일반 모드로 돌아왔을 때 일반 초안 복원 확인
  inputAdapter.exitAnswerMode();
  assert.equal(elements.replyModeEl.hidden, true);
  assert.equal(elements.say.value, '새 작업 추가 초안');

  inputAdapter.dispose();
});

test('§4.5 Item 1: 실제 어댑터로 생성 시작 -> 작성 -> 다른 세션 이동 -> 완료 연결 시 세션 격리 검증', () => {
  const posted: any[] = [];
  const bridge = { postMessage: (m: any) => posted.push(m) };
  const actions = createWebviewActionAdapter(bridge);
  const state = createAnswerState();

  const listeners: Record<string, (e: any) => void> = {};
  const elements = {
    say: {
      value: '',
      placeholder: '',
      focus() {},
      setSelectionRange() {},
      addEventListener: (type: string, fn: any) => { listeners[type] = fn; },
      removeEventListener: () => {},
    } as any,
    sendBtn: { textContent: '', addEventListener() {}, removeEventListener() {} } as any,
    replyModeEl: { hidden: true } as any,
    replyTargetEl: { textContent: '' } as any,
    replyCancelEl: { addEventListener() {}, removeEventListener() {} } as any,
    noteEl: { textContent: '' } as any,
    hintEl: { textContent: '' } as any,
  };

  const inputAdapter = createWebviewInputAdapter(elements, actions, state);

  // 1. 빈 세션 C1/''에서 메시지 전송으로 생성 시작
  inputAdapter.onContextChange('/workspace', '', null, 0, 'view-1');
  elements.say.value = 'M1 전송';
  inputAdapter.send();
  const sayMsg = posted.pop();
  assert.equal(sayMsg.kind, 'say');
  assert.ok(sayMsg.creationTaskId);
  const taskId = sayMsg.creationTaskId;

  // 2. 생성이 완료되기 전에 사용자가 추가 초안 M2 작성
  elements.say.value = 'M2 추가 초안';
  listeners['input']?.({});

  // 3. 다른 세션 S2로 이동
  inputAdapter.onContextChange('/workspace', 'sess-2', null, 0, 'view-1');
  assert.equal(elements.say.value, '', 'sess-2 must start with empty draft');

  // 사용자가 S2에서 작업 진행
  elements.say.value = 'S2 진행 중 초안';
  listeners['input']?.({});

  // 4. 이제 호스트로부터 최초 생성 요청 S1의 sessionCreated 도착
  inputAdapter.onSessionCreated?.({
    companionKey: '/workspace',
    session: 'sess-1-created',
    creationTaskId: taskId,
    webviewId: 'view-1',
  });

  // 현재 화면은 S2이므로 S2의 입력창은 영향받지 않아야 함 (§4.5 Item 1)
  assert.equal(elements.say.value, 'S2 진행 중 초안', 'S2 input must not be changed when S1 completes');

  // S1 생성 세션에 M2 초안이 정상 귀속되어 있어야 함
  assert.equal(state.getGeneralDraft('/workspace', 'sess-1-created'), 'M2 추가 초안');

  // 5. 나중에 S1으로 전환했을 때 M2 초안이 복원됨
  inputAdapter.onContextChange('/workspace', 'sess-1-created', null, 0, 'view-1');
  assert.equal(elements.say.value, 'M2 추가 초안');

  inputAdapter.dispose();
});

test('§4.5 Item 1: 빈 세션 연속 전송 동일 작업 귀속, 전송 B 미복원, DRAFT 작성 S2 이동 복원 및 실패 재시도 검증', () => {
  const posted: any[] = [];
  const bridge = { postMessage: (m: any) => posted.push(m) };
  const actions = createWebviewActionAdapter(bridge);
  const state = createAnswerState();

  const listeners: Record<string, (e: any) => void> = {};
  const elements = {
    say: {
      value: '',
      placeholder: '',
      focus() {},
      setSelectionRange() {},
      addEventListener: (type: string, fn: any) => { listeners[type] = fn; },
      removeEventListener: () => {},
    } as any,
    sendBtn: { textContent: '', addEventListener() {}, removeEventListener() {} } as any,
    replyModeEl: { hidden: true } as any,
    replyTargetEl: { textContent: '' } as any,
    replyCancelEl: { addEventListener() {}, removeEventListener() {} } as any,
    noteEl: { textContent: '' } as any,
    hintEl: { textContent: '' } as any,
  };

  const inputAdapter = createWebviewInputAdapter(elements, actions, state);
  inputAdapter.onContextChange('/ws', '', null, 0, 'view-1');

  // 1. A 전송 -> 빈 세션이므로 create-1 발급
  elements.say.value = 'A 메시지';
  inputAdapter.send();
  assert.equal(posted.length, 1);
  assert.equal(posted[0].text, 'A 메시지');
  assert.equal(posted[0].creationTaskId, 'create-1');
  assert.equal(inputAdapter.getActiveCreationTaskId?.(), 'create-1');

  // 2. session-new 대기 중 B 작성 및 전송 -> 동일한 create-1 작업 재사용 (§4.5 Item 1)
  elements.say.value = 'B 메시지';
  listeners['input']?.({});
  inputAdapter.send();
  assert.equal(posted.length, 2);
  assert.equal(posted[1].text, 'B 메시지');
  assert.equal(posted[1].creationTaskId, 'create-1', 'B send must reuse active creationTaskId create-1');
  assert.equal(inputAdapter.getActiveCreationTaskId?.(), 'create-1');

  // 전송 직후 작업 초안은 ''이어야 함 (B가 미전송 초안으로 되살아나지 않아야 함)
  assert.equal(state.getCreationTask('/ws', 'create-1')?.draft, '');

  // 3. 추가 작성 없이 바로 완료된 경우: B가 복원되지 않고 빈 입력 유지 (§4.5 Item 1)
  inputAdapter.onSessionCreated?.({
    companionKey: '/ws',
    session: 'sess-done-no-draft',
    creationTaskId: 'create-1',
    webviewId: 'view-1',
  });
  assert.equal(elements.say.value, '', 'sent message B must NOT be resurrected as draft');
  assert.equal(state.getGeneralDraft('/ws', 'sess-done-no-draft'), '');
  assert.equal(inputAdapter.getActiveCreationTaskId?.(), null, 'creation task must be cleared on completion');

  // 4. 새 빈 세션에서 A 전송 -> B 전송 -> DRAFT 작성 -> S2 이동 -> S1 완료 시나리오
  inputAdapter.onContextChange('/ws', '', null, 0, 'view-1');
  elements.say.value = 'A2 메시지';
  inputAdapter.send();
  assert.equal(posted[2].creationTaskId, 'create-2');

  elements.say.value = 'B2 메시지';
  listeners['input']?.({});
  inputAdapter.send();
  assert.equal(posted[3].creationTaskId, 'create-2', 'B2 must reuse create-2');

  // B2 전송 후 사용자가 DRAFT 작성
  elements.say.value = 'DRAFT 후속 메모';
  listeners['input']?.({});
  assert.equal(state.getCreationTask('/ws', 'create-2')?.draft, 'DRAFT 후속 메모');

  // S2로 이동
  inputAdapter.onContextChange('/ws', 'sess-2', null, 0, 'view-1');
  elements.say.value = 'S2 기존 작업';
  listeners['input']?.({});

  // S1(create-2) 완료 이벤트 도착
  inputAdapter.onSessionCreated?.({
    companionKey: '/ws',
    session: 'sess-1-complete',
    creationTaskId: 'create-2',
    webviewId: 'view-1',
  });

  // S2 입력창은 불변이어야 함 (§4.5 Item 1)
  assert.equal(elements.say.value, 'S2 기존 작업', 'S2 input must remain untouched when S1 completes');

  // S1으로 복귀 시 DRAFT가 복원되고 B2는 복원되지 않음
  inputAdapter.onContextChange('/ws', 'sess-1-complete', null, 0, 'view-1');
  assert.equal(elements.say.value, 'DRAFT 후속 메모', 'DRAFT must be restored on returning to S1');

  // 5. 생성 실패 -> 재시도 시 원래 자료 보존 및 새 작업 ID 발급 검증
  inputAdapter.onContextChange('/ws', '', null, 0, 'view-1');
  elements.say.value = '실패할 요청';
  inputAdapter.send();
  assert.equal(posted[4].creationTaskId, 'create-3');

  // 실패 이벤트 수신
  inputAdapter.onSessionCreationFailed?.({
    companionKey: '/ws',
    creationTaskId: 'create-3',
    webviewId: 'view-1',
    error: 'connection refused',
  });

  // 활성 생성 작업 ID가 초기화되었는지 확인
  assert.equal(inputAdapter.getActiveCreationTaskId?.(), null);
  // 원래 작업 자료는 보존되어 있어야 함
  const failedTask = state.getCreationTask('/ws', 'create-3');
  assert.equal(failedTask?.status, 'failed');
  assert.equal(failedTask?.error, 'connection refused');

  // 재시도 전송 시 새 ID create-4 발급 확인
  elements.say.value = '재시도 요청';
  inputAdapter.send();
  assert.equal(posted[5].creationTaskId, 'create-4', 'retry send must issue new creationTaskId');
  assert.equal(state.getCreationTask('/ws', 'create-4')?.status, 'pending');
  // create-3은 여전히 failed로 보존
  assert.equal(state.getCreationTask('/ws', 'create-3')?.status, 'failed');

  inputAdapter.dispose();
});

test('§4.5 Item 2: dispatchHostMessage -> receiveHandlers -> inputAdapter 거절 수신 경로 전체 상태 불변 및 유효 충돌·직후 rows 검증', () => {
  const posted: any[] = [];
  const bridge = { postMessage: (m: any) => posted.push(m) };
  const actions = createWebviewActionAdapter(bridge);
  const state = createAnswerState();

  const listeners: Record<string, (e: any) => void> = {};
  const elements = {
    say: {
      value: '',
      placeholder: '',
      focus() {},
      setSelectionRange() {},
      addEventListener: (type: string, fn: any) => { listeners[type] = fn; },
      removeEventListener: () => {},
    } as any,
    sendBtn: { textContent: 'Send', addEventListener() {}, removeEventListener() {} } as any,
    replyModeEl: { hidden: true } as any,
    replyTargetEl: { textContent: '' } as any,
    replyCancelEl: { addEventListener() {}, removeEventListener() {} } as any,
    noteEl: { textContent: '' } as any,
    hintEl: { textContent: '' } as any,
  };

  const inputAdapter = createWebviewInputAdapter(elements, actions, state);
  let currentSession = '';
  let currentCompanionKey = '/workspace';
  let currentWebviewId = 'view-1';
  let currentAsk: Ask | null = null;
  const drawnRows: any[] = [];

  const handlers = createWebviewReceiveHandlers({
    inputAdapter,
    answerState: state,
    getCurrentAsk: () => currentAsk,
    getCurrentSession: () => currentSession,
    setCurrentSession: (s: string) => { currentSession = s; },
    getCurrentCompanionKey: () => currentCompanionKey,
    setCurrentCompanionKey: (k: string) => { currentCompanionKey = k; },
    getCurrentWebviewId: () => currentWebviewId,
    setCurrentWebviewId: (w: string) => { currentWebviewId = w; },
    clearExpandedCallIds: () => {},
    resetCurrentAsk: () => { currentAsk = null; },
    drawRows: (r) => { drawnRows.push(r); },
    drawAsk: () => {},
    drawRefs: () => {},
    drawState: () => {},
    drawInfo: () => {},
    setNoteText: () => {},
    getNoteText: () => '',
  });

  // 초기 상태: 빈 세션에서 첫 메시지 전송 후 DRAFT 작성
  inputAdapter.onContextChange('/workspace', '', null, 0, 'view-1');
  elements.say.value = 'M1 전송';
  inputAdapter.send();
  const taskId = posted[0].creationTaskId;
  assert.equal(taskId, 'create-1');

  elements.say.value = 'CURRENT_DRAFT_TEXT';
  listeners['input']?.({});

  // 기준 상태 스냅샷
  assert.equal(inputAdapter.getActiveCreationTaskId?.(), 'create-1');
  assert.equal(inputAdapter.getCurrentSession?.(), '');
  assert.equal(currentSession, '');
  assert.equal(elements.say.value, 'CURRENT_DRAFT_TEXT');
  assert.equal(elements.replyModeEl.hidden, true);

  // 1. 미등록 task ID의 sessionCreated 주입 -> 거절되어 전체 상태 불변이어야 함 (§4.5 Item 2)
  const rejected1 = dispatchHostMessage({
    kind: 'sessionCreated',
    companionKey: '/workspace',
    session: 'sess-unrelated-1',
    creationTaskId: 'unregistered-task-id',
    webviewId: 'view-1',
  }, handlers);
  assert.equal(rejected1, true, 'message format was valid and dispatched');
  assert.equal(inputAdapter.getActiveCreationTaskId?.(), 'create-1', 'activeCreationTaskId must NOT be cleared on rejection');
  assert.equal(inputAdapter.getCurrentSession?.(), '', 'inputAdapter session must NOT change on rejection');
  assert.equal(currentSession, '', 'receiveHandlers session must NOT change on rejection');
  assert.equal(elements.say.value, 'CURRENT_DRAFT_TEXT', 'say.value must NOT change on rejection');
  assert.equal(elements.replyModeEl.hidden, true, 'reply mode must NOT change on rejection');

  // 2. 다른 웹뷰 ID의 sessionCreated 주입 -> 거절되어 전체 상태 불변 (§4.5 Item 2)
  dispatchHostMessage({
    kind: 'sessionCreated',
    companionKey: '/workspace',
    session: 'sess-unrelated-2',
    creationTaskId: 'create-1',
    webviewId: 'view-old-wrong',
  }, handlers);
  assert.equal(inputAdapter.getActiveCreationTaskId?.(), 'create-1');
  assert.equal(inputAdapter.getCurrentSession?.(), '');
  assert.equal(currentSession, '');
  assert.equal(elements.say.value, 'CURRENT_DRAFT_TEXT');

  // 3. 다른 컴패니언의 sessionCreated 주입 -> 거절되어 전체 상태 불변 (§4.5 Item 2)
  dispatchHostMessage({
    kind: 'sessionCreated',
    companionKey: '/workspace-other',
    session: 'sess-unrelated-3',
    creationTaskId: 'create-1',
    webviewId: 'view-1',
  }, handlers);
  assert.equal(inputAdapter.getActiveCreationTaskId?.(), 'create-1');
  assert.equal(inputAdapter.getCurrentSession?.(), '');
  assert.equal(currentSession, '');
  assert.equal(elements.say.value, 'CURRENT_DRAFT_TEXT');

  // 4. 대상 세션에 이미 초안이 있는 유효 생성 완료 (초안 충돌 분기) (§4.5 Item 2)
  // 대상 세션 sess-conflict에 기존 초안 준비
  state.switchContext('/workspace', 'sess-conflict', { webviewId: 'view-1' });
  state.onInputChange('TARGET_EXISTING_DRAFT');
  // 다시 빈 세션으로 복귀
  state.switchContext('/workspace', '', { webviewId: 'view-1' });
  elements.say.value = 'CURRENT_DRAFT_TEXT';

  dispatchHostMessage({
    kind: 'sessionCreated',
    companionKey: '/workspace',
    session: 'sess-conflict',
    creationTaskId: 'create-1',
    webviewId: 'view-1',
  }, handlers);

  // 유효한 완료이므로:
  // - activeCreationTaskId는 해제됨
  assert.equal(inputAdapter.getActiveCreationTaskId?.(), null);
  // - 세션 전환은 양쪽 모두 일관되게 'sess-conflict'로 동기화됨 (§4.5 Item 2)
  assert.equal(inputAdapter.getCurrentSession?.(), 'sess-conflict');
  assert.equal(currentSession, 'sess-conflict');
  // - 대상의 기존 초안은 DOM 입력과 저장소에 모두 일치하게 반영되고 원래 작업 자료도 보존됨 (§4.5 Item 1)
  assert.equal(elements.say.value, 'TARGET_EXISTING_DRAFT', 'say.value must render existing draft on conflict');
  assert.equal(state.getGeneralDraft('/workspace', 'sess-conflict'), 'TARGET_EXISTING_DRAFT');
  assert.equal(state.getState('/workspace', 'sess-conflict').generalDraft, 'TARGET_EXISTING_DRAFT');
  assert.equal(state.getCreationTask('/workspace', 'create-1')?.draft, 'CURRENT_DRAFT_TEXT');
  assert.equal(state.getCreationTask('/workspace', 'create-1')?.status, 'completed');

  // 5. 중복 완료 주입 -> 이미 completed 상태이므로 거절되고 상태 불변 (§4.5 Item 2)
  const prevVal = elements.say.value;
  dispatchHostMessage({
    kind: 'sessionCreated',
    companionKey: '/workspace',
    session: 'sess-conflict',
    creationTaskId: 'create-1',
    webviewId: 'view-1',
  }, handlers);
  assert.equal(inputAdapter.getCurrentSession?.(), 'sess-conflict');
  assert.equal(currentSession, 'sess-conflict');
  assert.equal(elements.say.value, prevVal);

  // 6. sessionCreated 직후 rows 수신 시 비대칭 없이 정상 수신 검증 (§4.5 Item 2)
  drawnRows.length = 0;
  dispatchHostMessage({
    kind: 'rows',
    session: 'sess-conflict',
    rows: [{ who: 'magi', label: 'magi', text: '작업 완료' }],
    ask: null,
    refs: [],
    companionKey: '/workspace',
    webviewId: 'view-1',
  }, handlers);

  assert.equal(drawnRows.length, 1);
  assert.equal(currentSession, 'sess-conflict');
  assert.equal(inputAdapter.getCurrentSession?.(), 'sess-conflict');
  assert.equal(state.getGeneralDraft('/workspace', 'sess-conflict'), 'TARGET_EXISTING_DRAFT');

  inputAdapter.dispose();
});

test('§4.5 Item 3: dispatchHostMessage -> receiveHandlers -> inputAdapter 충돌 시 화면·저장 상태 일치 회귀 검증 (S1 EXISTING -> 빈 세션 생성 시작 -> NEW DRAFT 작성 -> S1 conflict -> 직후 rows -> S2 이동 -> S1 복귀 및 수정 전송)', () => {
  const posted: any[] = [];
  const bridge = { postMessage: (m: any) => posted.push(m) };
  const actions = createWebviewActionAdapter(bridge);
  const state = createAnswerState();

  const listeners: Record<string, (e: any) => void> = {};
  const elements = {
    say: {
      value: '',
      placeholder: '',
      focus() {},
      setSelectionRange() {},
      addEventListener: (type: string, fn: any) => { listeners[type] = fn; },
      removeEventListener: () => {},
    } as any,
    sendBtn: { textContent: '', addEventListener() {}, removeEventListener() {} } as any,
    replyModeEl: { hidden: true } as any,
    replyTargetEl: { textContent: '' } as any,
    replyCancelEl: { addEventListener() {}, removeEventListener() {} } as any,
    noteEl: { textContent: '' } as any,
    hintEl: { textContent: '' } as any,
  };

  const inputAdapter = createWebviewInputAdapter(elements, actions, state);
  let currentSession = '';
  let currentCompanionKey = '/workspace';
  let currentGeneration: number | undefined = 0;
  let currentWebviewId = 'view-1';
  const drawnRows: any[] = [];

  const handlers = createWebviewReceiveHandlers({
    inputAdapter,
    answerState: state,
    getCurrentAsk: () => null,
    getCurrentSession: () => currentSession,
    setCurrentSession: (s) => { currentSession = s; },
    getCurrentCompanionKey: () => currentCompanionKey,
    setCurrentCompanionKey: (k) => { currentCompanionKey = k; },
    getCurrentGeneration: () => currentGeneration,
    setCurrentGeneration: (g) => { currentGeneration = g; },
    getCurrentWebviewId: () => currentWebviewId,
    setCurrentWebviewId: (w) => { currentWebviewId = w; },
    clearExpandedCallIds: () => {},
    drawRows: (rows) => { drawnRows.push(...rows); },
    drawAsk: () => {},
    drawRefs: () => {},
    drawState: () => {},
    drawInfo: () => {},
    setNoteText: () => {},
    getNoteText: () => '',
  });

  // 1. S1에 EXISTING 저장
  inputAdapter.onContextChange('/workspace', 's1', null, 0, 'view-1');
  currentSession = 's1';
  elements.say.value = 'EXISTING';
  listeners['input']?.({});
  assert.equal(elements.say.value, 'EXISTING', 'Step 1: S1 DOM input is EXISTING');
  assert.equal(state.getGeneralDraft('/workspace', 's1'), 'EXISTING', 'Step 1: S1 storage is EXISTING');
  assert.equal(state.getState('/workspace', 's1').generalDraft, 'EXISTING');

  // 2. 빈 세션에서 생성 시작
  inputAdapter.onContextChange('/workspace', '', null, 0, 'view-1');
  currentSession = '';
  assert.equal(elements.say.value, '', 'Step 2: empty session starts with empty input');
  elements.say.value = 'INIT_SEND';
  inputAdapter.send();
  const sayMsg = posted.pop();
  assert.equal(sayMsg.kind, 'say');
  assert.equal(sayMsg.text, 'INIT_SEND');
  assert.ok(sayMsg.creationTaskId);
  const taskId = sayMsg.creationTaskId;
  assert.equal(inputAdapter.getActiveCreationTaskId?.(), taskId);

  // 3. NEW DRAFT 작성
  elements.say.value = 'NEW DRAFT';
  listeners['input']?.({});
  assert.equal(elements.say.value, 'NEW DRAFT', 'Step 3: DOM input has NEW DRAFT');
  assert.equal(state.getCreationTask('/workspace', taskId)?.draft, 'NEW DRAFT', 'Step 3: task draft has NEW DRAFT');
  assert.equal(state.getCreationTask('/workspace', taskId)?.status, 'pending');

  // 4. S1 생성 완료(conflict)
  dispatchHostMessage({
    kind: 'sessionCreated',
    companionKey: '/workspace',
    session: 's1',
    creationTaskId: taskId,
    webviewId: 'view-1',
  }, handlers);

  assert.equal(inputAdapter.getCurrentSession?.(), 's1');
  assert.equal(currentSession, 's1');
  assert.equal(elements.say.value, 'EXISTING', 'Step 4: S1 DOM input must be EXISTING on conflict');
  assert.equal(state.getGeneralDraft('/workspace', 's1'), 'EXISTING', 'Step 4: S1 storage must be EXISTING');
  assert.equal(state.getState('/workspace', 's1').generalDraft, 'EXISTING', 'Step 4: S1 snapshot must be EXISTING');
  assert.equal(state.getCreationTask('/workspace', taskId)?.draft, 'NEW DRAFT', 'Step 4: creation task data must preserve NEW DRAFT');
  assert.equal(state.getCreationTask('/workspace', taskId)?.status, 'completed');

  // 5. 직후 S1 rows
  drawnRows.length = 0;
  dispatchHostMessage({
    kind: 'rows',
    companionKey: '/workspace',
    session: 's1',
    rows: [{ who: 'magi', label: 'magi', text: 'response 1' }],
    ask: null,
    refs: [],
    webviewId: 'view-1',
  }, handlers);

  assert.equal(drawnRows.length, 1);
  assert.equal(inputAdapter.getCurrentSession?.(), 's1');
  assert.equal(currentSession, 's1');
  assert.equal(elements.say.value, 'EXISTING', 'Step 5: DOM input remains EXISTING after rows');
  assert.equal(state.getGeneralDraft('/workspace', 's1'), 'EXISTING', 'Step 5: storage remains EXISTING after rows');
  assert.equal(state.getState('/workspace', 's1').generalDraft, 'EXISTING');
  assert.equal(state.getCreationTask('/workspace', taskId)?.draft, 'NEW DRAFT', 'Step 5: task draft remains NEW DRAFT');

  // 6. S2 이동
  dispatchHostMessage({
    kind: 'rows',
    companionKey: '/workspace',
    session: 's2',
    rows: [],
    ask: null,
    refs: [],
    webviewId: 'view-1',
  }, handlers);
  assert.equal(currentSession, 's2');
  assert.equal(elements.say.value, '', 'Step 6: S2 has empty input');
  assert.equal(state.getGeneralDraft('/workspace', 's1'), 'EXISTING', 'Step 6: S1 storage not clobbered when moving to S2');
  assert.equal(state.getState('/workspace', 's1').generalDraft, 'EXISTING');
  assert.equal(state.getCreationTask('/workspace', taskId)?.draft, 'NEW DRAFT', 'Step 6: task draft remains NEW DRAFT');

  // 7. S1 복귀
  dispatchHostMessage({
    kind: 'rows',
    companionKey: '/workspace',
    session: 's1',
    rows: [{ who: 'magi', label: 'magi', text: 'response 1' }],
    ask: null,
    refs: [],
    webviewId: 'view-1',
  }, handlers);
  assert.equal(currentSession, 's1');
  assert.equal(inputAdapter.getCurrentSession?.(), 's1');
  assert.equal(elements.say.value, 'EXISTING', 'Step 7: DOM input restored to EXISTING upon return to S1');
  assert.equal(state.getGeneralDraft('/workspace', 's1'), 'EXISTING', 'Step 7: S1 storage restored to EXISTING upon return');
  assert.equal(state.getState('/workspace', 's1').generalDraft, 'EXISTING');
  assert.equal(state.getCreationTask('/workspace', taskId)?.draft, 'NEW DRAFT', 'Step 7: task draft remains NEW DRAFT');

  // 8. 복귀 후 입력 수정과 실제 전송도 대상 세션과 일치하는지 확인
  elements.say.value = 'EXISTING MODIFIED';
  listeners['input']?.({});
  assert.equal(state.getGeneralDraft('/workspace', 's1'), 'EXISTING MODIFIED');
  assert.equal(state.getState('/workspace', 's1').generalDraft, 'EXISTING MODIFIED');

  inputAdapter.send();
  const sendMsg = posted.pop();
  assert.equal(sendMsg.kind, 'say');
  assert.equal(sendMsg.text, 'EXISTING MODIFIED');
  assert.equal(sendMsg.creationTaskId, undefined, 'sending in s1 must not include creationTaskId');
  assert.equal(elements.say.value, '', 'DOM input cleared after send');
  assert.equal(state.getGeneralDraft('/workspace', 's1'), '', 'S1 storage cleared after send');
  assert.equal(state.getState('/workspace', 's1').generalDraft, '');
  assert.equal(state.getCreationTask('/workspace', taskId)?.draft, 'NEW DRAFT', 'task draft preserved as NEW DRAFT');

  inputAdapter.dispose();
});

test('§4.5 Item 4: 충돌 없는 완료 및 추가 작성 없는 완료의 DOM 입력과 상태 모듈 스냅샷 일치 검증', () => {
  const posted: any[] = [];
  const bridge = { postMessage: (m: any) => posted.push(m) };
  const actions = createWebviewActionAdapter(bridge);
  const state = createAnswerState();

  const listeners: Record<string, (e: any) => void> = {};
  const elements = {
    say: {
      value: '',
      placeholder: '',
      focus() {},
      setSelectionRange() {},
      addEventListener: (type: string, fn: any) => { listeners[type] = fn; },
      removeEventListener: () => {},
    } as any,
    sendBtn: { textContent: '', addEventListener() {}, removeEventListener() {} } as any,
    replyModeEl: { hidden: true } as any,
    replyTargetEl: { textContent: '' } as any,
    replyCancelEl: { addEventListener() {}, removeEventListener() {} } as any,
    noteEl: { textContent: '' } as any,
    hintEl: { textContent: '' } as any,
  };

  const inputAdapter = createWebviewInputAdapter(elements, actions, state);
  let currentSession = '';
  let currentCompanionKey = '/workspace';
  let currentGeneration: number | undefined = 0;
  let currentWebviewId = 'view-1';

  const handlers = createWebviewReceiveHandlers({
    inputAdapter,
    answerState: state,
    getCurrentAsk: () => null,
    getCurrentSession: () => currentSession,
    setCurrentSession: (s) => { currentSession = s; },
    getCurrentCompanionKey: () => currentCompanionKey,
    setCurrentCompanionKey: (k) => { currentCompanionKey = k; },
    getCurrentGeneration: () => currentGeneration,
    setCurrentGeneration: (g) => { currentGeneration = g; },
    getCurrentWebviewId: () => currentWebviewId,
    setCurrentWebviewId: (w) => { currentWebviewId = w; },
    clearExpandedCallIds: () => {},
    drawRows: () => {},
    drawAsk: () => {},
    drawRefs: () => {},
    drawState: () => {},
    drawInfo: () => {},
    setNoteText: () => {},
    getNoteText: () => '',
  });

  // A. 충돌 없는 완료 (빈 세션에서 전송 후 새 초안 작성 -> 충돌 없이 완료 연결)
  inputAdapter.onContextChange('/workspace', '', null, 0, 'view-1');
  elements.say.value = 'M_FIRST';
  inputAdapter.send();
  const say1 = posted.pop();
  const task1 = say1.creationTaskId;

  elements.say.value = 'NEW_DRAFT_NO_CONFLICT';
  listeners['input']?.({});
  assert.equal(elements.say.value, 'NEW_DRAFT_NO_CONFLICT');

  dispatchHostMessage({
    kind: 'sessionCreated',
    companionKey: '/workspace',
    session: 'sess-no-conflict',
    creationTaskId: task1,
    webviewId: 'view-1',
  }, handlers);

  assert.equal(currentSession, 'sess-no-conflict');
  assert.equal(inputAdapter.getCurrentSession?.(), 'sess-no-conflict');
  assert.equal(elements.say.value, 'NEW_DRAFT_NO_CONFLICT', 'DOM input must show draft when no conflict');
  assert.equal(state.getGeneralDraft('/workspace', 'sess-no-conflict'), 'NEW_DRAFT_NO_CONFLICT');
  assert.equal(state.getState('/workspace', 'sess-no-conflict').generalDraft, 'NEW_DRAFT_NO_CONFLICT');
  assert.equal(state.getCreationTask('/workspace', task1)?.draft, 'NEW_DRAFT_NO_CONFLICT');
  assert.equal(state.getCreationTask('/workspace', task1)?.status, 'completed');

  // B. 추가 작성 없는 완료 (빈 세션에서 전송 후 추가 입력 없이 즉시 완료 연결)
  inputAdapter.onContextChange('/workspace', '', null, 0, 'view-1');
  currentSession = '';
  elements.say.value = 'M_SECOND';
  inputAdapter.send();
  const say2 = posted.pop();
  const task2 = say2.creationTaskId;

  assert.equal(elements.say.value, '', 'input cleared on send');

  dispatchHostMessage({
    kind: 'sessionCreated',
    companionKey: '/workspace',
    session: 'sess-fresh-empty',
    creationTaskId: task2,
    webviewId: 'view-1',
  }, handlers);

  assert.equal(currentSession, 'sess-fresh-empty');
  assert.equal(inputAdapter.getCurrentSession?.(), 'sess-fresh-empty');
  assert.equal(elements.say.value, '', 'DOM input remains empty when no draft was typed');
  assert.equal(state.getGeneralDraft('/workspace', 'sess-fresh-empty'), '');
  assert.equal(state.getState('/workspace', 'sess-fresh-empty').generalDraft, '');
  assert.equal(state.getCreationTask('/workspace', task2)?.draft, '');
  assert.equal(state.getCreationTask('/workspace', task2)?.status, 'completed');

  inputAdapter.dispose();
});



