package main

// indexHTML is the whole front end: one file, no build step, nothing fetched at load.
//
// A framework would mean a bundler, a lockfile and a second toolchain for a transcript and a text
// box — and magi is one static binary precisely so there is nothing to install. The page is also
// readable end to end, which matters for something that puts a working directory's contents on a
// port.
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
// The session id is substituted for {{SID}}. Not a format verb: the CSS is full of percent signs
// (width:100%, color-mix percentages) and Fprintf reads those as verbs — it failed to build the
// first time for exactly that reason.
const indexHTML = `<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
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
  html { scrollbar-gutter:stable; }
  body {
    margin:0; background:var(--bg); color:var(--fg);
    font:14px/1.6 ui-monospace,SFMono-Regular,"SF Mono",Menlo,Consolas,monospace;
    -webkit-font-smoothing:antialiased;
  }

  /* ── header ─────────────────────────────────────────────────────────────── */
  header {
    position:sticky; top:0; z-index:2; background:var(--surface);
    border-bottom:1px solid var(--outlineVariant);
    padding:.55rem 1.1rem; display:flex; gap:.9rem; align-items:baseline;
  }
  .mark { color:var(--primary); font-weight:600; letter-spacing:.06em; }
  /* The three councillors, in their own hues — the signature the terminal wears. */
  .magi span { font-size:11px; letter-spacing:.14em; }
  .magi .m { color:var(--melchior); } .magi .b { color:var(--balthasar); } .magi .c { color:var(--casper); }
  .sid { color:var(--muted); font-size:11.5px; opacity:.75; }
  #state { margin-left:auto; font-size:11.5px; color:var(--muted); display:flex; align-items:center; gap:.4rem; }
  #state::before {
    content:""; width:7px; height:7px; border-radius:50%; background:var(--outline);
  }
  #state.live::before { background:var(--success); box-shadow:0 0 0 3px color-mix(in srgb, var(--success) 22%, transparent); }
  #state.lost::before { background:var(--error); }

  /* ── transcript ─────────────────────────────────────────────────────────── */
  main { padding:1.1rem 1.1rem 8rem; max-width:108ch; margin:0 auto; }
  .row { display:grid; grid-template-columns:5.5rem 1fr; gap:.85rem; align-items:start;
         padding:.2rem 0; }
  .who { color:var(--muted); text-align:right; user-select:none; font-size:12px;
         padding-top:.1rem; opacity:.8; }
  .txt { white-space:pre-wrap; overflow-wrap:anywhere; }

  /* A user turn is the anchor you scan for, so it gets the primary bar. */
  .row.user { margin:.9rem 0 .5rem; }
  .row.user .txt {
    color:var(--primary); border-left:2px solid var(--primary);
    padding-left:.7rem; margin-left:-.7rem;
  }
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
    padding:1.1rem; display:flex; gap:.6rem; justify-content:center;
  }
  .composer { display:flex; gap:.6rem; width:100%; max-width:108ch; align-items:flex-end; }
  textarea {
    flex:1; background:var(--surface); color:var(--fg);
    border:1px solid var(--outline); border-radius:8px; padding:.6rem .75rem;
    font:inherit; resize:none; min-height:2.9rem; max-height:12rem; overflow-y:auto;
  }
  textarea:focus { outline:none; border-color:var(--primary); }
  textarea::placeholder { color:var(--muted); opacity:.55; }
  button {
    background:transparent; color:var(--fg); border:1px solid var(--outline);
    border-radius:8px; padding:0 1rem; height:2.9rem; font:inherit; cursor:pointer;
    white-space:nowrap;
  }
  button:hover { border-color:var(--primary); color:var(--primary); }
  #stop:hover { border-color:var(--error); color:var(--error); }
  button:active { background:var(--primaryContainer); }

  @media (max-width:640px) {
    .row { grid-template-columns:1fr; gap:.15rem; }
    .who { text-align:left; }
  }
</style>

<header>
  <span class="mark">magi</span>
  <span class="magi"><span class="m">MELCHIOR</span> <span class="b">BALTHASAR</span> <span class="c">CASPER</span></span>
  <span class="sid">{{SID}}</span>
  <span id="state">connecting…</span>
</header>

<main id="log"></main>

<form id="f"><div class="composer">
  <textarea id="t" rows="1" placeholder="Ask magi to do something…  (enter to send, shift+enter for a newline)"></textarea>
  <button type="submit">send</button>
  <button type="button" id="stop">interrupt</button>
</div></form>

<script>
const log = document.getElementById('log'), state = document.getElementById('state');

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

let es;
function connect() {
  es = new EventSource('/events');
  es.onopen = () => { state.className = 'live'; state.textContent = 'live'; };
  es.onmessage = e => draw(JSON.parse(e.data));
  // The daemon outliving this page is normal, and so is the reverse. Reconnect quietly rather
  // than making a restart look like a failure.
  es.onerror = () => { state.className = 'lost'; state.textContent = 'reconnecting…';
                       es.close(); setTimeout(connect, 1500); };
}
connect();

async function post(path, body) {
  const r = await fetch(path, {method:'POST', body});
  if (!r.ok) { state.className = 'lost'; state.textContent = (await r.text()).trim().slice(0, 80); }
}

const f = document.getElementById('f'), t = document.getElementById('t');
const grow = () => { t.style.height = 'auto'; t.style.height = Math.min(t.scrollHeight, 192) + 'px'; };
t.addEventListener('input', grow);
f.onsubmit = e => {
  e.preventDefault();
  const v = t.value.trim(); if (!v) return;
  t.value = ''; grow(); post('/submit', new URLSearchParams({text: v}));
};
t.onkeydown = e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); f.requestSubmit(); } };
document.getElementById('stop').onclick = () => post('/interrupt', null);
</script>
`
