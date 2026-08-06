package main

// indexHTML is the whole front end: one file, no build step, no dependency fetched at load.
//
// A framework would mean a bundler, a lockfile and a second toolchain for a transcript and a text
// box — and magi is one static binary precisely so there is nothing to install. The page is also
// readable in its entirety, which matters for something that shows a working directory's contents
// on a port.
//
// %s is the session id.
const indexHTML = `<!doctype html>
<meta charset="utf-8">
<title>magi</title>
<style>
  :root { color-scheme: dark light; --bg:#14110d; --fg:#e8e2d8; --dim:#8d8578; --line:#2a251e; --accent:#ff7a1a; }
  @media (prefers-color-scheme: light) { :root { --bg:#faf7f2; --fg:#221d16; --dim:#6b6355; --line:#e2dbcf; } }
  * { box-sizing: border-box; }
  body { margin:0; background:var(--bg); color:var(--fg); font:14px/1.55 ui-monospace,SFMono-Regular,Menlo,monospace; }
  header { position:sticky; top:0; background:var(--bg); border-bottom:1px solid var(--line);
           padding:.6rem 1rem; display:flex; gap:1rem; align-items:baseline; }
  header b { color:var(--accent); }
  header span { color:var(--dim); font-size:12px; }
  main { padding:1rem; padding-bottom:7rem; max-width:110ch; margin:0 auto; }
  .row { margin:.45rem 0; display:grid; grid-template-columns:5.5rem 1fr; gap:.75rem; align-items:start; }
  .who { color:var(--dim); text-align:right; user-select:none; }
  .txt { white-space:pre-wrap; overflow-wrap:anywhere; }
  .user .txt { color:var(--accent); }
  .thinking .txt, .tool .txt { color:var(--dim); }
  .failed .who, .failed .txt { color:#e5534b; }
  form { position:fixed; left:0; right:0; bottom:0; background:var(--bg); border-top:1px solid var(--line);
         padding:.75rem 1rem; display:flex; gap:.5rem; }
  textarea { flex:1; background:transparent; color:var(--fg); border:1px solid var(--line); border-radius:6px;
             padding:.5rem; font:inherit; resize:vertical; min-height:2.6rem; }
  button { background:transparent; color:var(--fg); border:1px solid var(--line); border-radius:6px;
           padding:0 .9rem; font:inherit; cursor:pointer; }
  button:hover { border-color:var(--accent); }
</style>
<header><b>magi</b> <span id="sid">%s</span> <span id="state">connecting…</span></header>
<main id="log"></main>
<form id="f">
  <textarea id="t" placeholder="Ask magi to do something…  (enter to send, shift+enter for a newline)"></textarea>
  <button type="submit">send</button>
  <button type="button" id="stop">interrupt</button>
</form>
<script>
const log = document.getElementById('log'), state = document.getElementById('state');
// Follow the tail only while the reader is already at the bottom. Yanking the view back down while
// somebody is reading the middle of a long run is the way to make a live page useless.
function atBottom() { return window.innerHeight + window.scrollY >= document.body.offsetHeight - 40; }
function draw(rows) {
  const stick = atBottom();
  log.replaceChildren(...rows.map(r => {
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
  es.onopen = () => state.textContent = 'live';
  es.onmessage = e => draw(JSON.parse(e.data));
  // The daemon outliving this page is the normal case, and so is the reverse. Reconnect quietly
  // rather than making a restart look like a failure.
  es.onerror = () => { state.textContent = 'reconnecting…'; es.close(); setTimeout(connect, 1500); };
}
connect();
async function post(path, body) {
  const r = await fetch(path, {method:'POST', body});
  if (!r.ok) state.textContent = 'error: ' + (await r.text()).trim();
}
const f = document.getElementById('f'), t = document.getElementById('t');
f.onsubmit = e => { e.preventDefault(); const v = t.value.trim(); if (!v) return;
  t.value = ''; post('/submit', new URLSearchParams({text: v})); };
t.onkeydown = e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); f.requestSubmit(); } };
document.getElementById('stop').onclick = () => post('/interrupt', null);
</script>
`
