package main

// indexHTML is the whole front end: one file, no build step, nothing fetched at load.
//
// A framework would mean a bundler, a lockfile and a second toolchain for a transcript and a text
// box — and magi is one static binary precisely so there is nothing to install. The page is also
// readable end to end, which matters for something that puts a working directory's contents on a
// port.
//
// # Two views, one document
//
// `/` is the fleet: every daemon this config directory knows about. `/?d=<socket>` is one of them,
// with its transcript and a composer. Same page, switched by the query string and pushState, so
// entering an agent and coming back is instant and there is only ever one idea of what magi looks
// like. The server tells them apart the same way — see server.target.
//
// # The colours are magi's, not a second scheme
//
// Every value below is lifted from styles.go: the same Material Design 3 colour ROLES themed after
// NERV/MAGI — amber primary, cyan accent, warm dark surface — and the same three councillor hues.
// A browser view that invented its own palette would be a second thing to keep in step, and the
// first time somebody retuned the terminal the two would disagree about what magi looks like.
//
// Kept as CSS custom properties with the role names the Go side uses (primary, accent, muted,
// outline, surface, primaryContainer, outlineVariant, error, success, warn), so the correspondence
// is checkable by reading rather than by remembering.
//
// # Phones are not a smaller desktop
//
// The page is installable — a manifest and an icon, so adding it to a home screen opens it without
// browser chrome. That is the whole of magi's answer to the native mobile app other orchestrators
// ship: the thing you actually want on a phone is to see what your agents are doing and to say one
// sentence to one of them, and a page that launches full screen does that without a second client
// to keep in step with the daemon protocol.
//
// The layout starts at one column and grows, rather than starting wide and collapsing. Three things
// on a phone are not preferences: inputs at 16px (anything smaller makes iOS Safari zoom the page
// on focus, and it does not zoom back), the composer padded by env(safe-area-inset-bottom) (a home
// indicator otherwise sits on top of the send button), and controls at least 44px tall. The
// transcript's label column folds above its text below 640px, because five and a half characters of
// gutter is most of a narrow screen.
//
// Nothing is interpolated into this string. It used to substitute a session id, and the format verb
// that did it read `width:100%` as a verb and broke the build; there is now nothing to substitute
// because the page learns what it is looking at from /fleet.
const indexHTML = `<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="dark light">
<meta name="theme-color" content="#14110d">
<link rel="manifest" href="/manifest.webmanifest">
<link rel="icon" href="/icon.svg">
<link rel="apple-touch-icon" href="/icon.svg">
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-title" content="magi">
<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">
<title>magi</title>
<style>
  /* Roles, verbatim from internal/adapter/tui/styles.go — nervDark / nervLight. */
  :root {
    color-scheme: dark light;
    --primary:#FF7A1A; --accent:#5CD8E6; --muted:#C9C2B8; --outline:#5A5048;
    --error:#F2B8B5; --success:#86EFAC; --surface:#211B14;
    --primaryContainer:#4A2E0B; --outlineVariant:#463E34; --warn:#FFD479;
    --melchior:#FFB454; --balthasar:#5CD8E6; --casper:#FF8A8A;
    --bg:#14110d; --fg:#E8E2D8;
  }
  @media (prefers-color-scheme: light) {
    :root {
      --primary:#B45309; --accent:#0E7490; --muted:#4A453C; --outline:#8A7E6E;
      --error:#B3261E; --success:#15803D; --surface:#F5EEE3;
      --primaryContainer:#F8D9A8; --outlineVariant:#D8CFC0; --warn:#92600A;
      --melchior:#B45309; --balthasar:#0E7490; --casper:#B3261E;
      --bg:#FBF8F3; --fg:#221D16;
    }
  }

  /* Newsreader, an editorial serif drawn for reading on screens, served from this binary — see
     fonts/README.md. A font CDN would make the page's appearance depend on somebody else's machine
     and tell it when you look at your agents; embedding costs 60KB and nothing leaves the host.
     swap, so the page is readable in the fallback before the face arrives. */
  @font-face {
    font-family:"Newsreader"; font-style:normal; font-weight:400; font-display:swap;
    src:url(/font/newsreader-400.woff2) format("woff2");
  }
  @font-face {
    font-family:"Newsreader"; font-style:normal; font-weight:600; font-display:swap;
    src:url(/font/newsreader-600.woff2) format("woff2");
  }
  @font-face {
    font-family:"Newsreader"; font-style:italic; font-weight:400; font-display:swap;
    src:url(/font/newsreader-italic.woff2) format("woff2");
  }

  /* Two families. The serif for what a person READS — names, the lead line, the empty state — and
     monospace for everything that is a fact from the machine: paths, commands, transcript. The
     system stack behind Newsreader is not decoration: it carries every script the subset does not,
     so a Korean workspace name renders in the platform's serif rather than in tofu. */
  :root {
    --display: "Newsreader", "Iowan Old Style", "Palatino Linotype", Palatino, Georgia, serif;
    --mono: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;
    --measure: 74ch;   /* prose */
    --wide: 108ch;     /* transcript, where lines are code and wrapping costs more than width */
  }

  * { box-sizing:border-box; }

  /* Keyboard focus, said once and loudly.
     A dashboard is a page of links and buttons, and the fleet is navigated with tab as readily as
     with a mouse — the underline this layout uses for a pressed state is not a focus ring, and a
     border colour that shifts by one step is not one either. :focus-visible so a mouse click does
     not leave a ring behind it, and an offset so the ring is not mistaken for the element's own
     rule. The outline:none below applies to :focus, which this then overrides for the keyboard. */
  :focus-visible {
    outline:2px solid var(--primary); outline-offset:3px; border-radius:2px;
  }
  html { scrollbar-gutter:stable; -webkit-text-size-adjust:100%; }
  body {
    margin:0; background:var(--bg); color:var(--fg);
    font:14px/1.65 var(--mono);
    -webkit-font-smoothing:antialiased; text-rendering:optimizeLegibility;
    font-variant-numeric:tabular-nums;  /* ages and step counts line up down the column */
  }
  [hidden] { display:none !important; }

  /* A kicker: the small letterspaced label an editorial layout puts above a headline. Here it is
     the state, which is the first thing you want and the last thing that deserves a box. */
  /* On opacity: every value below is set so the RESULT clears WCAG AA (4.5:1) against the page in
     BOTH themes, which is checked in page_test.go. Editorial layouts get their hierarchy from
     dimming secondary text, and the arithmetic is easy to get wrong twice over — the muted role is
     already lowered, and light mode has less headroom than dark. Measured before this note: eight
     of thirteen dimmed pairs were under, the worst at 2.47:1. */
  .kicker {
    font:600 10.5px/1.4 var(--mono); letter-spacing:.18em; text-transform:uppercase;
    color:var(--muted);
  }

  /* ── masthead ───────────────────────────────────────────────────────────── */
  header {
    position:sticky; top:0; z-index:2; background:var(--bg);
    border-bottom:1px solid var(--fg);
    box-shadow:0 3px 0 -2px var(--outlineVariant);   /* the hairline under the rule */
    padding:.7rem 1.4rem .5rem;
    padding-top:calc(.7rem + env(safe-area-inset-top));
    display:flex; gap:1rem; align-items:baseline; flex-wrap:wrap;
    max-width:var(--wide); margin:0 auto;
  }
  .mark {
    font:600 22px/1 var(--display); letter-spacing:.01em; color:var(--primary);
    font-feature-settings:"liga" 1;
  }
  /* The three councillors, in their own hues — the signature the terminal wears, set as a
     nameplate's standing line. */
  .magi { display:flex; gap:.6rem; }
  .magi span { font-size:9.5px; letter-spacing:.22em; font-weight:600; }
  .magi .m { color:var(--melchior); } .magi .b { color:var(--balthasar); } .magi .c { color:var(--casper); }
  .sid { color:var(--muted); font-size:11px; letter-spacing:.04em; opacity:.8; overflow-wrap:anywhere; }
  #state {
    margin-left:auto; font:600 10.5px/1.4 var(--mono); letter-spacing:.16em; text-transform:uppercase;
    color:var(--muted); display:flex; align-items:center; gap:.45rem;
  }
  #state::before { content:""; width:6px; height:6px; border-radius:50%; background:var(--outline); }
  #state.live::before { background:var(--success); box-shadow:0 0 0 3px color-mix(in srgb, var(--success) 20%, transparent); }
  #state.lost::before { background:var(--error); }
  #back {
    color:var(--muted); text-decoration:none; font-size:11px; letter-spacing:.12em;
    text-transform:uppercase; border-bottom:1px solid var(--outlineVariant); padding-bottom:2px;
  }
  #back:hover { color:var(--primary); border-bottom-color:var(--primary); }

  main { padding:1.6rem 1.4rem 9rem; max-width:var(--wide); margin:0 auto; }

  /* ── the fleet, as a column of entries ──────────────────────────────────── */
  /* Rules, not boxes. A card with a border around it is a widget; a rule between entries is a
     page, and twenty agents read as a list of stories rather than a wall of chrome. */
  #fleet { display:block; max-width:var(--measure); }

  .card {
    display:block; text-decoration:none; color:inherit;
    border-top:1px solid var(--outlineVariant);
    padding:1.15rem 0 1.25rem 1rem;
    margin-left:-1rem; border-left:2px solid transparent;
    transition:border-left-color .12s ease, background .12s ease;
  }
  #fleet .card:first-of-type { border-top:0; }
  /* M3 keeps its state layers; they are just quieter here than a filled card would be. */
  .card:hover { background:color-mix(in srgb, var(--primary) 5%, transparent); border-left-color:var(--outline); }
  .card:active { background:color-mix(in srgb, var(--primary) 10%, transparent); }
  .card.here { border-left-color:var(--primary); }
  .card.working { border-left-color:var(--success); }
  .card.waiting { border-left-color:var(--warn); }
  .card.abandoned { border-left-color:var(--error); }
  .card.stopped { opacity:.8; }

  /* wrap, not wrap-reverse: the badge is order:-1 with a full-width basis, so it takes the first
     line on its own and the name follows underneath. wrap-reverse would invert the cross axis and
     put that first line at the BOTTOM — the kicker below the headline, which is the one arrangement
     this layout is not. */
  .card .top { display:flex; align-items:baseline; gap:.7rem; flex-wrap:wrap; }
  .card .name {
    font:600 20px/1.25 var(--display); letter-spacing:.005em; color:var(--fg);
  }
  .card:hover .name { color:var(--primary); }
  /* The state is the kicker, and it sits ABOVE the name in the reading order a person uses:
     what is happening, then which agent. */
  .card .badge {
    order:-1; flex-basis:100%;
    font:600 10.5px/1.4 var(--mono); letter-spacing:.18em; text-transform:uppercase;
    color:var(--muted); margin-bottom:.15rem;
  }
  .card.working .badge { color:var(--success); }
  .card.abandoned .badge { color:var(--error); }
  .card.idle .badge { color:var(--accent); }
  .card.waiting .badge { color:var(--warn); }
  .card .path {
    font-size:11.5px; color:var(--muted); opacity:.9; overflow-wrap:anywhere; margin-top:.3rem;
  }
  /* The lead: what it is doing, set as a sentence rather than a log line. */
  .card .last {
    font:italic 15.5px/1.55 var(--display); color:var(--fg); margin-top:.55rem;
    display:-webkit-box; -webkit-line-clamp:2; -webkit-box-orient:vertical; overflow:hidden;
  }
  .card .asking {
    font:600 14px/1.5 var(--mono); color:var(--warn); margin-top:.55rem; overflow-wrap:anywhere;
  }
  .card .meta {
    margin-top:.55rem; font-size:11px; letter-spacing:.06em; color:var(--muted); opacity:.8;
  }

  /* Answering, as text buttons — the editorial equivalent of a form: words with rules under them.
     Still 44px of touch target, which is a phone's business and not a style's. */
  .answer { display:flex; gap:1.1rem; margin-top:.75rem; flex-wrap:wrap; align-items:center; }
  .answer button {
    background:none; border:0; border-bottom:1px solid var(--warn); border-radius:0;
    color:var(--warn); font:600 11.5px/1 var(--mono); letter-spacing:.16em; text-transform:uppercase;
    padding:.35rem .1rem; min-height:44px; cursor:pointer;
  }
  .answer button:hover { color:var(--primary); border-bottom-color:var(--primary); }
  .answer input {
    flex:1; min-width:9rem; background:transparent; color:var(--fg); font:16px/1.5 var(--mono);
    border:0; border-bottom:1px solid var(--outline); border-radius:0; padding:.45rem .1rem;
  }
  .answer input:focus { outline:none; border-bottom-color:var(--primary); }
  .answer input:focus-visible { outline:2px solid var(--primary); outline-offset:3px; }

  .empty {
    font:17px/1.7 var(--display); color:var(--muted); padding:2.5rem 0; max-width:52ch;
  }
  .empty code { font:14px/1 var(--mono); color:var(--accent); }

  /* ── transcript ─────────────────────────────────────────────────────────── */
  /* Monospace throughout: every line here is something the machine said or did, and a serif would
     be dressing up evidence. The editorial part is the rhythm — a wide gutter of small-caps labels
     against a single column of text. */
  #log { max-width:var(--wide); }
  .row { display:grid; grid-template-columns:6.5rem 1fr; gap:1.1rem; align-items:start; padding:.22rem 0; }
  .who {
    font:600 10px/1.9 var(--mono); letter-spacing:.16em; text-transform:uppercase;
    color:var(--muted); text-align:right; user-select:none; opacity:.8;
  }
  .txt { white-space:pre-wrap; overflow-wrap:anywhere; }

  /* A user turn is the anchor you scan for: set as a lead, with the rule an editorial layout uses
     for a pull quote. */
  .row.user { margin:1.6rem 0 .7rem; }
  .row.user .txt {
    font:17px/1.55 var(--display); color:var(--primary);
    border-left:2px solid var(--primary); padding-left:.9rem; margin-left:-.9rem;
  }
  .row.user .who { color:var(--primary); }
  .row.assistant .txt { color:var(--fg); }
  .row.thinking .txt { color:var(--muted); font-style:italic; opacity:.8; }
  .row.tool .txt { color:var(--accent); }
  .row.tool .who { color:var(--accent); }
  .row.result .txt, .row.failed .txt {
    color:var(--muted); border-left:1px solid var(--outlineVariant);
    padding:.15rem 0 .15rem .8rem; max-height:11rem; overflow:auto;
  }
  .row.failed .who, .row.failed .txt { color:var(--error); border-left-color:var(--error); }

  /* ── the prompt an agent is blocked on, on that agent's own page ─────────── */
  /* Without this, opening an agent is the one place you CANNOT see that it is waiting for you: the
     prompt is not in the log — it is a question about what should happen, not a record of what did
     — so the transcript shows a run that has simply stopped. It sat above the composer because
     that is where the answer goes. */
  #prompt {
    position:fixed; left:0; right:0; bottom:0; z-index:3;
    background:var(--bg); border-top:2px solid var(--warn);
    padding:.9rem 1.4rem; padding-bottom:calc(.9rem + env(safe-area-inset-bottom));
  }
  #prompt .inner { max-width:var(--wide); margin:0 auto; }
  #prompt .asking { font:600 14px/1.5 var(--mono); color:var(--warn); overflow-wrap:anywhere; }

  /* ── composer ───────────────────────────────────────────────────────────── */
  form {
    position:fixed; left:0; right:0; bottom:0; z-index:2;
    background:linear-gradient(to top, var(--bg) 74%, transparent);
    padding:1rem 1.4rem; padding-bottom:calc(1rem + env(safe-area-inset-bottom));
    display:flex; justify-content:center;
  }
  .composer {
    display:flex; gap:.9rem; width:100%; max-width:var(--wide); align-items:flex-end;
    border-top:1px solid var(--fg); padding-top:.8rem;
  }
  textarea {
    flex:1; background:transparent; color:var(--fg);
    border:0; border-bottom:1px solid var(--outline); border-radius:0;
    padding:.5rem .1rem; font:16px/1.6 var(--mono); resize:none;
    min-height:2.6rem; max-height:12rem; overflow-y:auto;
  }
  textarea:focus { outline:none; border-bottom-color:var(--primary); }
  textarea:focus-visible { outline:2px solid var(--primary); outline-offset:3px; }
  textarea::placeholder { color:var(--muted); opacity:.8; }
  button {
    background:none; border:0; border-bottom:1px solid var(--outline); border-radius:0;
    color:var(--muted); font:600 11.5px/1 var(--mono); letter-spacing:.16em; text-transform:uppercase;
    padding:0 .1rem; min-height:44px; cursor:pointer; white-space:nowrap;
  }
  button:hover { color:var(--primary); border-bottom-color:var(--primary); }
  #stop:hover { color:var(--error); border-bottom-color:var(--error); }

  @media (max-width:640px) {
    header { padding-left:1rem; padding-right:1rem; }
    main { padding:1.2rem 1rem 9.5rem; }
    .card .name { font-size:18px; }
    .row { grid-template-columns:1fr; gap:.2rem; }
    .who { text-align:left; }
    .row.user .txt { font-size:16px; }
    form { padding-left:1rem; padding-right:1rem; }
  }
</style>

<header>
  <span class="mark">magi</span>
  <span class="magi"><span class="m">MELCHIOR</span> <span class="b">BALTHASAR</span> <span class="c">CASPER</span></span>
  <a id="back" href="/" hidden>← fleet</a>
  <span class="sid" id="sid"></span>
  <span id="state"></span>
</header>

<main>
  <div id="fleet"></div>
  <div id="log"></div>
</main>

<div id="prompt" hidden></div>

<form id="f" hidden><div class="composer">
  <textarea id="t" rows="1" placeholder="Ask magi to do something…"></textarea>
  <button type="submit">send</button>
  <button type="button" id="stop">interrupt</button>
</div></form>

<script>
const fleetEl = document.getElementById('fleet'), log = document.getElementById('log');
const state = document.getElementById('state'), sidEl = document.getElementById('sid');
const back = document.getElementById('back'), f = document.getElementById('f');

const sock = () => new URLSearchParams(location.search).get('d');
const q = () => sock() ? '?d=' + encodeURIComponent(sock()) : '';

// ── the fleet ────────────────────────────────────────────────────────────────
const ago = s => s < 0 ? '' : s < 60 ? s + 's ago' : s < 3600 ? Math.round(s/60) + 'm ago'
                : s < 86400 ? Math.round(s/3600) + 'h ago' : Math.round(s/86400) + 'd ago';

function card(a) {
  const el = document.createElement('a');
  el.className = 'card ' + a.state + (a.here ? ' here' : '');
  el.href = '/?d=' + encodeURIComponent(a.socket);
  el.onclick = e => { e.preventDefault(); go(a.socket); };

  const top = document.createElement('div'); top.className = 'top';
  const n = document.createElement('span'); n.className = 'name'; n.textContent = a.name;
  const b = document.createElement('span'); b.className = 'badge'; b.textContent = a.state;
  top.append(n, b);

  const p = document.createElement('div'); p.className = 'path'; p.textContent = a.workdir;
  el.append(top, p);

  // What it is blocked on comes before what it was doing, and in the warning colour: it is the
  // only line on this page that is a request rather than a report.
  if (a.asking) {
    const k = document.createElement('div'); k.className = 'asking'; k.textContent = '⏸ ' + a.asking;
    el.append(k, answerBox(a));
  }
  if (a.task) { const l = document.createElement('div'); l.className = 'last'; l.textContent = a.task; el.append(l); }

  const bits = [];
  if (a.steps) bits.push(a.steps + ' step' + (a.steps === 1 ? '' : 's'));
  if (a.idle >= 0) bits.push(ago(a.idle));
  if (a.live) bits.push('pid ' + a.pid);
  if (a.here) bits.push('this directory');
  const m = document.createElement('div'); m.className = 'meta'; m.textContent = bits.join(' · ');
  el.append(m);
  return el;
}

// answerBox is the reply to a blocked agent, on the card itself. Answering is why you looked.
//
// The buttons stop the click from opening the agent (the card is a link) — reading and answering
// are different intentions and the same tap must not do both.
function answerBox(a) {
  const box = document.createElement('div'); box.className = 'answer';
  const send = (text) => post('/answer?d=' + encodeURIComponent(a.socket),
                              new URLSearchParams({call: a.askId, kind: a.askKind, text})).then(loadFleet);
  if (a.askKind === 'question') {
    const i = document.createElement('input'); i.placeholder = 'your answer…';
    const b = document.createElement('button'); b.textContent = 'answer';
    const go = e => { e.preventDefault(); e.stopPropagation(); if (i.value.trim()) send(i.value.trim()); };
    b.onclick = go;
    i.onclick = e => { e.preventDefault(); e.stopPropagation(); };
    i.onkeydown = e => { if (e.key === 'Enter') go(e); };
    box.append(i, b);
  } else {
    for (const [label, decision] of [['allow', 'allow'], ['always', 'always'], ['deny', 'deny']]) {
      const b = document.createElement('button'); b.textContent = label;
      b.onclick = e => { e.preventDefault(); e.stopPropagation(); send(decision); };
      box.append(b);
    }
  }
  return box;
}

// The tab's own title carries the count. A dashboard is a page you leave open in a background tab
// or behind an app switcher, and an agent that starts waiting there is invisible until you look —
// the title is the one channel that reaches a page nobody is watching, without asking for
// notification permission or shipping a service worker to deliver them.
function retitle(waiting) {
  document.title = waiting ? '(' + waiting + ') magi' : 'magi';
}

// drawPrompt puts what an agent is blocked on above its own composer.
//
// An agent's page was the one place this could not be seen: the prompt is not in the log — it is a
// question about what should happen, not a record of what did — so the transcript showed a run
// that had simply stopped, and the only way to find out was to go back to the fleet. Which is the
// opposite of where you would be, having opened this agent to watch it.
function drawPrompt(a) {
  const box = document.getElementById('prompt');
  if (!a || a.state !== 'waiting') { box.hidden = true; box.replaceChildren(); return; }
  const inner = document.createElement('div'); inner.className = 'inner';
  const k = document.createElement('div'); k.className = 'asking'; k.textContent = '⏸ ' + a.asking;
  inner.append(k, answerBox(a));
  box.replaceChildren(inner);
  box.hidden = false;
}

async function loadFleet() {
  let list;
  try { list = await (await fetch('/fleet')).json(); }
  catch { state.className = 'lost'; state.textContent = 'cannot reach magi-web'; return; }
  state.className = '';
  const waiting = list.filter(a => a.state === 'waiting').length;
  retitle(waiting);
  // On an agent's page the fleet is polled for this one entry, and nothing else on screen changes.
  const here = sock();
  if (here) { drawPrompt(list.find(a => a.socket === here)); return; }
  state.textContent = list.length + (list.length === 1 ? ' agent' : ' agents') +
                      (waiting ? ' · ' + waiting + ' waiting on you' : '');
  state.className = waiting ? 'lost' : '';
  if (!list.length) {
    fleetEl.replaceChildren();
    const e = document.createElement('div'); e.className = 'empty';
    e.innerHTML = 'No magi daemons under this config directory.<br>' +
                  'Start one with <code>magi --daemon</code> in a workspace.';
    fleetEl.append(e);
    return;
  }
  fleetEl.replaceChildren(...list.map(card));
}

// ── one agent ────────────────────────────────────────────────────────────────
// Follow the tail only while the reader is already at the bottom. Yanking the view down while
// somebody reads the middle of a long run is how a live page becomes unreadable.
const atBottom = () => window.innerHeight + window.scrollY >= document.body.offsetHeight - 48;

function draw(rows) {
  const stick = atBottom();
  log.replaceChildren(...(rows || []).map(r => {
    const d = document.createElement('div'); d.className = 'row ' + r.who;
    const w = document.createElement('div'); w.className = 'who'; w.textContent = r.who;
    const t = document.createElement('div'); t.className = 'txt'; t.textContent = r.text;
    d.append(w, t); return d;
  }));
  if (stick) window.scrollTo(0, document.body.scrollHeight);
}

let es, fleetTimer;
function connect() {
  es = new EventSource('/events' + q());
  es.onopen = () => { state.className = 'live'; state.textContent = 'live'; };
  es.onmessage = e => draw(JSON.parse(e.data));
  // The daemon outliving this page is normal, and so is the reverse. Reconnect quietly rather
  // than making a restart look like a failure.
  es.onerror = () => { state.className = 'lost'; state.textContent = 'reconnecting…';
                       es.close(); if (sock()) setTimeout(connect, 1500); };
}

// ── routing ──────────────────────────────────────────────────────────────────
// Same document either way: entering an agent is a pushState, and the browser's back button lands
// on the fleet without a reload. Every view switch tears the other one's polling down — two of
// them running at once is how a page keeps costing after you leave it.
function render() {
  if (es) { es.close(); es = null; }
  if (fleetTimer) { clearInterval(fleetTimer); fleetTimer = null; }
  const s = sock();
  fleetEl.hidden = !!s; log.hidden = !s; f.hidden = !s; back.hidden = !s;
  sidEl.textContent = s ? s.replace(/^.*\//, '') : '';
  document.getElementById('prompt').hidden = true;
  if (s) { draw([]); connect(); }
  else { state.className = ''; state.textContent = ''; }
  // Both views poll the fleet: the dashboard for its cards, an agent's page for the one thing about
  // itself that is not in its log.
  loadFleet();
  fleetTimer = setInterval(loadFleet, 3000);
}
function go(s) { history.pushState({}, '', s ? '/?d=' + encodeURIComponent(s) : '/'); render(); }
back.onclick = e => { e.preventDefault(); go(null); };
addEventListener('popstate', render);

async function post(path, body) {
  const r = await fetch(path + q(), {method:'POST', body});
  if (!r.ok) { state.className = 'lost'; state.textContent = (await r.text()).trim().slice(0, 80); }
}

const t = document.getElementById('t');
const grow = () => { t.style.height = 'auto'; t.style.height = Math.min(t.scrollHeight, 192) + 'px'; };
t.addEventListener('input', grow);
f.onsubmit = e => {
  e.preventDefault();
  const v = t.value.trim(); if (!v) return;
  t.value = ''; grow(); post('/submit', new URLSearchParams({text: v}));
};
// Enter sends on a keyboard and inserts a newline on a phone: a soft keyboard's return key is the
// only way to break a line there, and hijacking it leaves no way to write a second paragraph.
const touch = matchMedia('(hover: none)').matches;
t.onkeydown = e => { if (e.key === 'Enter' && !e.shiftKey && !touch) { e.preventDefault(); f.requestSubmit(); } };
document.getElementById('stop').onclick = () => post('/interrupt', null);

render();
</script>
`
