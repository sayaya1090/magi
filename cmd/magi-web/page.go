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
<meta name="theme-color" content="#211B14">
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

  * { box-sizing:border-box; }
  html { scrollbar-gutter:stable; -webkit-text-size-adjust:100%; }
  body {
    margin:0; background:var(--bg); color:var(--fg);
    font:14px/1.6 ui-monospace,SFMono-Regular,"SF Mono",Menlo,Consolas,monospace;
    -webkit-font-smoothing:antialiased;
  }
  [hidden] { display:none !important; }

  /* ── header ─────────────────────────────────────────────────────────────── */
  header {
    position:sticky; top:0; z-index:2; background:var(--surface);
    border-bottom:1px solid var(--outlineVariant);
    padding:.55rem .9rem; padding-top:calc(.55rem + env(safe-area-inset-top));
    display:flex; gap:.7rem; align-items:baseline; flex-wrap:wrap;
  }
  .mark { color:var(--primary); font-weight:600; letter-spacing:.06em; }
  /* The three councillors, in their own hues — the signature the terminal wears. */
  .magi span { font-size:11px; letter-spacing:.14em; }
  .magi .m { color:var(--melchior); } .magi .b { color:var(--balthasar); } .magi .c { color:var(--casper); }
  .sid { color:var(--muted); font-size:11.5px; opacity:.75; overflow-wrap:anywhere; }
  #state { margin-left:auto; font-size:11.5px; color:var(--muted); display:flex; align-items:center; gap:.4rem; }
  #state::before { content:""; width:7px; height:7px; border-radius:50%; background:var(--outline); }
  #state.live::before { background:var(--success); box-shadow:0 0 0 3px color-mix(in srgb, var(--success) 22%, transparent); }
  #state.lost::before { background:var(--error); }
  #back {
    color:var(--muted); text-decoration:none; border:1px solid var(--outlineVariant);
    border-radius:6px; padding:.15rem .5rem; font-size:12px; line-height:1.7;
  }
  #back:hover { color:var(--primary); border-color:var(--primary); }

  main { padding:1rem .9rem 8rem; max-width:108ch; margin:0 auto; }

  /* ── dashboard ──────────────────────────────────────────────────────────── */
  /* One column, widening only when there is room. auto-fill rather than auto-fit so a lone
     daemon stays a card and does not stretch to the width of the window. */
  #fleet { display:grid; gap:.7rem; grid-template-columns:1fr; }
  @media (min-width:700px) { #fleet { grid-template-columns:repeat(auto-fill, minmax(310px, 1fr)); } }

  .card {
    display:block; text-decoration:none; color:inherit; background:var(--surface);
    border:1px solid var(--outlineVariant); border-left:3px solid var(--outline);
    border-radius:8px; padding:.7rem .85rem; min-height:44px;
  }
  .card:hover { border-color:var(--primary); }
  .card:active { background:var(--primaryContainer); }
  .card.here { border-left-color:var(--primary); }
  .card.working { border-left-color:var(--success); }
  .card.abandoned { border-left-color:var(--error); }
  .card.stopped { opacity:.62; }

  .card .top { display:flex; align-items:baseline; gap:.5rem; }
  .card .name { color:var(--primary); font-weight:600; overflow-wrap:anywhere; }
  .card .badge {
    margin-left:auto; font-size:10.5px; letter-spacing:.1em; text-transform:uppercase;
    color:var(--muted); white-space:nowrap;
  }
  .card.working .badge { color:var(--success); }
  .card.abandoned .badge { color:var(--error); }
  .card.idle .badge { color:var(--accent); }
  .card .path { color:var(--muted); font-size:11.5px; opacity:.8; overflow-wrap:anywhere; margin-top:.15rem; }
  .card .last {
    margin-top:.45rem; color:var(--fg); opacity:.85; font-size:12.5px;
    display:-webkit-box; -webkit-line-clamp:2; -webkit-box-orient:vertical; overflow:hidden;
  }
  .card .meta { margin-top:.45rem; color:var(--muted); font-size:11px; opacity:.75; }
  .empty { color:var(--muted); padding:2rem 0; }
  .empty code { color:var(--accent); }

  /* ── transcript ─────────────────────────────────────────────────────────── */
  .row { display:grid; grid-template-columns:5.5rem 1fr; gap:.85rem; align-items:start; padding:.2rem 0; }
  .who { color:var(--muted); text-align:right; user-select:none; font-size:12px; padding-top:.1rem; opacity:.8; }
  .txt { white-space:pre-wrap; overflow-wrap:anywhere; }

  /* A user turn is the anchor you scan for, so it gets the primary bar. */
  .row.user { margin:.9rem 0 .5rem; }
  .row.user .txt { color:var(--primary); border-left:2px solid var(--primary); padding-left:.7rem; margin-left:-.7rem; }
  .row.assistant .txt { color:var(--fg); }
  /* Reasoning and tool calls are context, not the point: present, quieter. */
  .row.thinking .txt { color:var(--muted); font-style:italic; opacity:.72; }
  .row.tool .txt { color:var(--accent); }
  .row.result .txt, .row.failed .txt {
    color:var(--muted); background:color-mix(in srgb, var(--surface) 70%, transparent);
    border-left:2px solid var(--outlineVariant); padding:.25rem .6rem; border-radius:3px;
    max-height:11rem; overflow:auto;
  }
  .row.failed .who, .row.failed .txt { color:var(--error); border-left-color:var(--error); }
  .row.tool .who { color:var(--accent); opacity:.65; }

  /* ── composer ───────────────────────────────────────────────────────────── */
  form {
    position:fixed; left:0; right:0; bottom:0; z-index:2;
    background:linear-gradient(to top, var(--bg) 72%, transparent);
    padding:.9rem; padding-bottom:calc(.9rem + env(safe-area-inset-bottom));
    display:flex; gap:.5rem; justify-content:center;
  }
  .composer { display:flex; gap:.5rem; width:100%; max-width:108ch; align-items:flex-end; }
  textarea {
    flex:1; background:var(--surface); color:var(--fg);
    border:1px solid var(--outline); border-radius:8px; padding:.6rem .75rem;
    font:inherit; font-size:16px; resize:none; min-height:2.9rem; max-height:12rem; overflow-y:auto;
  }
  textarea:focus { outline:none; border-color:var(--primary); }
  textarea::placeholder { color:var(--muted); opacity:.55; }
  button {
    background:transparent; color:var(--fg); border:1px solid var(--outline);
    border-radius:8px; padding:0 .9rem; min-height:2.9rem; font:inherit; cursor:pointer; white-space:nowrap;
  }
  button:hover { border-color:var(--primary); color:var(--primary); }
  #stop:hover { border-color:var(--error); color:var(--error); }
  button:active { background:var(--primaryContainer); }

  @media (max-width:640px) {
    .row { grid-template-columns:1fr; gap:.15rem; }
    .who { text-align:left; }
    main { padding-bottom:9rem; }
    /* On a phone the two buttons take the width the placeholder cannot have anyway. */
    #stop { padding:0 .7rem; }
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

  if (a.last) { const l = document.createElement('div'); l.className = 'last'; l.textContent = a.last; el.append(l); }

  const bits = [];
  if (a.steps) bits.push(a.steps + ' step' + (a.steps === 1 ? '' : 's'));
  if (a.idle >= 0) bits.push(ago(a.idle));
  if (a.live) bits.push('pid ' + a.pid);
  if (a.here) bits.push('this directory');
  const m = document.createElement('div'); m.className = 'meta'; m.textContent = bits.join(' · ');
  el.append(m);
  return el;
}

async function loadFleet() {
  let list;
  try { list = await (await fetch('/fleet')).json(); }
  catch { state.className = 'lost'; state.textContent = 'cannot reach magi-web'; return; }
  state.className = '';
  state.textContent = list.length + (list.length === 1 ? ' agent' : ' agents');
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
  if (s) { draw([]); connect(); }
  else { state.className = ''; state.textContent = ''; loadFleet(); fleetTimer = setInterval(loadFleet, 3000); }
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
