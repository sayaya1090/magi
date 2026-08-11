// RxJS, from this binary (vendor/README.md says how it was built). The console has three streams
// that used to be hand-rolled: the language pack every label reads, the fleet poll, and the
// transcript. Hand-rolling them is what produced the races this page has already been caught by —
// a slow answer landing on a panel that had been redrawn, a poll that kept firing after you left.
// The Material Web components. Imported for the side effect — each module registers its custom
// element — so the M3 design comes from the system instead of being written here a second time.
import '/vendor/material.js';   // buttons, text fields, tabs
import { BehaviorSubject, timer, from, of, EMPTY,
         switchMap, catchError, map, distinctUntilChanged, shareReplay,
         filter as onlyWhen } from '/vendor/rxjs.js';
// The markdown LEXER, and nothing else it can do. See vendor/README.md: the transcript is arbitrary
// output from a model and from tools, so no HTML is ever built from it. Tokens in, DOM nodes out.
import { lexer as mdLex } from '/vendor/marked.js';

// ── labels ───────────────────────────────────────────────────────────────────
// The same shape the handbook uses: a flat dot-keyed pack per locale, chosen by localStorage then
// the browser, falling back to English when the pack cannot be read. Published as a stream so a
// label change reaches everything that draws one, rather than being read once at startup by
// whichever function happened to run first.
// Where this page is mounted. The binary serves it at the root, so BASE is '/' and every url the
// router builds has looked like '/?v=skills' for that reason. A static copy of the same page lives
// under /<repo>/ on a project site, where those escape to the domain root: the clicks still work
// because they are intercepted, but the address pushed is wrong and a reload lands nowhere.
//
// Read from the document rather than configured, so nothing has to be told where it was put.
const BASE = location.pathname.replace(/[^/]*$/, '');
const at = query => BASE + (query || '');

// Seeded, not empty. The page is served with its English pack inlined ahead of this module, so the
// FIRST paint already has words — without it every label would show its dotted key until a fetch
// came back, which is a flash of debug output on somebody's dashboard.
const labels$ = new BehaviorSubject(globalThis.__LANG || {});
let L = {};
labels$.subscribe(v => { L = v; });
// t('nav.lessons') — the key IS the fallback, so a missing entry shows the key rather than a blank
// space, which is the difference between "somebody forgot to translate this" and "this is empty".
// A companion's state in the reader's language. Not tr() directly: tr falls back to the KEY, which
// would put "state.gone" on a row, and the raw word is the better fallback because it is at least
// the thing itself. Five states reached the screen in English on a Korean page while the pack
// carried all five — written, and never called.
const stateWord = s => L['state.' + s] || s || '';
const tr = (key, vars) => {
  let out = L[key] ?? key;
  if (vars) for (const [k, v] of Object.entries(vars)) out = out.replace('{' + k + '}', v);
  return out;
};
// What the reader most likely reads, in the order that respects a stated choice over a guess: this
// console's own setting, then every language the browser lists (navigator.languages is ordered by
// preference, and reading only navigator.language ignores the rest of it), then English.
//
// Matched against the packs that exist, so a browser asking for a language nothing is written in
// falls through to the next one it asked for rather than to a 404.
const PACKS = ['en', 'ko'];
// The two preferences, written out. Every key is a literal because the check that a label exists in
// both language packs reads them out of this file — a key assembled at runtime ('pref.' + kind) is
// one the check cannot see, and an unseen key renders as its own dotted name on somebody's screen.
const CHOICES = {
  theme: {
    label: 'pref.theme',
    options: [['system', 'pref.theme.system'], ['light', 'pref.theme.light'], ['dark', 'pref.theme.dark']],
  },
  lang: {
    // Kept in step with PACKS by a test, not by care.
    label: 'pref.lang',
    options: [['system', 'pref.lang.system'], ['en', 'pref.lang.en'], ['ko', 'pref.lang.ko']],
  },
};
// A preference is what somebody chose, and 'system' is a choice too — the choice to keep asking the
// machine. Stored as that word rather than as the resolved value, so a reader who picks 'system' on
// a light morning is still following the machine that evening.
const prefOf = kind => localStorage.getItem(kind) || 'system';
// The theme is an attribute on the root, which is what the stylesheet's two override blocks read.
// Nothing is written for 'system': the absence of the attribute IS the deferral, and the
// prefers-color-scheme query underneath answers.
function applyTheme() {
  const want = prefOf('theme');
  if (want === 'system') document.documentElement.removeAttribute('color-theme');
  else document.documentElement.setAttribute('color-theme', want);
}
applyTheme();
// What the page is actually showing, which is not the same as what was chosen: 'system' resolves
// through the machine. The toggle needs the resolved answer to know which way to flip.
const showing = () => {
  const want = prefOf('theme');
  if (want !== 'system') return want;
  return matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
};
const locale = () => {
  const chosen = prefOf('lang');
  const asked = [chosen === 'system' ? null : chosen, ...(navigator.languages || [navigator.language])];
  for (const tag of asked) {
    const code = (tag || '').slice(0, 2).toLowerCase();
    if (PACKS.includes(code)) return code;
  }
  return 'en';
};
// The locale's pack, then English, then the keys. The last step is not really reachable — the pack
// is served by the same process as the page — but a screen full of dotted keys is a better failure
// than a screen full of blanks, because it says what went wrong.
// Asked for with a revalidation the browser cannot skip.
//
// The pack is served no-cache with an ETag, which is the right answer for every copy stored from
// now on — and no answer at all for the copies already stored under the max-age this used to send.
// A browser holding one of those reuses it for a day without asking, so a console that had been
// open before an upgrade went on rendering every new label as its own dotted key. Measured: a
// default fetch answered from the cache with the old pack while the same URL fetched no-store
// carried the new one.
const pack$ = url => from(fetch(url, {cache: 'no-cache'})).pipe(
  switchMap(r => r.ok ? from(r.json()) : EMPTY),
  // A pack is an object of strings. Anything else — a list, a null, an error page that happened to
  // parse — is not one, and letting it through would blank every label on the page and repaint the
  // screen to say so.
  onlyWhen(pack => !!pack && typeof pack === 'object' && !Array.isArray(pack)),
  catchError(() => EMPTY),
);
// Named, because it runs again when somebody picks a language rather than only at startup. A
// reload would do it too and would throw away the transcript to change one word.
function loadPack() {
  const want = locale();
  pack$(at('i18n/language.' + want + '.json'))
    .pipe(catchError(() => EMPTY))
    .subscribe({
      next: pack => labels$.next(pack),
      complete: () => {
        if (Object.keys(L).length || want === 'en') return;
        pack$(at('i18n/language.en.json')).subscribe(pack => labels$.next(pack));
      },
    });
}
loadPack();
// Anything already on screen is repainted when a pack lands. Guarded on the first paint having
// happened: this subscribes before the page's own elements are declared, and a BehaviorSubject
// hands its current value to a new subscriber immediately — painting there would reach for
// constants that do not exist yet.
let painted = false;

// Whose console this is. Not an account — magi has no users to log in — but the two facts that
// answer "am I looking at the right machine": the host it runs on and the config directory it
// reads. A supervisor with three of these open in three tabs has asked that question.
function loadConsole() {
  fetchList('/console').then(c => {
    if (!c) return;
    consoleEl.replaceChildren();
    embedModel = c.embedModel || '';
    for (const [k, val] of [['field.host', c.host], ['field.config', c.configDir]]) {
      if (!val) continue;
      const line = cell('');
      const b = document.createElement('b');
      b.textContent = tr(k) + ' ';
      line.append(b, document.createTextNode(val));
      consoleEl.append(line);
    }
  });
}
// ── notifications ────────────────────────────────────────────────────────────
// A phone that is asleep, told that a companion is blocked.
//
// # Why this is the only channel that reaches anybody
//
// The tab title carries the count, and that is a channel to a person who has the tab in front of
// them. The fleet page exists for the person who does not. Everything else this console can do
// requires somebody to be looking at it.
//
// # Three ways this can be unavailable, and they are different
//
// A switch that is simply missing teaches nobody anything, so each reason says itself:
//
//   - no service worker or no PushManager — an old browser, and nothing to be done here;
//   - not a secure context — the page is being read over plain http from another machine. Note that
//     a tunnel to localhost IS secure, and magi-web only ever binds loopback, so the ordinary way
//     of reaching this from a phone already qualifies;
//   - permission denied — the browser was told no once and will not ask again from a click. Only
//     the reader can undo that, in the browser's own settings.
const notifyBtn = document.getElementById('notifyBtn');
const notifyWhy = document.getElementById('notifyWhy');
let vapidKey = null;

// A base64url key becomes the Uint8Array PushManager wants. It takes no other form.
const keyBytes = k => {
  const b = atob(k.replace(/-/g, '+').replace(/_/g, '/') + '==='.slice((k.length + 3) % 4));
  return Uint8Array.from(b, c => c.charCodeAt(0));
};

async function currentSub() {
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) return undefined;
  const reg = await navigator.serviceWorker.getRegistration();
  return reg ? reg.pushManager.getSubscription() : null;
}

async function paintNotify() {
  const why = (key, on) => {
    notifyWhy.textContent = tr(key);
    notifyBtn.disabled = !on;
  };
  document.getElementById('notifyK').textContent = tr('notify.k');
  // The static demo has no console behind it and does not export the worker. Checked first, because
  // every reason below it would be the browser's and this one is the page's.
  if (globalThis.MAGI_DEMO) {
    notifyBtn.textContent = tr('notify.on');
    return why('notify.demo', false);
  }
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) {
    notifyBtn.textContent = tr('notify.on');
    return why('notify.unsupported', false);
  }
  if (!window.isSecureContext) {
    notifyBtn.textContent = tr('notify.on');
    return why('notify.insecure', false);
  }
  if (Notification.permission === 'denied') {
    notifyBtn.textContent = tr('notify.on');
    return why('notify.denied', false);
  }
  const sub = await currentSub();
  notifyBtn.textContent = tr(sub ? 'notify.off' : 'notify.on');
  why(sub ? 'notify.is_on' : 'notify.how', true);
}

notifyBtn.onclick = async () => {
  // The prompt is asked for FIRST, before anything is awaited. requestPermission needs transient
  // user activation, and an await hands the turn back to the event loop — the activation is spent
  // by the time the call is reached, and it resolves 'default' without ever showing a prompt. That
  // is exactly what "it does not ask for permission" looks like: a button that does nothing.
  //
  // Harmless when a subscription already exists, which is the other thing this button does: a
  // permission already granted resolves immediately and shows nobody anything.
  const asked = 'Notification' in window && Notification.permission !== 'granted'
    ? Notification.requestPermission() : Promise.resolve('granted');
  notifyBtn.disabled = true;
  try {
    const existing = await currentSub();
    if (existing) {
      // Told BOTH sides. Unsubscribing only in the browser leaves this console posting to an
      // endpoint that answers 410 for a while and then stops existing; telling only the console
      // leaves the browser holding a subscription nothing will ever use.
      await post('/push', new URLSearchParams({
        endpoint: existing.endpoint, p256dh: '-', auth: '-', delete: '1'}));
      await existing.unsubscribe();
      return paintNotify();
    }
    if (await asked !== 'granted') return paintNotify();
    if (!vapidKey) {
      const info = await fetchList('/push');
      vapidKey = info && info.key;
    }
    if (!vapidKey) { notifyWhy.textContent = tr('notify.nokey'); return; }
    const reg = await navigator.serviceWorker.register('/sw.js');
    // The worker has to be running before it can be subscribed against; a registration that is
    // still installing has no active worker and subscribe throws.
    await navigator.serviceWorker.ready;
    const sub = await reg.pushManager.subscribe({
      // Every browser requires this. A subscription that could deliver silently is a subscription
      // a page could use to track somebody without showing them anything.
      userVisibleOnly: true,
      applicationServerKey: keyBytes(vapidKey),
    });
    const j = sub.toJSON();
    await post('/push', new URLSearchParams({
      endpoint: j.endpoint, p256dh: j.keys.p256dh, auth: j.keys.auth}));
  } catch (e) {
    notifyWhy.textContent = String(e && e.message || e);
  } finally {
    paintNotify();
  }
};

labels$.pipe(distinctUntilChanged()).subscribe(() => { if (painted) paint(); });

const fleetEl = document.getElementById('fleet'), log = document.getElementById('log');
const state = document.getElementById('state'), sidEl = document.getElementById('sid');
const back = document.getElementById('back'), f = document.getElementById('f');
const summaryEl = document.getElementById('summary');
const tabsEl = document.getElementById('tabs');
// ── the companion page's two panels, below the two-column width ──────────────
// Which of a companion's two halves is on screen. Only meaningful under 840px, where the columns
// have collapsed into a stack; above it both are visible and the strip is display:none.
//
// Not in the URL. The destination is the companion; which half of it you were reading is a scroll
// position, and putting it in the address bar would make a link somebody sends land on a screen
// they did not mean to share. It does not survive a reload either, on purpose: arriving at a
// companion, the conversation is what you came for.
const ptabs = document.getElementById('ptabs');
const streamEl = document.getElementById('stream'), sideEl = document.getElementById('side');
const detailEl = document.getElementById('detail');
let panel = 'talk';
// A media query object rather than a width read: it fires on the change, so a window dragged past
// the breakpoint re-lays out without waiting for anything else to happen.
const wide = matchMedia('(min-width:52.5em)');
// List-detail: from this width a chosen companion appears BESIDE the list rather than instead of
// it. The guide's own example of a pane is exactly this shape — the list of messages is one pane,
// a specific thread is another — and going to a page for it meant the list, the count and whoever
// else was waiting all left the screen to read one row.
//
// 1000px rather than the 840 where two panes are first recommended: at 840 the detail is already
// two panes of its own (the conversation and the facts beside it), and a third at that width gives
// the conversation about 350px. The facts pane folds by hand and is remembered, so somebody who
// wants the list at 840 can have it by folding the other.

function drawPanels() {
  const s = sock();
  ptabs.hidden = !s || wide.matches;
  if (!s || wide.matches) {
    // Both halves, as they were. Nothing may stay hidden from a previous narrow visit.
    if (sideEl) sideEl.hidden = false;
    if (detailEl) detailEl.hidden = !s;
    log.hidden = !s;
    return;
  }
  const talk = panel === 'talk';
  log.hidden = !talk;
  detailEl.hidden = talk;
  sideEl.hidden = talk;
}
// Only when the reader switched, not on the poll that redraws the facts four times a minute.
// Sideways, in the direction the reader moved. Talk sits left of state, so arriving at state comes
// in from the right and going back to talk comes in from the left — which is what tells somebody
// these two are peers rather than one being under the other.
function revealPanel(fromIndex) {
  const how = fromIndex === undefined ? 'enter'
            : (panel === 'state' ? 'slideL' : 'slideR');
  reveal(panel === 'talk' ? log : detailEl, how);
  if (panel !== 'talk') reveal(sideEl, how);
}
ptabs.addEventListener('change', () => {
  const was = panel;
  panel = ptabs.activeTabIndex === 1 ? 'state' : 'talk';
  drawPanels();
  revealPanel(was === panel ? undefined : 0);
  measureDock();
});
wide.addEventListener('change', drawPanels);
// Dragging a window past this one changes which shape the page is in, not just how it is spaced,
// so it is a re-render rather than a re-layout.

const intervenedEl = document.getElementById('intervened');
const skillsEl = document.getElementById('skills'), tabSkills = document.getElementById('tabSkills');
const boardEl = document.getElementById('board');
const mcpEl = document.getElementById('mcp');
// The last fleet answer, so the "which companion" picker names them without a second fetch.
let fleetSeen = [];
const tabFleet = document.getElementById('tabFleet');
const railEl = document.getElementById('rail');
const langEl = document.getElementById('lang');
const prefsK = document.getElementById('prefsK'), consoleK = document.getElementById('consoleK');
const prefsEl = document.getElementById('prefs');
const prefsDialog = document.getElementById('prefsDialog');
const mcpDialog = document.getElementById('mcpDialog');
const stopDialog = document.getElementById('stopDialog');
const stopK = document.getElementById('stopK'), stopBody = document.getElementById('stopBody');
const stopCancel = document.getElementById('stopCancel'), stopGo = document.getElementById('stopGo');
const mcpFormEl = document.getElementById('mcpForm');
const mcpDialogK = document.getElementById('mcpDialogK');
const mcpCancel = document.getElementById('mcpCancel'), mcpGo = document.getElementById('mcpGo');
const prefsClose = document.getElementById('prefsClose');
const railMenu = document.getElementById('railMenu');
// There are TWO facts about the connection and they were one variable.
//
// One is whether the polls land — the fleet, the skills, the servers, all plain fetches. The other
// is whether the transcript's stream is open. They fail apart: a daemon that is answering /fleet
// while the SSE is dead is the exact case a person needs told about, and it is the case this dot
// used to hide, because every successful poll wrote the indicator clear and the stream's own
// failure lasted until the next one — measured at 400ms, three seconds apart.
//
// So they are kept apart and the dot shows the worse of the two. Anything else is a readout with
// two writers, which is how it was wrong.
// There is a THIRD fact, and it is the one that matters most on a companion's own page: whether
// that companion is still there. The two above are both about this console's link to the server
// that serves it, and the server outlives the daemon — so killing a companion left the dot green
// and the word "connected" beside a page whose subject no longer exists. Reported from a live
// console, watching the daemon it had just stopped.
let streamAt = '', reachOK = true, companionOK = true;
function paintConn() {
  const lost = !reachOK || !companionOK || streamAt === 'lost';
  state.classList.toggle('lost', lost);
  state.classList.toggle('live', !lost && streamAt === 'live');
}
let openMCP = () => {};
const noteEl = document.getElementById('note');
const conn = how => { streamAt = how; paintConn(); };
const reach = ok => { reachOK = ok; paintConn(); };
// Said as well as drawn, and only on the edge: the note is a status line with several writers, and
// repeating this into it every three seconds would keep overwriting whatever else it was saying.
const companionAlive = ok => {
  if (companionOK === ok) return;
  companionOK = ok;
  paintConn();
  says(ok ? '' : tr('state.companion_gone'));
};
const railBadge = document.getElementById('railBadge'), tabBadge = document.getElementById('tabBadge');
const themeToggle = document.getElementById('themeToggle');
const consoleEl = document.getElementById('console');
const railFleet = document.getElementById('railFleet');
const railSkills = document.getElementById('railSkills');
// Which resource this console is showing. A companion's own page is neither — it is one level in.
// Corrections used to be a destination of its own and is now the first half of the experience
// page. An address somebody kept still lands on the thing it was pointing at.
// board went the same way in the other direction: it was a destination and is now reached from the
// fleet, because it is a question ABOUT the fleet — what these companions have been doing — rather
// than a fifth place to be. Both addresses still land where they pointed.
// mcp joined them. What a companion has LEARNED and what it can REACH are the two halves of what
// an organisation shares — one is knowledge and one is capability, and both are managed by the same
// person on the same afternoon. Two destinations for them made the reader carry the connection.
const RENAMED = {interventions: 'skills', mcp: 'skills'};
const view = () => {
  const v = new URLSearchParams(location.search).get('v') || 'fleet';
  return RENAMED[v] || v;
};
const crumbSep = document.getElementById('crumbSep'), crumbHere = document.getElementById('crumbHere');
const crumbSep2 = document.getElementById('crumbSep2'), crumbDeep = document.getElementById('crumbDeep');
const crumbSep3 = document.getElementById('crumbSep3'), crumbLeaf = document.getElementById('crumbLeaf');
// The four sections, named as nouns: a tab is a place you are, and "what I had to say" is a
// sentence about it. The same words do three jobs — the tab, the crumb, and the browser title —
// so they are written once.
const SECTION_KEY = {fleet: 'nav.companions', skills: 'nav.shared', board: 'nav.board'};
const SECTION = new Proxy({}, {get: (_, v) => tr(SECTION_KEY[v] || 'nav.companions')});

const HREF = {fleet: '', skills: '?v=skills', board: '?v=board'};
// In the order they are written in the markup, because md-tabs addresses its tabs by index.
// The board is not among them. It keeps its address and its crumb; what it lost is a permanent
// seat in a navigation that has to fit on a phone, for a screen somebody opens when they have a
// question about the past rather than one they live on.
const TABS = ['fleet', 'skills'];

const sock = () => new URLSearchParams(location.search).get('d');
// One level in from a companion's conversation. Addresses, not state: a screen somebody is looking
// at is a screen they can send to somebody else, and the back button is the way out of it for free.
// The sub parameter is a child session id; cr is "<round>:<member>", a council seat in a round.
const subOf = () => new URLSearchParams(location.search).get('sub') || '';
const crOf = () => new URLSearchParams(location.search).get('cr') || '';
// past is the third: "" for the list of finished work, a session id for one of them. Present with
// an empty value, which is why it is read with has() rather than by truthiness — an address that
// means "the list" and one that means "not here" are different addresses.
const pastOn = () => new URLSearchParams(location.search).has('past');
const pastOf = () => new URLSearchParams(location.search).get('past') || '';
// ask is the decision itself, opened at full width. The bar above the composer stays — it is the
// right shape for "allow this one command" — but a report is three sections of prose and a strip
// under a transcript is not where anybody reads three sections of prose.
const askOf = () => new URLSearchParams(location.search).get('ask') || '';
// insp is what the terminal answers with /tools and /loop: what this companion can do, and the
// shape of the run so far. One parameter with two values rather than two parameters, because they
// are alternatives — you are looking at one of them — and every level here is addressed the same
// way so that a screen can be sent to somebody.
const inspOf = () => new URLSearchParams(location.search).get('insp') || '';
const deepIn = () => !!(sock() && (subOf() || crOf() || pastOn() || askOf() || inspOf()));
// Going one level in and coming back out. Both are pushState + render, so the address bar, the
// crumbs and what is drawn can never disagree — there is one source and it is the URL.
function goDeep(param, value) {
  const u = new URLSearchParams(location.search);
  u.delete('sub'); u.delete('cr'); u.delete('past'); u.delete('ask'); u.delete('insp');
  // An empty value is still a value here: ?past= is the list. Only a null clears the level.
  if (value !== null && value !== undefined) u.set(param, value);
  history.pushState({}, '', '?' + u.toString());
  render();
}
const goBackUp = () => goDeep('sub', null);

// The third crumb says which level you are standing on, and it is made of words.
//
// Its own function because two callers need it: render(), which knows the level changed, and
// paint(), which knows the language did. Written only by render(), it kept whatever the inlined
// English seed said for as long as the screen stayed open — measured on a Korean browser standing
// in past work: "companions / design / What it has done", with every other label around it in
// Korean. paint() already repaints the first crumb for exactly this reason; the deeper one was
// added later and missed.
function paintDeepCrumb() {
  crumbDeep.textContent = !deepIn() ? ''
    : inspOf() === 'tools' ? '🛠 ' + tr('insp.tools')
    : inspOf() === 'loop' ? '↻ ' + tr('insp.loop')
    : askOf() ? '⏸ ' + tr('ask.deciding')
    : crOf() ? '⚖ ' + crOf().split(':').slice(1).join(':')
    : pastOn() ? tr('field.history')
    : '◆ ' + tr('detail.subagent');
}

// goVerdict opens one member's vote, KEEPING which session it was read in.
//
// goDeep clears every level, which is right when they are alternatives — and a verdict read on a
// past session is not an alternative to that session, it is inside it. Cleared, the screen asked
// the LIVE session for a round that belongs to a finished one: the evidence came back null and the
// vote could not be found in the transcript on screen, so the way in from a past council row led
// to a page with a name on it and nothing else. Reported as "the council's evidence cannot be
// reached once the work is done".
function goVerdict(round, member) {
  const u = new URLSearchParams(location.search);
  u.delete('sub'); u.delete('ask'); u.delete('insp');
  u.set('cr', round + ':' + member);
  history.pushState({}, '', '?' + u.toString());
  render();
}
const peerOf = () => new URLSearchParams(location.search).get('p') || '';
// The pair (peer, socket) identifies a companion once more than one console is in the list: a
// socket path is only meaningful on the machine that owns it.
const q = () => sock() ? '?d=' + encodeURIComponent(sock()) + (peerOf() ? '&p=' + encodeURIComponent(peerOf()) : '') : '';

// ── the fleet ────────────────────────────────────────────────────────────────
// A span of seconds in the largest unit that still says something. Two readings of it: how long
// ago something happened, and how long into a turn somebody waited before stepping in — the second
// is not an "ago" and reads as nonsense with the suffix on it.
const dur = s => s < 60 ? s + 's' : s < 3600 ? Math.round(s/60) + 'm'
               : s < 86400 ? Math.round(s/3600) + 'h' : Math.round(s/86400) + 'd';
// The unit stays compact — s/m/h/d reads the same in every language this ships in, and a table
// column sized for "4s" cannot hold "4 seconds". Only the word is translated, which is the part
// that was English on an otherwise Korean row.
const ago = s => s < 0 ? '' : tr('time.ago', {d: dur(s)});

// The order the eye should travel: what needs somebody, what is moving, what is asleep, what is
// gone. Kubernetes consoles sort trouble to the top for the same reason — a list you have to read
// to find the problem is a list that hides it.
const ORDER = {waiting: 0, working: 1, idle: 2, abandoned: 3, stopped: 4, remote: 5};
// Elsewhere is its own group and not part of 'gone'. A companion on another machine has not
// stopped — nothing here dialled it, so nothing here knows either way — and putting it under the
// heading for crashes would be a claim the row itself refuses to make.
const GROUP = {waiting: 'waiting', working: 'working', idle: 'idle', abandoned: 'gone', stopped: 'gone',
  remote: 'remote'};
let filter = null;   // one of the summary keys, or null for everything

// href is where a companion lives in this console's URL space.
function href(a) {
  return '/?d=' + encodeURIComponent(a.socket) + (a.peer ? '&p=' + encodeURIComponent(a.peer) : '');
}

function cell(cls, text) {
  const d = document.createElement('div');
  d.className = cls;
  if (text !== undefined) d.textContent = text;
  return d;
}

// ── icons ────────────────────────────────────────────────────────────────────
// An icon, when this build has one, and whatever the page drew before when it does not.
//
// The art is Font Awesome Pro and its licence lets you use it in something you deploy but not
// republish it as files, so it is baked into the binary at build time and is simply absent from a
// build that had no licence to bake from — a contributor's checkout, or a CI job without the token.
// That is a supported state, not a broken one: this asks the document whether the symbol is there,
// and hands back the fallback when it is not.
//
// Asked of the DOM rather than of a list the build also has to ship: the sprite IS the list, it is
// already in the page, and a second copy of the answer is a second thing that can be wrong.
//
// The reference is written out in full — "#i-<style>-<icon>", where sl = sharp light, ss = sharp
// solid, b = brands — because the generator finds the icons to bake by grepping this file for
// exactly that string. Assembled from parts it would find none, and the sprite would be empty on
// a build that had every licence it needed. So the id is greppable on purpose.
const SVGNS = 'http://www.w3.org/2000/svg';
function icon(ref, opts) {
  const name = String(ref).replace(/^#i-/, '');
  const has = typeof document.getElementById === 'function' && document.getElementById('i-' + name);
  if (!has) return (opts && opts.fallback) ? opts.fallback() : null;
  const svg = document.createElementNS(SVGNS, 'svg');
  // 'sic', not 'ic'. The rail's own drawings have carried class="ic" since before there was a
  // sprite, sized by a width attribute — and a class here called 'ic' set width:1em on them, which
  // a stylesheet wins: every rail icon quietly shrank from 24px to 14. Different thing, different
  // name.
  svg.setAttribute('class', 'sic ' + ((opts && opts.cls) || ''));
  svg.setAttribute('aria-hidden', 'true');
  // No width or height: the size is the use site's business and belongs in the stylesheet, where
  // it can answer a reader who raised their default font size.
  const use = document.createElementNS(SVGNS, 'use');
  // Both spellings. href is the current one; xlink:href is what Safari needed for years, and the
  // cost of keeping it is one attribute.
  use.setAttribute('href', '#i-' + name);
  use.setAttributeNS('http://www.w3.org/1999/xlink', 'xlink:href', '#i-' + name);
  svg.append(use);
  return svg;
}

// withMark puts an icon in front of a component's label, in the slot the library keeps for one.
//
// Material's buttons take a leading icon through slot="icon" — not as a child of the label — and
// giving them one is the difference between a word in a dense list and a control somebody can find
// by shape. Nothing happens where the build has no sprite: the word was always enough on its own,
// and a blank slot would only add a gap in front of it.
function withMark(btn, ref) {
  const m = icon(ref);
  if (!m) return btn;
  m.setAttribute('slot', 'icon');
  btn.prepend(m);
  return btn;
}

// dressIcons swaps the page's own drawings for the baked ones, where there are baked ones.
//
// The markup cannot ask a question — it is a static document — so it carries the shape it has
// always carried and names the symbol it would rather be in data-i. This runs once and replaces
// the contents of the ones whose symbol arrived, keeping the element itself: the stylesheet, the
// size and the aria-hidden are already right on it, and the component it sits inside slotted it a
// long time before this ran.
//
// Full "#i-…" refs in the attribute, for the reason icon() takes them: the generator finds what to
// bake by grepping for that string, and page.html is one of the two files it reads.
function dressIcons(root) {
  for (const box of (root || document).querySelectorAll('[data-i]')) {
    const ref = box.getAttribute('data-i');
    const drawn = icon(ref);
    if (!drawn) continue;                       // no sprite in this build: keep what is there
    box.replaceChildren(...drawn.children);     // the <use>, into the svg the markup already has
  }
}

// iconOr is the common shape: an icon if there is one, else the mark the page has always drawn.
// Returns a node either way, so callers append rather than branch.
function iconOr(ref, glyph, cls) {
  return icon(ref, {cls: cls, fallback: () => {
    const s = el('span', glyph);
    s.className = 'gl ' + (cls || '');
    return s;
  }});
}

// What each companion's state was the last time the table was drawn, so the next draw can tell
// which rows are news. Keyed by socket rather than by index: the fleet gains and loses rows, and an
// index would report the whole table as changed the moment one of them left.
const wasState = new Map();

// card is one row of the table. The class list is the state, so the left rule and the status colour
// come from one place, and the row is a link because opening it is the common case.
function card(a) {
  const el = document.createElement('a');
  // A state change gets a wash, once. Not on the first sight of a companion — everything is new
  // then, and a table that lights up entirely on load has told the reader nothing.
  const before = wasState.get(a.socket);
  const news = before !== undefined && before !== a.state;
  wasState.set(a.socket, a.state);
  el.className = 'card ' + a.state + ' state' + (a.here ? ' here' : '') + (news ? ' noticed' : '')
    + (a.socket === sock() ? ' open' : '');
  // A companion on another machine is not opened from here. Its socket is a path on ITS
  // filesystem, and this console would resolve that path against its own — which on two machines
  // set up by one person is frequently a real companion, the wrong one. So the row is shown and
  // does not link, which is the honest shape of "we know it exists and cannot reach into it".
  if (a.state !== 'remote') {
    el.href = href(a);
    el.onclick = e => { e.preventDefault(); go(a.socket, a.peer); };
  }

  const badge = cell('badge', stateWord(a.state));
  // How far through its own plan, INSIDE the status cell. Not a progress bar: a todo list is not a
  // schedule, and a bar would promise a completion time nobody can honour.
  //
  // Inside, because it is conditional and the row is a grid of seven fixed columns. As a sibling it
  // was an eighth item on the rows that had a plan: every cell after it shifted one column right,
  // the task landed in the step count's 72px, and the actions wrapped onto a line of their own.
  // A conditional cell in a fixed template is a row that reads differently depending on its data.
  if (a.planTotal) badge.append(cell('plan', a.planDone + '/' + a.planTotal));
  el.append(badge);

  const who = cell('who-col');
  who.append(cell('name', a.name));
  // What it is for, when it says so. The path stays: a role is how you pick a companion and a
  // path is how you go and look at it, and neither answers the other's question.
  if (a.role) who.append(cell('role', a.role));
  // The team is NOT repeated here. It is on the group's heading, which is where it belongs — and
  // with grouping on whenever any companion declares one, a team cell in the row could only ever
  // say again what the line above it just said. Unreachable the rest of the time, because "no
  // grouping" and "no teams" are the same condition.
  who.append(cell('path', a.workdir));
  el.append(who);

  // What it is doing. A blocked agent shows the question instead — that is the thing to know — and
  // the buttons that answer it sit under the question rather than in the actions column, because
  // they belong to the prompt and not to the row.
  const doing = cell('doing');
  if (a.asking) {
    doing.append(cell('asking', '⏸ ' + a.asking), answerBox(a));
  } else if (a.task) {
    doing.append(cell('last', a.task));
    // Under the request, when a tool inside it is reporting. The task says what was asked and the
    // step count says how much has been done; neither says anything about a turn that has been
    // inside one call for ten minutes, which is the row somebody is squinting at wondering whether
    // it is stuck. Only ever set on a working agent (see fleet.Agent.Doing).
    if (a.doing) {
      const n = cell('note');
      n.append(iconOr('#i-sl-spinner-third', '\u23F3', 'spin'), document.createTextNode(' ' + a.doing));
      doing.append(n);
    }
  }
  el.append(doing);

  // The step count carries its own word on a phone. The column heads are hidden there, so this and
  // the two beside it were three unlabelled readings — "3  4s ago  studio" — and only the age says
  // what it is on its own.
  const steps = cell('num r', a.steps ? a.steps + '' : '—');
  steps.append(cell('colk', tr('field.steps')));
  el.append(steps);
  el.append(cell('num r', ago(a.idle)));

  const host = cell('host');
  const name = document.createElement('b');
  // The console it came from first when there is one: with three machines federated, that is the
  // thing the eye needs before the hostname.
  name.textContent = a.peer ? a.peer : (a.host || 'this machine');
  host.append(name);
  if (a.addr) host.append(document.createElement('br'), document.createTextNode(a.addr));
  if (a.here) host.append(document.createElement('br'), document.createTextNode('this directory'));
  el.append(host);

  el.append(rowActions(a));
  // The grounds go last and take the whole row. They are paragraphs, and a paragraph in a table
  // column 12rem wide comes out one word per line — measured, and it looked like a bug rather than
  // like reasoning. Marked "span" so the check that a row has as many cells as the head has
  // columns still counts columns: this is not one.
  const why = grounds(a);
  if (why) el.append(why);
  return el;
}

// confirmStop asks before halting a turn, and says what halting it costs.
//
// A row in a list is an easy thing to mis-hit, and the cost of hitting this one is whatever the
// agent was in the middle of. The guide keeps dialogs for exactly this — "critical information
// that requires a decision", "confirmation of a choice before committing to it" — and is specific
// about the shape: a headline that poses the question concretely rather than "Are you sure?", the
// dismissive action to the LEFT of the confirming one, and a confirming label that says what will
// happen instead of "OK".
//
// The same dialog guards the button beside the composer. One action, one question: two behaviours
// for one verb would be the console teaching that stopping means different things in different
// corners of it.
function confirmStop(who, go) {
  // The headline names the companion when there is a name to use, and asks the same question
  // without one rather than leaving a hole where a name should be.
  stopK.textContent = who ? tr('stop.headline', {name: who}) : tr('stop.headline_plain');
  stopBody.textContent = tr('stop.body');
  stopCancel.textContent = tr('action.keep_running');
  withMark(stopCancel, '#i-sl-play');
  stopGo.textContent = tr('action.interrupt');
  withMark(stopGo, '#i-ss-circle-stop');
  stopCancel.onclick = () => stopDialog.close('cancel');
  stopGo.onclick = () => { stopDialog.close('stop'); go(); };
  stopDialog.show();
}

// rowActions: stopping, and only that.
//
// "open" is gone. The whole row is a link to the companion, so a button beside it saying "open" was
// a second way to do what a click anywhere already did — and the pair of labelled buttons was wider
// than the column they sit in, which is why they hung off the right edge of the table.
//
// Stopping stays, and it stays HERE rather than one level in: the row you want to halt is the one
// you are already looking at, and making somebody open it first is how a runaway turn gets another
// thirty seconds. As an icon, because it is one action in a 6rem column and the word did not fit.
function rowActions(a) {
  const box = cell('actions');
  if (!(a.live && (a.state === 'working' || a.state === 'waiting'))) return box;
  // A solid glyph, in the colour of what it does.
  //
  // It drew an OUTLINED square in the muted role, which is the toggle vocabulary exactly — the
  // guide: "toggle buttons should use an outlined icon when unselected, and a filled version when
  // selected", while "default icon buttons should use filled icons". So the one control that halts
  // a running turn was drawn as an unticked checkbox, and it was grey until the cursor was on it:
  // a destructive action whose meaning appeared only after you had found it.
  //
  // Not md-filled-icon-button, which is what the guide would give a high-emphasis action: that
  // component is not in the vendored bundle, and a page that names an element nobody registered
  // gets an inert span — measured, 20px with no container at all. The container is a re-vendor, and
  // the two things that were actually wrong are the glyph and the colour.
  const stop = document.createElement('md-icon-button');
  stop.className = 'stop';
  stop.setAttribute('aria-label', tr('action.interrupt'));
  tip(stop, tr('action.interrupt'));
  stop.innerHTML = '<svg data-i="#i-ss-circle-stop" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">' +
    '<rect x="7" y="7" width="10" height="10" rx="1.5" fill="currentColor"/></svg>';
  dressIcons(stop);
  stop.onclick = e => {
    e.preventDefault(); e.stopPropagation();
    confirmStop(nameOf(a.socket) || a.name, () => post('/interrupt', null, a.socket, a.peer).then(loadFleet));
  };
  box.append(stop);
  return box;
}

function tableHead() {
  const h = cell('thead');
  // Written out here in English until now, on a page where every other word came from the pack —
  // and invisible to the audit that finds unasked-for phrases, because a column nobody translated
  // has no key to go unused. The last column is the actions, which has no heading to give.
  for (const [c, key] of [['', 'col.status'], ['', 'col.agent'], ['', 'col.doing'],
                          ['r', 'col.steps'], ['r', 'col.age'], ['', 'col.host'], ['r', '']]) {
    h.append(cell(c, key ? tr(key) : ''));
  }
  return h;
}

// The summary is four numbers and a filter. Counting rows to find out whether anything needs you is
// the work this removes, and it is the first thing a console shows.
function summarise(list) {
  const box = document.getElementById('summary');
  const counts = {waiting: 0, working: 0, idle: 0, gone: 0, remote: 0};
  for (const a of list) counts[GROUP[a.state] || 'idle']++;
  box.replaceChildren(...Object.entries(counts)
    // A zero is informative for the four states a local companion moves between — "nothing is
    // waiting" is worth reading. It is not informative for a cluster: somebody with one machine
    // has no companions elsewhere and never will, and a chip permanently reading zero is a box
    // that has to be looked at every time to learn nothing.
    .filter(([k, n]) => k !== 'remote' || n > 0)
    .map(([k, n]) => {
    const b = document.createElement('md-filter-chip');
    b.className = 'tile ' + k;
    b.disabled = n === 0;
    // The chip's own selected state, not an aria attribute of ours. It toggles itself on click and
    // this list is rebuilt from filter on the next render, so the two cannot drift.
    b.selected = filter === k;
    // The state's own mark, in the slot a chip keeps for one: waiting is a pause, working is the
    // turning spinner, idle is the moon, and one nobody can reach is a broken link. The count and
    // the word stay — the mark is a third way of saying it, not a replacement for the two that
    // survive a build with no icons.
    const m = icon(STATE_MARK[k] || '');
    if (m) { m.setAttribute('slot', 'icon'); b.append(m); }
    b.append(cell('n', n + ''), cell('k', stateWord(k)));
    b.onclick = () => {
      filter = filter === k ? null : k;
      render();
      if (filter) jumpToFirstRow();
    };
    return b;
  }));
  // The way to the board, from the list it is about. Text rather than a chip: the chips are a
  // filter on this table and this is not — a control that looked like them and did something else
  // would be the worst of both.
  // …and only when there is a past to look at. On a machine with no companions the board can
  // never have held anything, and a control that can be pressed to reach a blank screen is worse
  // than one that is not there — the same rule the zero tiles above already follow.
  if (list.length) {
    // An icon, not a word. The row it sits in is four counting chips, and a fifth thing shaped like
    // a word reads as a fifth count — this is a way OUT of the list rather than a filter on it, and
    // the shape is what says so. It keeps its name in the tooltip and its aria-label, because an
    // icon alone is a guess for anybody who has not pressed it once.
    const past = document.createElement('md-icon-button');
    past.className = 'toboard';
    tip(past, tr('nav.board'));
    past.setAttribute('aria-label', tr('nav.board'));
    // The drawn columns stay as the fallback and the baked one takes over where there is one, which
    // is the same bargain the markup's icons strike — see dressIcons.
    past.innerHTML = '<svg data-i="#i-sl-chart-kanban" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">' +
      '<path d="M4 5.5h5v13H4zM9.5 5.5h5v8h-5zM15 5.5h5v10.5h-5z" fill="none" ' +
      'stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>';
    dressIcons(past);
    past.onclick = () => { history.pushState({}, '', at(HREF.board)); render(); };
    box.append(past);
  }
}

// grounds is the report the agent wrote for whoever has to decide.
//
// Drawn as a labelled block rather than folded into the question, because it is not the question:
// the question is one line and this is the working behind it — what was tried, what each way
// costs, which way the agent leans. Its sections are whatever the decision-report skill asked for,
// so nothing here names them; they arrive in the order that skill put them in, and that order is
// part of the report.
//
// Returns null when there are none. A companion running an older build, or one whose report was
// dropped somewhere between its socket and this page, gets the prompt it always had rather than an
// empty box implying somebody left the grounds blank.
function grounds(a) {
  if (!a.report || !a.report.length) return null;
  const box = cell('grounds span');
  for (const sec of a.report) {
    if (!sec || !sec.text) continue;
    // Each section is its own box rather than two loose cells in a shared grid. Flat, the block
    // could only ever be one column of pairs: on a row 1400px wide that left the report in the
    // left 45% and 770px of card empty beside it, because the one thing a column of prose must
    // not do is grow to the width of the furniture. Boxed, the sections can sit side by side —
    // each keeps a readable line, and together they use the room.
    const s = cell('gsec');
    s.append(cell('gk', sec.key), cell('gv', sec.text));
    box.append(s);
  }
  return box.children.length ? box : null;
}

// markWaiting puts the count on the companions section, in both navigations.
//
// Hidden at zero rather than shown as "0": a badge is there to be noticed, and one that is always
// present is one the eye stops reading. Both surfaces are set from here so the rail and the tabs
// cannot end up claiming different numbers.
function markWaiting(n) {
  for (const b of [railBadge, tabBadge]) {
    // Four characters including the "+", which is what the badge container is drawn to hold.
    b.value = n ? (n > 999 ? '999+' : String(n)) : '';
    b.hidden = !n;
  }
  // A badge is a number with its meaning in its POSITION, and position is the one thing a screen
  // reader does not have. Measured in the accessibility tree: the rail's destination read out as
  // "Companions" with the count nowhere in it, so somebody listening was never told anybody was
  // waiting.
  //
  // As CONTENT, not as a label on the host. A list item of type link renders its own anchor in a
  // shadow root and the name comes from what is slotted into it — an aria-label on the host is set,
  // is visible in the DOM, and does not reach the link. Measured both ways: the attribute stood
  // there reading "Companions, 2 waiting on you" while the tree still said "Companions".
  const said = n ? ', ' + tr('state.waiting_on_you', {n}) : '';
  // Into the CONTENT of both, which is what names a link. It used to need a patch written into the
  // list item's shadow root — the component put role="listitem" on the anchor it rendered, and a
  // listitem takes no name from what is inside it. The rail is an anchor now and this is all it is.
  for (const host of [railFleet, tabFleet]) {
    let note = host.querySelector('.srcount');
    if (!note) { note = srOnly(''); note.classList.add('srcount'); host.append(note); }
    note.textContent = said;
  }
}

// srOnly is a phrase for the reader who is listening and not looking. Used where a number is
// drawn as a badge or a bare count: the digit carries its meaning in where it sits, and where it
// sits is exactly what does not survive into a screen reader.
function srOnly(text) {
  const s = document.createElement('span');
  s.className = 'sr-only';
  s.textContent = text;
  return s;
}

// arm makes a destructive control ask once before it acts.
//
// M3's answer to a destructive action is a confirmation, not a colour, and this page had neither:
// forget and remove posted on the first press, and the only warning was an error colour that
// appeared on HOVER — which does not exist on a phone, so on the surface where a misplaced thumb is
// likeliest there was no warning at all.
//
// Two presses rather than a dialog. A dialog for "forget this lesson" is heavier than the act it
// guards, and it would take a component this page does not carry; the second press is the same
// gesture, one row away from where the eye already is. The error colour arrives with the question,
// so the colour appears exactly when it means something.
//
// It disarms itself after a few seconds. An armed control left sitting is one that will be pressed
// by somebody who has forgotten what it asked.
function arm(btn, label, act) {
  let armed = false, timer = 0;
  // The mark survives the label changing. textContent replaces EVERYTHING a component was given —
  // slotted icon included — so a button that had been handed one lost it on the first write and
  // again on every disarm: the two destructive controls on the lessons page were the only ones
  // that came out plain. The icon is taken out, the word is set, and it goes back in.
  const say = word => {
    const mark = [...(btn.children || [])].find(k => k.getAttribute && k.getAttribute('slot') === 'icon');
    btn.textContent = word;
    if (mark) btn.prepend(mark);
  };
  say(label);
  const reset = () => { armed = false; btn.className = btn.className.replace(' armed', ''); say(label); };
  btn.onclick = () => {
    if (armed) { clearTimeout(timer); reset(); act(); return; }
    armed = true;
    btn.className += ' armed';
    say(tr('action.confirm'));
    timer = setTimeout(reset, 5000);
  };
}

// emptyState is the two lines a screen shows when it has nothing: what is absent, and how it stops
// being absent. Both from the pack — these were the last four sentences on the page written in
// English, and they are the ones a first-time reader meets before anything else.
//
// The second line may carry markup (a command in <code>), which is why it is set as HTML. It comes
// from a pack this binary serves and embeds; nothing a companion or the network says reaches here.
function emptyState(whatKey, howKey) {
  const e = document.createElement('div');
  e.className = 'empty';
  e.innerHTML = tr(whatKey) + '<br>' + tr(howKey);
  return e;
}

// stripSource removes the provenance line the store appends to a body, so the rule reads as the
// rule. Where it came from is drawn as its own field rather than as a sentence at the end of the
// text — see the meta line.
const SOURCE_LINE = /\n*\(source: ([^)]*)\)\s*$/;
function stripSource(body) { return body.replace(SOURCE_LINE, '').trim(); }
function sourceOf(body) { const m = SOURCE_LINE.exec(body || ''); return m ? m[1] : ''; }

// jumpToFirstRow brings the top row of the filtered list into view.
//
// Filtering alone was not enough. On a phone the list starts below a screen of masthead and
// summary, and the report a decision rests on was measured at 989px down a 720px screen — so
// pressing "2 waiting" reordered something the reader could not see. Scrolling is the other half
// of the answer to "show me what needs me".
//
// Deferred a frame: the rows are replaced by the render() that precedes this, and measuring an
// element the layout has not placed yet gives a position it is about to leave.
// jumpToNextWaiting brings the next blocked companion into view, skipping the one just answered.
//
// Skipped by socket rather than by "the first waiting row", because the fleet is polled and the row
// just answered can still be drawn as waiting for one more tick — landing back on it would look
// like the answer did not take.
function jumpToNextWaiting(justAnswered) {
  requestAnimationFrame(() => {
    for (const row of fleetEl.querySelectorAll('.card.waiting')) {
      if (row.getAttribute('href') && justAnswered &&
          row.getAttribute('href').includes(encodeURIComponent(justAnswered))) {
        continue;
      }
      if (row.scrollIntoView) row.scrollIntoView({block: 'center', behavior: 'smooth'});
      return;
    }
  });
}

function jumpToFirstRow() {
  requestAnimationFrame(() => {
    const first = fleetEl.querySelector('.card');
    if (first && first.scrollIntoView) first.scrollIntoView({block: 'start', behavior: 'smooth'});
  });
}

// answerBox is the reply to a blocked agent, next to the question it answers.
//
// The buttons stop the click from opening the agent (the row is a link) — reading and answering are
// different intentions and the same tap must not do both.
function answerBox(a) {
  const box = document.createElement('div'); box.className = 'answer';
  // The socket is passed, not spliced into the path: post() adds the target itself, and doing it
  // in both places produced /answer?d=X?d=X — invisible on the fleet, where post()'s own target is
  // empty, and broken on an agent's page, where it is not.
  // Answering moves on to the next one still waiting.
  //
  // Two blocked companions among twenty is a list you hunt through twice: the count at the top
  // takes you to the first, and after that you are on your own — scrolling a table looking for the
  // other pause marker. Answering is a QUEUE, not a browse, and the difference between the two is
  // whether the page advances or leaves you where you were.
  //
  // It does not switch the filter or navigate anywhere. Somebody who was reading the whole fleet
  // stays reading the whole fleet; the view just moves to the row that now needs them. When
  // nothing else is waiting it does nothing at all, which is the right end of a queue.
  const send = (text) => post('/answer', new URLSearchParams({call: a.askId, kind: a.askKind, text}),
                              a.socket, a.peer)
    .then(loadFleet)
    .then(() => jumpToNextWaiting(a.socket));
  if (a.askKind === 'question') {
    const i = document.createElement('md-outlined-text-field');
    i.label = tr('label.answer');
    const b = document.createElement('md-filled-button'); b.textContent = tr('action.answer');
  withMark(b, '#i-ss-paper-plane');
    // Disabled until there is something to send, rather than pressable and inert. The guide is
    // explicit that an action which cannot happen is DISABLED and not hidden, and the third state
    // — drawn as pressable and then doing nothing — is the one it does not offer: a press that
    // answers nothing reads as broken, and there is no way to tell it from a page that has died.
    const arm2 = () => b.toggleAttribute('disabled', !i.value.trim());
    arm2();
    const go = e => { e.preventDefault(); e.stopPropagation(); if (i.value.trim()) send(i.value.trim()); };
    b.onclick = go;
    i.addEventListener('input', arm2);
    i.onclick = e => { e.preventDefault(); e.stopPropagation(); };
    i.onkeydown = e => { if (e.key === 'Enter') go(e); };
    box.append(i, b);
  } else {
    // "always" is not a mode and does not touch one: it grants THIS tool for THIS session, in the
    // daemon's memory, and the approval mode is exactly where it was. The label said "Always",
    // which reads as a promise about every tool and every run — the terminal's own list is
    // clearer, because it puts the project rule beside it as a separate choice.
    for (const [label, decision] of [['action.allow', 'allow'], ['action.always', 'always'],
                                     ['action.deny', 'deny']]) {
      // Filled tonal, not text. M3 ranks buttons by emphasis — filled, filled tonal, outlined, text
      // — and these three were at the BOTTOM of it while being the highest-stakes control on the
      // page: the one that approves a "drop table" on a live database. On the same screen, answering
      // "which surface should the empty state sit on?" was drawn as a filled button, so the page
      // was shouting about a design question and whispering about a destructive command.
      //
      // All three at the SAME weight, deliberately. Raising only "allow" would make the page nudge
      // toward approving, and a console that leans on a permission decision is worse than one that
      // draws it quietly. The colour stays neutral for the same reason; the question above them
      // already carries the warning colour.
      const b = document.createElement('md-filled-tonal-button'); b.textContent = tr(label);
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
// How many were waiting when the title was last written, so a repaint can rewrite it without
// re-counting a fleet it has not fetched.
let lastWaiting = 0;
function retitle(waiting) {
  lastWaiting = waiting;
  // Where you are goes in the title as well: with four sections and a page you leave open, the tab
  // strip in a browser is the outermost breadcrumb somebody actually reads.
  const s = sock();
  const where = s ? nameOf(s) : SECTION[view()] || tr('nav.companions');
  const name = 'magi · ' + where;
  document.title = waiting ? '(' + waiting + ') ' + name : name;
}

// drawPrompt puts what an agent is blocked on above its own composer.
//
// An agent's page was the one place this could not be seen: the prompt is not in the log — it is a
// question about what should happen, not a record of what did — so the transcript showed a run that
// had simply stopped, and the only way to find out was to go back to the fleet.
// Whether the prompt bar was already up. It is redrawn on every poll while an agent waits, so
// animating on "it is visible" would restart the entrance three times a minute under somebody who
// is trying to read the question.
let promptWasUp = false;
function drawPrompt(a) {
  const box = document.getElementById('prompt');
  if (!a || a.state !== 'waiting') {
    box.hidden = true; box.replaceChildren(); promptWasUp = false; measureDock(); return;
  }
  const inner = document.createElement('div'); inner.className = 'inner';
  const k = document.createElement('div'); k.className = 'asking';
  // Which of how many, when there is more than one coming. Silent at one, which is nearly all of
  // them: "1 of 1" answers a question nobody asked and teaches the eye to skip the spot where the
  // real one shows up.
  k.textContent = '⏸ ' + a.asking + (a.askTotal > 1 ? '  ' + tr('ask.of', {i: a.askIndex, n: a.askTotal}) : '');
  inner.append(k);
  const why = grounds(a);
  if (why) {
    // The report, folded into the width it has here, plus the way to the width it wants. A decision
    // with grounds is one somebody has to READ before answering, and this bar is a strip under a
    // transcript — the right shape for "run this command?" and the wrong one for three sections of
    // prose written by an agent that worked for an hour while nobody was watching.
    inner.append(why);
    const open = el('button', tr('ask.open'));
    open.type = 'button';
    open.className = 'deeper hit48';
    open.onclick = () => goDeep('ask', a.askId);
    inner.append(open);
  }
  // A question is answered in the composer, not in a second box above it. Both drawn, the page had
  // two text fields one over the other — the upper one answering the question and the lower one
  // sending a fresh request to an agent that is not listening — and no mark saying which was which.
  // A permission prompt keeps its own controls: they are buttons, so nothing collides, and leaving
  // the composer live there is deliberate — "do something else instead" is a legitimate reply to
  // being asked whether to drop a table.
  if (a.askKind !== 'question') inner.append(answerBox(a));
  box.replaceChildren(inner);
  box.hidden = false;
  if (!promptWasUp) reveal(box, 'rise');
  promptWasUp = true;
  answerMode(a.askKind === 'question' ? a : null);
  measureDock();
}

// What the composer is for right now. Null when it sends work; an agent when it answers that
// agent's question. Held here rather than read off the DOM because the submit handler needs the
// call id, and a call id parked in a data attribute is a string that can go stale without anything
// noticing.
let answering = null;
// One field in two roles rather than two fields that have to agree about which is live. The
// component is the same md-outlined-text-field; what changes is the label, the supporting line
// under it, and where pressing the button sends what was typed.
function answerMode(a) {
  answering = a;
  t.setAttribute('label', tr(a ? 'label.answer' : 'label.ask'));
  const note = document.getElementById('cnote');
  note.textContent = a ? tr('answer.instead') : '';
  note.hidden = !a;
  // The word AND the mark, and both change with the mode: a paper plane for sending something into
  // the conversation, a reply arrow for answering the question above it. textContent wipes the
  // slot, so the mark goes back on after the word — the same footgun arm() hit.
  const send = document.getElementById('send');
  send.textContent = tr(a ? 'action.answer' : 'action.send');
  withMark(send, a ? '#i-sl-reply' : '#i-ss-paper-plane');
  // The old text was addressed at magi and the new question is not the same subject. Carrying it
  // over would put a half-written request in front of somebody as though it were their answer.
  if (!!a !== wasAnswering) { t.value = ''; }
  wasAnswering = !!a;
}
let wasAnswering = false;

// What this companion has done before now.
//
// Every other panel on this page is about the turn it is in. When that turn ends the page shows the
// next one, so "what has this one actually been doing" — the question somebody has after being away
// — had no answer here, while the answer sat in the log store the whole time.
//
// The request as it was made, not a summary of it. A summary would be this page deciding what the
// work was about, which quietly rewrites what somebody asked for.
// findQuery is what has been typed into the history card's search. Kept outside the draw so a
// fleet poll redrawing this panel does not throw away what somebody is in the middle of typing.
let findQuery = '';

// findField is the search box over this companion's past work.
//
// On the same screen as the list, not on one of its own: the list already IS the past sessions,
// and a search that lived elsewhere would be a second list of the same things. The terminal made
// the same call about its resume picker.
function findField(total) {
  const wrap = cell('find');
  // The Material field, not a bare input. A bare one has no text size of its own, inherits the
  // body's 14px, and iOS Safari zooms the page on focus and does not zoom back — there is a test
  // over the whole page for exactly this, and it caught this field.
  const input = document.createElement('md-outlined-text-field');
  input.className = 'findin';
  input.value = findQuery;
  input.setAttribute('type', 'search');
  input.setAttribute('label', tr('find.placeholder'));
  // Debounced: every keystroke would otherwise read every log in the workspace.
  let timer;
  input.addEventListener('input', () => {
    findQuery = input.value;
    clearTimeout(timer);
    timer = setTimeout(() => render$past().then(() => {
      // Redrawing replaces the field, so the caret goes back where it was — a search box that
      // loses focus mid-word is a search box you cannot type into.
      const again = detailEl2().querySelector('.findin');
      if (again) { again.focus(); again.setSelectionRange(again.value.length, again.value.length); }
    }), 220);
  });
  wrap.append(input);
  if (!findQuery) wrap.append(cell('findn', String(total)));
  return wrap;
}

// drawFound puts the ranked sessions, and the turns that matched, into the card.
async function drawFound(box, a) {
  const hits = await fetchList('/search?q=' + encodeURIComponent(findQuery) + '&' + qFor(a).slice(1));
  if (!hits) return;
  if (!hits.length) {
    // Said, rather than an empty card. A list that silently empties under the keystrokes is one
    // somebody cannot tell from a broken one.
    box.append(cell('findnone', tr('find.none')));
    return;
  }
  for (const h of hits) {
    const row = el('button');
    row.type = 'button';
    row.className = 'hs found hit48';
    row.append(cell('when', new Date(h.when).toLocaleDateString()));
    let what = h.title || tr('history.untitled');
    if (h.scheduled) what += ' · ' + tr('find.scheduled');
    row.append(cell('what', what));
    // The found session opens too, the same way a listed one does.
    row.onclick = () => goDeep('past', h.id);
    box.append(row);
    // WHY it matched. With a search typed the titles no longer explain the list — the words were
    // matched somewhere in the middle of the conversation, which is the whole reason for searching
    // turns rather than titles.
    for (const sn of h.snippets || []) box.append(cell('snip', sn.prompt));
    if (h.turns > (h.snippets || []).length) {
      box.append(cell('snip more', tr('find.more').replace('{n}', h.turns - (h.snippets || []).length)));
    }
  }
}

// qMore appends the companion selector to a query string that already has one.
function qMore() {
  const s = sock();
  return s ? '&d=' + encodeURIComponent(s) : '';
}

// ── the board ────────────────────────────────────────────────────────────────
// Work as cards, a column per companion, and a day you can move.
//
// # Why the columns are companions and not states
//
// A kanban's columns are usually a state, and a state is a fact about NOW: waiting, working, idle.
// There is no such thing as the state a companion was in last Tuesday — the fleet derives state
// from what is open in a log, and nothing is open in a log from last week. Columns of state would
// be a board that could only ever show today, which is the one day the fleet page already covers.
//
// So the column is who did it and the card is the work, which reads the same on any day. The day
// moves; the shape does not.
//
// # One request per companion
//
// The history endpoint answers for one companion because that is the question every other panel
// asks it. Fanning out here rather than adding a fleet-wide endpoint keeps one way to ask, and the
// fan-out is the size of the fleet — a handful, once per visit.
// Which day a timestamp fell on, in the READER's timezone.
//
// It used to be the first ten characters of the RFC3339 string, which is the UTC day — and it was
// compared against todayISO(), which is the local one. East of UTC in the evening the two disagree,
// so the board showed the wrong day's work and could filter out a session running at that moment:
// measured at 00:30 KST, four cards became one, and the one that survived was the only one whose
// UTC and local days happened to straddle the boundary.
//
// A person asking what happened on the 9th means their own 9th. Formatted through the same offset
// arithmetic todayISO uses, so the two are the same function applied to two different instants.
const dayOf = ts => {
  const t = Date.parse(ts || '');
  if (Number.isNaN(t)) return '';
  return new Date(t - new Date(t).getTimezoneOffset() * 60000).toISOString().slice(0, 10);
};
const todayISO = () => new Date(Date.now() - new Date().getTimezoneOffset() * 60000)
  .toISOString().slice(0, 10);
// The time of day, local, as HH:MM. Empty for anything unparseable rather than "Invalid".
const hhmm = ts => {
  const t = Date.parse(ts || '');
  if (Number.isNaN(t)) return '';
  return new Date(t - new Date(t).getTimezoneOffset() * 60000).toISOString().slice(11, 16);
};
let boardDay = '';
let boardQuery = '';

async function loadBoard() {
  const list = await fetchList('/fleet');
  if (!list) return;
  fleetSeen = list;
  if (!boardDay) boardDay = todayISO();

  const head = cell('boardhead');
  const day = document.createElement('md-outlined-text-field');
  day.setAttribute('type', 'date');
  day.setAttribute('label', tr('board.day'));
  day.value = boardDay;
  day.addEventListener('change', () => { boardDay = day.value || todayISO(); loadBoard(); });
  // Reading a week backwards is the common way to use this, and a date field makes that four
  // interactions per day: open the picker, find the cell, click, wait. A step is one click. The
  // field stays because jumping to a date a month back is the other way to use it, and stepping
  // there would be thirty clicks.
  const step = (delta, key) => {
    const b = document.createElement('md-icon-button');
    b.setAttribute('aria-label', tr(key));
    tip(b, tr(key));
    b.innerHTML = '<svg data-i="' + (delta < 0 ? '#i-sl-chevron-left' : '#i-sl-chevron-right') +
      '" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">' +
      '<path d="' + (delta < 0 ? 'M14.5 5.5 8 12l6.5 6.5' : 'M9.5 5.5 16 12l-6.5 6.5') +
      '" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" ' +
      'stroke-linejoin="round"/></svg>';
    dressIcons(b);
    b.onclick = () => {
      // Parsed as UTC and stepped in whole days, so the answer does not depend on which side of a
      // daylight-saving change the reader is standing on: local-midnight arithmetic lands on 23:00
      // the previous day twice a year, and a board that skips a day is worse than no arrows.
      const d = new Date(boardDay + 'T00:00:00Z');
      d.setUTCDate(d.getUTCDate() + delta);
      boardDay = d.toISOString().slice(0, 10);
      loadBoard();
    };
    return b;
  };
  const today = document.createElement('md-text-button');
  today.textContent = tr('board.today');
  today.onclick = () => { boardDay = todayISO(); loadBoard(); };
  // Same rule as the forward arrow beside it, which has been disabled on today all along: a
  // control that cannot go anywhere says so rather than answering a press with nothing.
  if (boardDay >= todayISO()) today.setAttribute('disabled', '');
  // Narrowing a day's work by what it was about. Ranked the same way the shared-knowledge search
  // is, so "the one about retries" finds it without knowing how the request was worded — and over
  // the cards already fetched, so it narrows as you type rather than after a round trip.
  const find = document.createElement('md-outlined-text-field');
  find.setAttribute('label', tr('label.find'));
  find.value = boardQuery;
  find.addEventListener('input', () => { boardQuery = find.value; loadBoard(); });
  // Tomorrow holds nothing: the store only has what has happened. The forward arrow is disabled on
  // today rather than hidden, because a control that vanishes moves the ones beside it.
  const fwd = step(1, 'board.next');
  if (boardDay >= todayISO()) fwd.setAttribute('disabled', '');
  head.append(step(-1, 'board.prev'), day, fwd, today, find);

  // Ordered the way the fleet is: trouble first. A board that sorted by name would bury the column
  // somebody needs behind the alphabet.
  const cols = [...list].sort((x, y) => (ORDER[x.state] - ORDER[y.state]) || (x.idle - y.idle));
  const runs = await Promise.all(cols.map(a =>
    fetchList('/history?d=' + encodeURIComponent(a.socket) + (a.peer ? '&p=' + encodeURIComponent(a.peer) : ''))
      .then(h => h || [])));

  const lanes = cell('lanes');
  let anything = false;
  // A lane per TEAM, not per companion.
  //
  // Per companion, a console with six of them had six columns whose heads were a name, a role
  // sentence and a team word — so every lane started at a different height and the cards below
  // them never lined up, which is the one thing a board is for. And the columns were the wrong
  // cut: work belongs to a team, and which companion did it is a fact about the card.
  //
  // Companions with no team keep their own lane; nothing declares a team on a single-workspace
  // machine, and grouping those under one heading would invent a team that does not exist.
  const laneOf = a => a.team || a.name;
  const order = [];
  const byLane = new Map();
  cols.forEach((a, i) => {
    const key = laneOf(a);
    if (!byLane.has(key)) { byLane.set(key, []); order.push(key); }
    byLane.get(key).push([a, i]);
  });
  order.forEach(key => {
    const members = byLane.get(key);
    const a = members[0][0];
    // A session counts for the day if it was running at any point in it, not only if it began
    // then: a task started at 23:40 and finished at 01:10 belongs to both days somebody might ask
    // about, and belonging to neither is how a long night disappears from a board.
    // Every member's work, each card remembering who did it.
    let work = [];
    for (const [who, i] of members) {
      for (const h of runs[i]) {
        if (dayOf(h.started) <= boardDay && dayOf(h.ended) >= boardDay) work.push({...h, who: who.name});
      }
    }
    work.sort((x, y) => String(y.started).localeCompare(String(x.started)));
    if (boardQuery.trim()) {
      const order = rankByIDF(boardQuery,
        work.map(h => [h.title, h.model, ...(h.labels || [])].filter(Boolean).join(' ')));
      work = order.map(k => work[k]);
    }
    if (!work.length) return;
    anything = true;
    const lane = cell('lane');
    const title = document.createElement('h2');
    title.className = 'lanehead';
    title.append(cell('lname', key));
    // The head is the team's name and a count, and nothing else. It used to carry the companion's
    // role sentence too, which is what made the six heads six different heights — the cards under
    // them started at six different places and could not be read across. Who did a piece of work
    // is on the card that says it.
    title.append(cell('lcount', work.length + ''));
    lane.append(title);
    for (const h of work) {
      // The card is a CONTAINER, not a button. A card is allowed to be one large target with
      // nothing actionable inside it, or a plain container holding actions — not both. It used to
      // be both: an onclick on the card and another on every label, with stopPropagation between
      // them, which is the shape the guide names when it says an action must not sit on an
      // actionable surface. It also put every one of those actions on a div, so a keyboard could
      // reach none of them — the fleet row beside it has been an <a> the whole time.
      const card = cell('wcard' + (h.current ? ' now' : ''));
      // The clock face is the reader's too. A card that said 22:00 for work somebody started at
      // seven in the morning is not telling them about their own day.
      const when = cell('wwhen', h.current ? tr('board.now') : hhmm(h.started));
      card.append(when);
      // Who did it, on the same line as when — that line is the card's "who and when" and this was
      // three rows down among the model and the labels, in the same muted grey, so it read as one
      // more fact rather than the answer to whose work this is.
      if (members.length > 1 || h.who !== key) when.append(cell('wwho', h.who));
      // The title is the way in. It carries the address so the companion is reachable with a middle
      // click and a copied url, the same as the fleet row.
      const mine = members.find(([m]) => m.name === h.who);
      const owner = mine ? mine[0] : a;
      const what = document.createElement('a');
      what.className = 'wwhat';
      what.href = href(owner);
      what.textContent = h.title || tr('history.untitled');
      what.onclick = e => { e.preventDefault(); go(owner.socket, owner.peer); };
      card.append(what);
      // Which companion did it. It was the column heading and is now a fact about the card, which
      // is the right place for it: the column is the team, and a team is several of them.
      // How long it took, when it is over. A card that says only when it started tells you nothing
      // about whether the day went well.
      if (!h.current && h.started && h.ended) {
        const mins = Math.round((Date.parse(h.ended) - Date.parse(h.started)) / 60000);
        if (mins > 0) card.append(cell('wlong', dur(mins * 60)));
      }
      // Which engine did it. The lane says WHO, and a companion's model can be changed mid-life —
      // so the one it is on now says nothing about the work on this card, and this is the only
      // place that fact survives.
      if (h.model) card.append(cell('wmodel', h.model));
      // What the agent said it was about. First on the card after the title, because it is the one
      // line somebody scanning a week is actually reading for.
      for (const l of h.labels || []) {
        // A button, because pressing it does something. As a div with an onclick it was invisible
        // to the keyboard and announced as nothing.
        const chip = document.createElement('button');
        chip.type = 'button';
        chip.className = 'wlabel hit48';
        chip.textContent = l;
        // Pressing one searches for it, which is the whole point of a label: the second piece of
        // work that carries it is what you are looking for.
        chip.onclick = () => { boardQuery = l; loadBoard(); };
        card.append(chip);
      }
      lane.append(card);
    }
    lanes.append(lane);
  });

  boardEl.replaceChildren(head,
    anything ? lanes : emptyState('board.nothing', 'board.nothing_how'));
}

// What the board would draw, as one string. The poll below rebuilds only when this changes: the
// cards come from every companion's history, and redrawing an identical board would blink the lane
// somebody is reading and throw away the sideways scroll of the strip.
async function boardSig() {
  const list = await fetchList('/fleet');
  if (!list) return null;
  const runs = await Promise.all(list.map(a =>
    fetchList('/history?d=' + encodeURIComponent(a.socket) + (a.peer ? '&p=' + encodeURIComponent(a.peer) : ''))
      .then(h => h || [])));
  return JSON.stringify(runs);
}

// A list from this console, or null when the console itself cannot be reached.
//
// The three loaders had this same try/catch, and the distinction it draws is the one thing they
// must not get differently: "magi-web is not answering" is a fact about the page you are looking
// at, and it is not the same as a companion being quiet. Null, so a caller cannot mistake the
// failure for an empty list and draw "nothing here" over a screen that simply lost its server.
async function fetchList(path) {
  // A refusal is not an exception. fetch only rejects when the request never completed, so a
  // daemon answering 500 came back through the happy path — .json() threw on the error body if it
  // was lucky, and returned garbage if it was not. Measured against a mock returning 500: the
  // console went on showing the fleet it had, three seconds old and then a minute old, with a
  // green dot and nothing said. Stale and confident is the worst of the three states this can be
  // in; the other two both tell you.
  let r;
  try { r = await fetch(path); }
  catch { reach(false); says(tr('error.unreachable')); return null; }
  if (!r.ok) {
    reach(false);
    const why = (await r.text().catch(() => '')).trim();
    says(why ? why.slice(0, 80) : tr('error.unreachable'));
    return null;
  }
  try { return await r.json(); }
  catch { reach(false); says(tr('error.unreachable')); return null; }
}

// fetchOne is fetchList for something that is not a list. Same failure handling, because the
// distinction it draws — "this console is not answering" against "there is nothing here" — is the
// same one and must not be drawn twice, differently.
const fetchOne = fetchList;

// councilWordOf is the council's vocabulary as a reader should see it. The log's words are
// mechanical; "done" on its own reads as a status rather than a judgement.
function councilWordOf(decision) {
  const key = {done: 'council.accept', continue: 'council.reject', abstain: 'council.abstain'}[decision];
  return key ? tr(key) : decision;
}

async function loadFleet() {
  const list = await fetchList('/fleet');
  if (!list) return;
  reach(true);
  const waiting = list.filter(a => a.state === 'waiting').length;
  retitle(waiting);

  // On an agent's page the fleet is polled for this one entry: the prompt it is blocked on and the
  // facts in its header reach the browser no other way.
  fleetSeen = list;
  const here = sock();
  if (here) {
    const mine = list.find(a => a.socket === here && (a.peer || '') === peerOf());
    // Gone means gone: not in the list at all (its socket was cleaned up), or in it and not
    // answering. Either way the page in front of somebody is about a companion that has stopped.
    companionAlive(!!mine && mine.live !== false);
    // Redrawn only when it CHANGED. The transcript is rebuilt whole by draw(), and doing that
    // every three seconds under somebody reading a folded tool result would close what they opened.
    const note = (mine && mine.doing) || '';
    // What this companion calls the person, when a plugin has renamed them. It is a property of
    // the daemon's memory, so it arrives on this poll like the note does — and like the note, a
    // redraw is only worth it when it changed.
    const named = (mine && mine.user) || '';
    if (note !== liveNote || named !== userName) { liveNote = note; userName = named; draw(lastRows); }
    drawPrompt(mine);
    drawDetail(mine);
    loadIntervened(mine);
    loadJobs(mine);
    drawReportFormat(mine);
    // The list is not on screen while a companion is open, at any width, so the rows below would
    // be built for nobody.
    return;
  }

  // The list's own screen is about all of them, so no single companion can be missing from it.
  companionAlive(true);

  // A badge on the section that holds them, which is what M3 uses one for: a count of things
  // wanting attention, on the navigation item that leads to them. It rides the rail item's end slot
  // so it survives the rail collapsing to icons — the state where a count matters most, because the
  // words are gone and the shape is all there is.
  markWaiting(waiting);

  // The masthead's count belongs to the list's own screen. Beside a companion the same line carries
  // that companion's crumb, and writing a count over it would be the two-writers shape again.
  if (!here) drawFleetCount(list, waiting);
  if (!list.length) {
    fleetEl.replaceChildren();
    if (!here) fleetEl.append(emptyState('empty.no_agents', 'empty.no_agents_how'));
    return;
  }
  // Trouble first, then movement, then quiet, then gone; most recently active within each. A list
  // you have to read to find the problem is a list that hides it.
  const rows = list
    .filter(a => !filter || GROUP[a.state] === filter)
    .sort((x, y) => (ORDER[x.state] - ORDER[y.state]) || (x.idle - y.idle));
  // Whatever is no longer in the fleet is no longer worth remembering: a companion that was shut
  // down should not leave its row in a map that grows for the life of the tab.
  const alive = new Set(list.map(a => (a.peer || '') + ' ' + a.socket));
  for (const key of [...shownCards.keys()]) if (!alive.has(key)) shownCards.delete(key);
  fleetEl.replaceChildren(...(here ? [] : [tableHead()]), ...grouped(rows));
}

// The masthead's readout for the list's own screen: how many there are, and a way to reach whoever
// is blocked.
function drawFleetCount(list, waiting) {
  // The count says somebody is blocked; pressing it goes there. It said so and did nothing before,
  // which is the readout every console has and the reason nobody presses it.
  state.replaceChildren(document.createTextNode(
    tr(list.length === 1 ? 'count.agent' : 'count.agents', {n: list.length}) +
    (waiting ? ' · ' : '')));
  if (waiting) {
    const go = document.createElement('md-text-button');
    go.className = 'jump';
    go.textContent = tr('state.waiting_on_you', {n: waiting});
    go.onclick = () => { filter = 'waiting'; render(); jumpToFirstRow(); };
    state.append(go);
  }
  // Not the connection's class. A red dot means the stream is gone everywhere else on this page,
  // and writing it here for "somebody is waiting" both said the wrong thing and CLEARED the real
  // one: the fleet poll runs every three seconds, so a dropped stream showed for 400ms and then
  // the console went back to claiming it was connected. Measured with the mock dropping it.
  state.classList.toggle('asking', waiting > 0);
  summarise(list);
}

// The mark each state wears, where a build has marks. Written once: the chips use it, and the row
// that carries the same state should not be free to disagree with the chip that counts it.
// Keyed by the GROUP the chips count, not by the raw state: stopped and abandoned are one tile
// called "gone", and a table written against the raw states left that tile — the only one that
// means something has ended badly — as the one with no mark on it.
const STATE_MARK = {
  waiting: '#i-ss-circle-pause',
  // Still, not spinning. A turning mark in a row of counts is the only moving thing on the screen
  // and the eye goes to it and stays — this is a tally, not a progress indicator. The place a
  // spinner earns its keep is the row of the companion that is actually mid-call.
  working: '#i-ss-play',
  idle: '#i-ss-moon',
  gone: '#i-ss-circle-stop',
  remote: '#i-sl-satellite-dish',
};

// grouped lays the rows out by team, when there are teams.
//
// # Why this does not simply group
//
// The order above exists for a reason written next to it: trouble first, because a list you have to
// read to find the problem is a list that hides it. Grouping by team scatters the blocked agents
// across the page and takes that back. So the ROWS keep their order inside a group, and the GROUPS
// are ordered by the worst state in each — a team with somebody blocked comes first. The rule the
// flat list followed still holds; it now holds twice.
//
// # Why it is conditional
//
// Nothing declares a team on a single-workspace machine, and a header saying so would be a line of
// furniture over every list. Grouping appears when the data has teams in it and not before, which
// is the same test the page applies to everything else it draws.
// keptCard is card(), except that a row nothing has changed about is the row that is already on
// screen.
//
// The list is redrawn every three seconds, and it was rebuilt entirely every time: five agents,
// five new rows, five new answer fields and every state layer and animation restarted, four times
// a minute, to say exactly what the last one said. A row is cheap and five of them are cheap; the
// things attached to them are not — a field somebody is typing in, a select somebody has open, the
// hover they are aiming at.
//
// The signature is everything the row DRAWS. It has to be: a field left out of it is a field that
// stops updating, which is a worse failure than the churn this avoids — so it is built from the
// same values card() reads, in one place, and a new one added to the row belongs in it.
const shownCards = new Map();
function cardSig(a) {
  return [a.state, a.name, a.role, a.team, a.hub, a.workdir, a.session, a.steps, a.idle,
          a.task, a.doing, a.asking, a.askId, a.askKind, a.planDone, a.planTotal,
          a.host, a.addr, a.pid, a.peer, a.live, a.permission, a.user,
          (a.report || []).map(x => x.key + ':' + x.text).join('|')].join('\u0001');
}
function keptCard(a) {
  const key = (a.peer || '') + ' ' + a.socket;
  const sig = cardSig(a);
  const had = shownCards.get(key);
  if (had && had.sig === sig) return had.node;
  const node = card(a);
  shownCards.set(key, {sig, node});
  return node;
}

function grouped(rows) {
  const teams = new Map();
  for (const a of rows) {
    const key = a.team || '';
    if (!teams.has(key)) teams.set(key, []);
    teams.get(key).push(a);
  }
  // One group, and it is the unnamed one: this machine has no teams and there is nothing to say.
  if (teams.size <= 1 && teams.has('')) return rows.map(keptCard);

  const order = [...teams.entries()].sort((x, y) => {
    // The unnamed group last however its members are doing: "these belong to no team" is a remark
    // about the roster, and the roster is not the thing somebody is scanning for.
    if ((x[0] === '') !== (y[0] === '')) return x[0] === '' ? 1 : -1;
    // The most urgent member decides where the group sits. ORDER counts up from waiting, so the
    // smallest number in a group is the loudest thing in it.
    const loudest = g => Math.min(...g.map(a => ORDER[a.state]));
    return (loudest(x[1]) - loudest(y[1])) || x[0].localeCompare(y[0]);
  });
  const out = [];
  for (const [name, members] of order) {
    out.push(teamHead(name, members), ...members.map(keptCard));
  }
  return out;
}

// teamHead names a team and says who answers for it.
//
// The hub is on the header rather than on its own row: which companion speaks for a team is a fact
// about the team, and a badge buried in one row is a fact somebody has to go looking for.
function teamHead(name, members) {
  const h = document.createElement('h2');
  h.className = 'teamhead';
  h.append(cell('tname', name || tr('team.none')));
  // Every companion claiming to speak for the team, not the first one found. Two is a
  // misconfiguration — a team answers with one voice or the question of who answers is open — and
  // naming one of them would draw a settled team over an unsettled one.
  const hubs = members.filter(a => a.hub).map(a => a.name);
  if (hubs.length) h.append(cell('thub', tr('team.spoken_for', {name: hubs.join(', ')})));
  const waiting = members.filter(a => a.state === 'waiting').length;
  if (waiting) {
    const b = document.createElement('md-badge');
    b.value = String(waiting);
    // Same reason as the rail's: in the accessibility tree this heading read "backend 1 1", two
    // bare numbers with nothing to tell them apart. The digits stay drawn and the words are said.
    b.setAttribute('aria-hidden', 'true');
    h.append(b, srOnly(tr('state.waiting_on_you', {n: waiting})));
  }
  const tn = cell('tn', members.length + '');
  tn.setAttribute('aria-hidden', 'true');
  h.append(tn, srOnly(tr(members.length === 1 ? 'count.agent' : 'count.agents', {n: members.length})));
  return h;
}

// Folded or not, remembered. A preference somebody sets on one companion means the same thing on
// the next one — it is a statement about how much of the screen they want the conversation to have,
// not about this agent.
function setFolded(want) {
  const box = document.getElementById('detail');
  box.toggleAttribute('folded', want);
  const bar = box.querySelector('.foldbar');
  if (bar) bar.setAttribute('aria-expanded', want ? 'false' : 'true');
  localStorage.setItem('facts', want ? 'folded' : 'open');
  measureDock();
}

// drawDetail is the agent page's own header: what this is, where it runs, how far it has got.
// A detail view that does not say which resource it is showing is the one place a console cannot
// afford to be quiet, and a transcript does not say it.
// The four approval modes, and which one this companion is on.
//
// # Why it is a control here and not only a word
//
// The mode decides whether the companion stops for permission and how long it waits when it does,
// and it was settable in the terminal alone. Somebody watching from a phone could see a companion
// blocked on a question and had no way to say "stop asking me" — the console could answer one
// prompt and not change the rule that produced it.
//
// # Why the element outlives the redraw
//
// The facts are rebuilt on every fleet poll, which is every three seconds. A select rebuilt under
// an open menu closes it, and a select rebuilt while you are choosing throws the choice away — so
// this one is made once and kept on the card, and only its VALUE is refreshed. Not even that while
// it has focus: writing the value under somebody mid-choice is the same bug one layer down.
// Value and label key in pairs, the way the preference selects carry theirs: a key built by
// concatenation is a key the pack check cannot see, and the label it names is the one that ships
// missing.
const PERM_MODES = [['ask', 'perm.ask'], ['auto', 'perm.auto'], ['allow', 'perm.allow'], ['deny', 'perm.deny']];
// The options, in the language that is loaded NOW.
//
// Split out because the select is built once and the pack arrives whenever it arrives: measured
// with a live daemon, the card drew before the fetch landed and every option read as its own key —
// "perm.allow" — for the rest of the session, because nothing came back to rewrite an element that
// is deliberately never rebuilt. So paint() calls this on a pack change, the way it repaints every
// other select whose labels are words.
function paintPerm(sel) {
  const want = sel.value;
  sel.replaceChildren(...PERM_MODES.map(([m, key]) => {
    const o = document.createElement('md-select-option');
    o.value = m;
    o.append(cell('', tr(key)));
    return o;
  }));
  if (want) {
    sel.value = want;
    if (sel.updateComplete) sel.updateComplete.then(() => { sel.value = want; });
  }
}

function permField(a) {
  const f = cell('f');
  f.append(cell('k', tr('field.permission')));
  const v = cell('v');
  let sel = permField.el;
  if (!sel) {
    sel = permField.el = document.createElement('md-outlined-select');
    sel.className = 'permsel';
    paintPerm(sel);
    sel.addEventListener('change', async () => {
      const want = sel.value;
      // Said by the daemon, not assumed by the page: the next poll paints whatever it reports, so
      // a refused change reverts visibly instead of leaving the console claiming a mode nobody is
      // on. Kept as the pending value until then so the poll in between does not fight the click.
      permField.want = want;
      const why = await post('/permission', new URLSearchParams({mode: want}), a.socket, a.peer);
      permField.want = '';
      if (!why) loadFleet();
    });
  }
  const now = permField.want || a.permission || '';
  if (now && sel.value !== now && document.activeElement !== sel) {
    sel.value = now;
    if (sel.updateComplete) sel.updateComplete.then(() => { sel.value = now; });
  }
  v.append(sel);
  f.append(v);
  return f;
}

// modelField is which model this companion is on, and the way to put it on another.
//
// Built like the approval field beside it, and for the same reasons: the select is kept between
// redraws so a poll landing while the menu is open does not shut it, the pending value is held
// until the daemon's next answer so the poll does not fight the click, and what is drawn is what
// the daemon SAYS it is on — a refused change reverts visibly rather than leaving the console
// claiming a model nobody is running.
//
// The options come from the companion's own daemon, once per screen. A console listing from its
// own config would offer models that companion cannot reach; a daemon too old to answer, or a
// backend that is down, leaves the list empty and the field is then the plain reading it was
// before — the choice is not offered rather than offered and broken.
function modelField(a, now) {
  const f = cell('f');
  f.append(cell('k', tr('field.model')));
  const v = cell('v');
  const key = (a.peer || '') + ' ' + a.socket;
  let sel = modelField.el;
  if (!sel || modelField.key !== key) {
    sel = modelField.el = document.createElement('md-outlined-select');
    modelField.key = key;
    modelField.list = null;
    sel.className = 'permsel';
    // No label on the field. The row it sits in already says "model" in the key beside it, and a
    // floating label repeating it is the same word twice, six pixels apart.
    sel.addEventListener('change', async () => {
      const want = sel.value;
      if (!want || want === modelField.now) return;
      modelField.want = want;
      const why = await post('/model', new URLSearchParams({model: want}), a.socket, a.peer);
      modelField.want = '';
      if (!why) loadFleet();
    });
  }
  // The roster, once. Asked on the first draw for this companion and kept: it is a property of the
  // backend, not of the turn, and re-asking it every three seconds would put a network round trip
  // behind a card that redraws on a timer.
  if (modelField.list === null) {
    modelField.list = [];
    fetchList('/model' + qFor(a)).then(names => {
      modelField.list = names || [];
      paintModels(sel, modelField.list, modelField.now || now);
    });
  }
  modelField.now = modelField.want || now;
  paintModels(sel, modelField.list || [], modelField.now);
  v.append(sel);
  f.append(v);
  return f;
}

// paintModels fills the select with what is on offer, plus the one it is on.
//
// The current model is always an option even when the backend did not list it — a companion can be
// running on something the list no longer mentions, and a select that cannot show its own value
// shows the wrong one.
function paintModels(sel, names, now) {
  const want = [...names];
  if (now && !want.includes(now)) want.unshift(now);
  const same = (sel._painted || []).join(' ') === want.join(' ');
  if (!same) {
    sel._painted = want;
    sel.replaceChildren(...want.map(n => {
      const o = document.createElement('md-select-option');
      o.value = n;
      o.append(el('div', n));
      return o;
    }));
  }
  if (now && sel.value !== now && document.activeElement !== sel) {
    sel.value = now;
    if (sel.updateComplete) sel.updateComplete.then(() => { sel.value = now; });
  }
  // One model and nothing to change to is a menu that only wastes a press.
  sel.disabled = want.length < 2;
}

// sessionField is which session this companion is in, and the way to the others.
//
// It was a reading and a button three rows apart: the id here, and "open history" further down
// leading to a list. One control now — the id IS the list, which is the shape a person expects
// from a thing that has other values.
//
// ⚠ Choosing one OPENS it; it does not move the companion into it. Redirecting the work would mean
// addressing a different session on every submit, and that is a change to how this console names
// what it is talking to rather than a change to this control. The rule that guards it is here
// already, so the day that lands nothing else has to move: a companion that is mid-turn cannot be
// pointed somewhere else, and the menu says so by being shut rather than by refusing afterwards.
function sessionField(a) {
  const f = cell('f wide');
  f.append(cell('k', tr('field.session')));
  const v = cell('v');
  let sel = sessionField.el;
  if (!sel || sessionField.key !== a.socket) {
    sel = sessionField.el = document.createElement('md-outlined-select');
    sessionField.key = a.socket;
    sessionField.list = null;
    sel.className = 'permsel';
    sel.addEventListener('change', () => {
      const want = sel.value;
      if (want && want !== a.session) goDeep('past', want);
    });
  }
  // Idle only. A turn in flight is a turn in THIS session, and a control that offers to leave it
  // while it is running is offering something it cannot honour.
  const idle = a.state === 'idle' || a.state === 'stopped';
  sel.disabled = !idle;
  tip(sel, idle ? tr('hint.session_pick') : tr('hint.session_busy'));
  if (sessionField.list === null) {
    sessionField.list = [];
    fetchList('/history' + qFor(a)).then(list => {
      sessionField.list = list || [];
      paintSessions(sel, sessionField.list, a.session);
    });
  }
  paintSessions(sel, sessionField.list || [], a.session);
  v.append(sel);
  f.append(v);
  return f;
}

// paintSessions fills the menu with what this workspace has run, newest first, and always with the
// one it is in — a session the list has not caught up with is still the session on screen.
function paintSessions(sel, list, now) {
  const rows = (list || []).slice();
  if (now && !rows.some(h => h.id === now)) rows.unshift({id: now, title: '', current: true});
  const want = rows.map(h => h.id).join(' ');
  if (sel._painted !== want) {
    sel._painted = want;
    sel.replaceChildren(...rows.map(h => {
      const o = document.createElement('md-select-option');
      o.value = h.id;
      // The id, then what the work was — an id alone is a menu of hashes, and a title alone puts
      // two identical-looking lines in front of somebody choosing between them.
      o.append(el('div', h.id + (h.title ? ' · ' + oneLine(h.title, 48) : '')));
      return o;
    }));
  }
  if (now && sel.value !== now && document.activeElement !== sel) {
    sel.value = now;
    if (sel.updateComplete) sel.updateComplete.then(() => { sel.value = now; });
  }
}

function drawDetail(a) {
  const box = document.getElementById('detail');
  if (!a) { box.hidden = true; box.replaceChildren(); return; }
  // Takes a KEY, not a word. Every label in this panel was written in English here while the pack
  // carried a translation for it, and the panel is the one screen that answers "what am I looking
  // at" — the last place that should be answering it in a language the reader did not pick.
  const field = (key, v, cls) => {
    const f = cell('f'); f.append(cell('k', tr(key)), cell('v ' + (cls || ''), v)); return f;
  };
  const grid = cell('grid');
  // The order answers questions in the order somebody asks them, and the grid packs in DOM order —
  // so this list IS the layout.
  //   1. what is it doing right now:      state, steps, last activity
  //   2. who and where it is:             role, team, host, workspace
  //   3. how you move around it:          session
  // and then, appended below, how it runs: approvals, model, cache, context — the two that are
  // controls sit together rather than one at the top of the card and one at the bottom.
  //
  // Wide fields span two columns rather than the whole row: a full-row span breaks the packing on
  // both sides and the card grew three near-empty rows, one of them holding a five-letter state.
  const wide = f => { f.className = 'f wide'; return f; };
  grid.append(
    field('field.status', stateWord(a.state), 'state ' + a.state),
    field('field.steps', a.steps ? a.steps + '' : '—'),
    field('field.last_activity', ago(a.idle)),
    ...(a.role ? [wide(field('field.role', a.role))] : []),
    ...(a.team ? [field('field.team', a.team + (a.hub ? ' · ' + tr('team.speaks') : ''))] : []),
    field('field.host', (a.host || 'this machine') + (a.addr ? ' · ' + a.addr : '') +
                  (a.pid ? ' · pid ' + a.pid : '')),
    wide(field('field.workspace', a.workdir)),
    sessionField(a),
  );
  grid.append(permField(a));
  // A button, not a clickable div: this is the one control on the card and it has to be reachable
  // by keyboard and announce itself as pressed or not.
  const bar = document.createElement('button');
  bar.type = 'button';
  bar.className = 'foldbar hit48';
  bar.append(cell('caret', '▾'), cell('k', tr('field.facts')),
             (() => {
               // The same rule as the status line: it ellipses, so it has to be readable in full
               // somewhere. The workdir is the part that gets cut and the part somebody is looking
               // for.
               const sum = cell('sum', stateWord(a.state) + ' · ' + a.workdir);
               tip(sum, stateWord(a.state) + ' · ' + a.workdir);
               return sum;
             })());
  bar.onclick = () => setFolded(!box.hasAttribute('folded'));
  // The facts sit in a wrapper of their own so folding can be a movement: a grid row going from
  // 1fr to 0fr is the one way to transition to content height without hard-coding what that height
  // is, and it needs a box around the thing being collapsed.
  //
  // The wrapper OUTLIVES the redraw, and that is what makes the movement possible at all. This card
  // is rebuilt on every fleet poll — every three seconds — and a box created in this frame has no
  // previous style to transition from, so a fold landing near a rebuild simply jumped. Measured:
  // no transition on the element at all. Kept, it is the same box each time and only its contents
  // are replaced.
  const wrap = drawDetail.wrap || (drawDetail.wrap = cell('foldwrap'));
  wrap.replaceChildren(grid);
  box.replaceChildren(bar, wrap);
  // What it can do and how the run is shaped — the two things the terminal answers with /tools and
  // /loop, which had no way in here at all. Buttons in the facts card rather than rows in the
  // transcript: they are answers to a question somebody asked, not a record of what happened, and
  // the transcript is already the one place where those two kinds of thing get mixed.
  {
    const row = cell('f');
    row.append(cell('k', tr('field.what_it_has')));
    const v = cell('v');
    for (const [key, label] of [['tools', tr('insp.tools')], ['loop', tr('insp.loop')]]) {
      const b = el('button', label);
      b.type = 'button';
      b.className = 'deeper hit48';
      b.onclick = () => goDeep('insp', key);
      v.append(b);
    }
    row.append(v);
    grid.append(row);
  }
  // Children the turn spawned, reached from the facts rather than from the transcript. A child is
  // started inside a tool call and finishes inside the same one, so the transcript row that
  // produced it says "spawn" and nothing about what came back — there was no way in at all.
  //
  // Only when there are some: a button that leads to an empty list is a button that teaches people
  // not to press it.
  fetchList('/subagents' + qFor(a)).then(kids => {
    if (!kids || !kids.length) return;
    const row = cell('f');
    row.append(cell('k', tr('detail.subagents')));
    const v = cell('v');
    for (const k of kids) {
      const b = el('button', (k.role || tr('detail.subagent')) + ' · ' + oneLine(k.task || '', 40));
      b.type = 'button';
      b.className = 'deeper hit48';
      b.onclick = () => goDeep('sub', k.id);
      v.append(b);
    }
    row.append(v);
    grid.append(row);
  });
  setFolded(localStorage.getItem('facts') === 'folded');
  box.hidden = false;
  // Which of the two panels it belongs to when the columns have stacked. Called here rather than
  // left to render(), because this runs on every fleet poll and render() does not.
  if (sock()) drawPanels();
  drawPlan(a);
  drawHandoffs(a);
  drawCron(a);
  // Returned rather than dropped: the caller does not wait for it, but a caller that WANTS to —
  // a test, or a later screen that needs the whole panel settled — has no other way to know when
  // the slow half landed, and a promise nobody can await is a promise nobody can check.
  return drawContext(a, box, grid, field);
}

// ── one level in: a council seat, or a child the turn spawned ────────────────
//
// The terminal has had this since it had a council: a verdict or a subagent pane opens into a
// screen of its own. Here the same row folded open to show the one line that was already on the
// transcript, so pressing it led back to what had just been read.
//
// One screen and two sources, because the question is the same for both. Who was this, what were
// they asked, what did they SEE, and what did they come back with — the last two being the halves
// that turn a verdict from something to weigh into something to check.

const detailEl2 = () => document.getElementById('agentdetail');

// head builds the detail's own heading: who, and what they concluded.
function detailHead(title, hue, chip, mark) {
  const h = cell('dhead');
  const who = cell('dwho');
  // The mark first, as a drawing when this build has one and as the glyph it has always been when
  // it does not. Two nodes rather than one string, because an icon is not a character: it takes
  // the line's colour, scales with the text, and is hidden from a reader who is being read to —
  // the heading already says in words what the mark says in shape.
  if (mark) {
    const m = iconOr(mark[0], mark[1], 'hic');
    if (m) who.append(m, document.createTextNode(' '));
  }
  who.append(document.createTextNode(title));
  if (hue) who.style.color = hue;
  h.append(who);
  if (chip) h.append(cell('dchip', chip));
  return h;
}

// section is a labelled block of the detail: a heading nobody has to guess at, and the body under
// it. Empty bodies are skipped rather than drawn as a heading over nothing.
function detailSection(box, label, body, opts) {
  if (!String(body || '').trim()) return;
  box.append(cell('dk', label));
  const b = cell('dbody');
  if (opts && opts.pre) { const p = el('pre'); p.textContent = body; b.append(p); }
  else md(b, String(body));
  box.append(b);
}

// drawVerdict is one council member's vote, and the material the round was shown.
//
// The evidence comes first: a vote read before what it judged can only be believed, and read after
// it can be checked. That ordering is the terminal's and it is the point of the screen.
async function drawVerdict(a, spec) {
  const box = detailEl2();
  const [roundStr, ...rest] = String(spec).split(':');
  const round = parseInt(roundStr, 10) || 0;
  const member = rest.join(':');
  // From the transcript already loaded rather than fetched again: these rows are the ones on
  // screen, and asking the server for a copy would be a second answer that can differ from what
  // the reader just pressed.
  //
  // The LAST vote of that seat in that round, not the first. A member votes twice when a rebuttal
  // round changes its mind, and both are in the transcript — reading the earlier one put a
  // rejection behind a row that said the opposite.
  // From whichever transcript is on screen: the live one, or the past session this verdict was
  // read in. Asked of the live rows while standing in a finished session, this found nothing and
  // drew a header with no vote under it.
  const from = pastOf() ? pastRows : lastRows;
  const seats = (from || []).filter(r => r.who === 'council' && r.round === round
    && String(r.member || '') === member);
  const v = seats[seats.length - 1] || {};
  // The seat's own colour, taken from the same token the transcript row uses so the two cannot
  // drift. Only for the three named seats: a custom member gets the default, rather than a lookup
  // of a token named after whatever the log said.
  const seat = COUNCIL_SEATS[member.toLowerCase()];
  const hue = seat ? 'var(--magi-ref-' + member.toLowerCase() + ')' : '';
  box.replaceChildren();
  box.append(detailHead((member || tr('council.outcome')), hue,
    (v.decision ? councilWordOf(v.decision) : '') + (v.lens ? ' · ' + v.lens : '')
    + (v.confidence ? ' · ' + Math.round(v.confidence * 100) + '%' : ''),
    ['#i-sl-scale-balanced', '\u2696']));

  const ev = await fetchOne('/council' + qFor(a) + '&round=' + round
    + (pastOf() ? '&session=' + encodeURIComponent(pastOf()) : ''));
  if (ev) {
    const seen = cell('dseen');
    detailSection(seen, tr('detail.task'), ev.task);
    detailSection(seen, tr('detail.plan'), ev.plan);
    detailSection(seen, tr('detail.report'), ev.report);
    detailSection(seen, tr('detail.actions'), ev.actions, {pre: true});
    detailSection(seen, tr('detail.changes'), ev.changes, {pre: true});
    if (ev.noChanges) seen.append(cell('dnote', tr('detail.no_changes')));
    if (seen.children.length) {
      box.append(cell('dk dhero', tr('detail.evidence')));
      box.append(seen);
    }
  } else {
    // Said, rather than left as a gap. A round whose convening was compacted away is a round whose
    // evidence is genuinely gone, and a screen that simply omits it reads as one that failed to
    // load — somebody presses again, and again. The vote below is still worth the trip.
    box.append(cell('dk dhero', tr('detail.evidence')));
    box.append(cell('dnote', tr('detail.evidence_gone')));
  }
  detailSection(box, tr('detail.rationale'), v.why);
  detailSection(box, tr('detail.next'), v.feedback);
  // What a revision must NOT lose. It is produced, recorded per member and injected back into the
  // model — and had no rendering here at all, so the one instruction protecting finished work was
  // the only part of a verdict nobody could read.
  detailSection(box, tr('detail.keep'), v.keep);
  // The grounds, including the two ways there are none: a member saying plainly it judged the
  // report's substance, and a member that did not answer. A "done" standing on nothing is a fact
  // about that vote and the empty case is the one worth seeing most.
  box.append(cell('dk', tr('detail.grounds')));
  box.append(cell('dbody', !String(v.cite || '').trim() ? tr('detail.no_grounds')
    : (v.cite.trim().toUpperCase() === 'NO-EVIDENCE' ? tr('detail.judged_on_report') : '"' + v.cite + '"')));
}

// drawChild is one spawned subagent: what it was, what it was asked, and its own transcript.
async function drawChild(a, id) {
  const box = detailEl2();
  const list = await fetchList('/subagents' + qFor(a));
  const me = (list || []).find(x => x.id === id) || {id: id};
  box.replaceChildren();
  box.append(detailHead((me.role || tr('detail.subagent')), '',
    (me.running ? tr('detail.running') : tr('detail.finished')) + (me.model ? ' · ' + me.model : ''),
    ['#i-sl-diamond', '\u25C6']));
  detailSection(box, tr('detail.asked'), me.task);
  // Its own transcript, built by the same code that builds the parent's, so a child reads the way
  // everything else on this page reads instead of like a second rendering of the same log.
  const rows = await fetchList('/transcript' + qFor(a) + '&session=' + encodeURIComponent(id));
  const log2 = cell('dlog');
  for (const r of (rows || [])) log2.append(rowNode(r));
  if (!log2.children.length) log2.append(cell('dnote', tr('detail.nothing_yet')));
  box.append(cell('dk dhero', tr('detail.what_it_did')));
  box.append(log2);
}

// ── what it can do, and the shape of the run ─────────────────────────────────
// The terminal prints both of these into the conversation, because a terminal has nowhere else to
// put them. Here they are screens: a transcript is a record of what happened, and a list somebody
// asked to see is not that.

// drawTools is the roster this companion is running with.
//
// Asked of the daemon, never assembled here. The registry is built at startup from the config, the
// plugins that loaded and the MCP servers that answered — a console listing the built-ins would be
// describing a companion that does not exist, and would be most confidently wrong exactly when it
// mattered, on the one whose plugin failed to load.
async function drawTools(a) {
  const box = detailEl2();
  const names = await fetchList('/tools' + qFor(a));
  box.replaceChildren();
  box.append(detailHead(tr('insp.tools'), '', names && names.length ? names.length + '' : '',
    ['#i-sl-screwdriver-wrench', '\u{1F6E0}']));
  if (!names || !names.length) {
    // Not "no tools". A companion always has some; what an empty answer means is that this daemon
    // is too old to be asked, and saying the other thing would be a screen inventing a fact.
    box.append(cell('dnote', tr('insp.tools_unknown')));
    return;
  }
  const list = cell('dlog');
  for (const n of names) {
    const row = cell('f');
    row.append(cell('k', n));
    list.append(row);
  }
  box.append(cell('dk dhero', tr('insp.tools_have')), list);
}

// drawLoop is the map of the turns, and — when this session was forked from another — what has
// changed since it left.
async function drawLoop(a) {
  const box = detailEl2();
  const shape = await fetchOne('/loop' + qFor(a));
  box.replaceChildren();
  box.append(detailHead(tr('insp.loop'), '', shape && shape.origin ? tr('insp.forked') : '',
    ['#i-sl-arrows-rotate', '\u21BB']));
  if (!shape) { box.append(cell('dnote', tr('error.unreachable'))); return; }
  // Preformatted, because the map IS its alignment: the same text with the spaces collapsed is a
  // paragraph of step numbers.
  detailSection(box, tr('insp.loop_map'), shape.map, {pre: true});
  if (!String(shape.map || '').trim()) box.append(cell('dnote', tr('detail.nothing_yet')));
  if (shape.origin) {
    detailSection(box, tr('insp.forked_from'), shape.origin);
    detailSection(box, tr('insp.since_fork'), shape.diff, {pre: true});
  }
}

// drawDeep decides which of the two is open, and says so in the crumb.
async function drawDeep(a) {
  const box = detailEl2();
  box.replaceChildren(cell('dnote', tr('detail.loading')));
  try {
    if (inspOf() === 'tools') await drawTools(a);
    else if (inspOf() === 'loop') await drawLoop(a);
    else if (askOf()) await drawAsk(a);
    else if (crOf()) await drawVerdict(a, crOf());
    else if (pastOn()) await drawPast(a);
    else await drawChild(a, subOf());
  } catch (e) {
    box.replaceChildren(cell('dnote', tr('error.unreachable')));
  }
  reveal(box);
}

// drawAsk is the decision, at the width a report needs.
//
// The bar above the composer is the right shape for a small one — "run this command?" is a line and
// two buttons. It is the wrong shape for the case this exists for: an agent that worked for an hour
// while nobody watched, and now asks something whose answer depends on what it found. That is three
// sections of prose, and a strip at the bottom of a transcript is where prose goes to be skipped.
//
// The prompt is NOT in the log — it is a question about what should happen, not a record of what
// did — so it is read from the fleet poll, the same place the bar reads it from. Which also means
// the screen empties itself when the question is answered from anywhere: the next poll has no
// prompt in it, and this returns to the conversation rather than showing a question nobody can
// answer any more.
async function drawAsk(a) {
  const box = detailEl2();
  const list = await fetchList('/fleet');
  const mine = (list || []).find(x => x.socket === a.socket && (x.peer || '') === (a.peer || ''));
  if (!mine || mine.state !== 'waiting' || mine.askId !== askOf()) {
    // Answered, or moved on. Back to the conversation rather than a dead screen — and emptied
    // first, because leaving the last question on screen while navigating away is a flash of a
    // decision that is no longer anybody's to make.
    box.replaceChildren();
    goDeep('ask', null);
    return;
  }
  box.replaceChildren();
  box.append(detailHead((mine.asking || ''), '',
    (mine.askKind === 'question' ? tr('ask.question') : tr('ask.permission'))
    + (mine.askTotal > 1 ? ' · ' + tr('ask.of', {i: mine.askIndex, n: mine.askTotal}) : ''),
    ['#i-sl-circle-pause', '\u23F8']));
  const why = grounds(mine);
  if (why) {
    box.append(cell('dk dhero', tr('detail.evidence_decide')));
    box.append(why);
  }
  box.append(cell('dk', tr('ask.your_answer')));
  const act = cell('askact');
  act.append(answerBox(mine));
  box.append(act);
}

// render$past redraws the past screen in place, for the search field's debounce. Named apart from
// render() because that one decides which screen is up; this one refreshes the screen already on.
async function render$past() {
  const s2 = sock();
  if (!s2 || !pastOn()) return;
  const known = (fleetSeen || []).find(x => x.socket === s2 && (x.peer || '') === peerOf());
  await drawPast(known || {socket: s2, peer: peerOf()});
}

// drawPast is what this companion has done before now: the list, or one of them opened.
//
// One level in rather than in the pane. The pane stays open, so what is in it has to be worth the
// width all the time — the plan, the queue and what was handed out all move while you watch. A list
// of finished sessions does not: you go and look at it, and while you are looking at it that is
// the screen you want, not a column beside something else.
async function drawPast(a) {
  const box = detailEl2();
  const want = pastOf();
  if (want) {
    const rows = await fetchList('/transcript' + qFor(a) + '&session=' + encodeURIComponent(want));
    // Kept, because a verdict opened from one of these rows is read out of the same list — asking
    // the server again would be a second answer that can differ from what is on screen.
    pastRows = rows || [];
    box.replaceChildren();
    box.append(detailHead(tr('field.history'), '', want, ['#i-sl-clock-rotate-left', '']));
    const log2 = cell('dlog');
    for (const r of (rows || [])) log2.append(rowNode(r));
    if (!log2.children.length) log2.append(cell('dnote', tr('detail.nothing_yet')));
    box.append(log2);
    return;
  }
  const list = await fetchList('/history' + qFor(a));
  box.replaceChildren();
  box.append(detailHead(tr('field.history'), '', list ? String(list.length) : '',
    ['#i-sl-clock-rotate-left', '']));
  box.append(findField((list || []).length));
  if (findQuery) { await drawFound(box, a); return; }
  for (const h of (list || [])) {
    // A row that opens. The list answers "what has it been doing"; the session behind a row
    // answers "what happened in that one", and until now there was no way to ask the second.
    const row = el('button');
    row.type = 'button';
    row.className = 'hs hit48' + (h.current ? ' now' : '');
    row.append(cell('when', h.current ? tr('state.working') : ago(h.ago)));
    row.append(cell('what', h.title || tr('history.untitled')));
    row.onclick = () => goDeep('past', h.id);
    box.append(row);
  }
  if (!(list || []).length) box.append(cell('dnote', tr('find.none')));
}

// ── the plan it is working through ───────────────────────────────────────────
// The agent's own todo list, as it last recorded it. Shown as it is: an item it dropped is gone,
// because the record is the whole plan each time and merging would resurrect what it decided
// against.
// planRows draws a plan as ticked lines. Shared by the panel and by the transcript row that
// WROTE the plan, so the two cannot come to disagree about what it says.
//
// completed | in_progress | pending, which is the todo tool's whole enum. A branch for 'done' sat
// here and a .td.done rule sat in the stylesheet, both waiting on a value the schema forbids.
// The mark for a plan step: done, doing, not yet. A node rather than a character, so the tick can
// be the drawn one where this build has icons — and the same character it always was where it does
// not. Pending stays a middle dot in both: there is no drawing for "nothing has happened here" that
// beats a dot, and a circle outline reads as a control somebody could press.
function planMark(t) {
  if (t.status === 'completed') return iconOr('#i-sl-check', '\u2713', 'mk');
  if (t.status === 'in_progress') return iconOr('#i-sl-chevron-right', '\u25B8', 'mk');
  return el('span', '\u00B7');
}

// One step, one row. Written once and used by both the card and the transcript's own copy of the
// plan: the card had its own second copy of these four lines, and the day the mark became a node
// that copy kept passing it where a string goes and drew "[object Object]" three times.
function planRow(t) {
  const row = cell('td ' + (t.status || ''));
  const mk = cell('mark');
  mk.append(planMark(t));
  row.append(mk, cell('what', t.content));
  return row;
}

function planRows(todos) {
  const box = cell('plan');
  box.append(...todos.map(planRow));
  return box;
}

async function drawPlan(a) {
  const box = document.getElementById('plan');
  const todos = await fetchList('/plan' + qFor(a));
  if (!todos || !todos.length) { box.hidden = true; box.replaceChildren(); return; }
  // How much of the plan is behind it. Determinate, because the counts are known — an
  // indeterminate bar here would say "something is happening" to somebody who can already see
  // exactly what is happening in the list below it.
  const done = todos.filter(t => t.status === 'completed').length;
  const pct = Math.round(done / todos.length * 100);
  // The library's own, not one drawn here. It was a div with a width — which meant reimplementing
  // the stop indicator, the role, and every state by hand. md-linear-progress is in the stable half
  // of Material Web and ships all three.
  const bar = document.createElement('md-linear-progress');
  bar.value = done / todos.length;
  // Named for what it is measuring, not "loading": a progress bar that only says "progress" tells
  // a screen reader nothing the number did not already say.
  bar.setAttribute('aria-label', tr('plan.progress', {done: done, total: todos.length}));
  bar.className = 'planbar';
  box.replaceChildren(cell('k', tr('field.plan')), bar,
    cell('plancount', tr('plan.progress', {done: done, total: todos.length})),
    ...todos.map(planRow));
  box.hidden = false;
}

// ── what it handed to the others ─────────────────────────────────────────────
// A companion answers in its own transcript and the asker reads it — cheap and honest, and it
// leaves somebody clicking through five pages to find out whether the work is done. This is that
// walk, done once, under the transcript of whoever handed it out.
async function drawHandoffs(a) {
  const box = document.getElementById('handoffs');
  const list = await fetchList('/handoffs' + qFor(a));
  if (!list || !list.length) { box.hidden = true; box.replaceChildren(); return; }
  const rows = list.map(h => {
    // `row`, not `el`: this scope used to call its row el, which shadows the page's own el() —
    // the moment the name became a link the row threw "el is not a function" and the whole card
    // vanished, on every console where the companion named was one this one can see.
    const row = cell('ho ' + h.state);
    // The name is a way to the companion it names, when this console can see one by that name.
    // Handed-out work is the one place on this page that talks about somebody who is not on the
    // screen, and the answer to "what is it doing with this" lives on their page.
    //
    // Only when it is known: an anchor to nowhere is worse than plain text, and a companion named
    // here may be on a machine this console has never been told about.
    const peer = (fleetSeen || []).find(x => x.name === h.to);
    let to = cell('to', h.to);
    if (peer && peer.socket) {
      to = el('a', h.to);
      to.className = 'to';
      to.setAttribute('href', at('?d=' + encodeURIComponent(peer.socket) +
        (peer.peer ? '&p=' + encodeURIComponent(peer.peer) : '')));
    }
    row.append(to, cell('req', h.request));
    // The answer only when the work is over. Anything else would be reporting a sentence
    // mid-thought as a conclusion.
    row.append(cell('ans', h.answer ? h.answer : 'still ' + h.state));
    return row;
  });
  box.replaceChildren(cell('k', tr('field.handed_out')), ...rows);
  box.hidden = false;
}

// ── what it does when nobody is watching ─────────────────────────────────────
//
// A job belongs to one workspace: the daemon holding it is the only thing that runs it. So it sits
// with this companion's other standing facts rather than in a destination of its own — and past
// 840px there are no tabs, both panes are drawn, so a third tab would have been invisible on every
// desktop anyway.
//
// The agent can schedule its own work now. That makes this the answer to "what did you leave
// running?", which is a question with no answer at all until it is on a screen.
async function drawCron(a) {
  const box = document.getElementById('cron');
  const list = await fetchList('/cron' + qFor(a));
  if (!list || !list.length) { box.hidden = true; box.replaceChildren(); return; }
  const rows = list.map(j => {
    const el = cell('job' + (j.enabled ? '' : ' off') + (j.problem ? ' broken' : ''));
    el.append(cell('jname', j.name), cell('jwhen', j.schedule));
    // When it next runs, or why it never will. A job that can never run is the one this list MUST
    // mark: it is switched on, it looks ordinary, and nothing else will ever mention it again.
    let state;
    if (j.problem) state = tr('cron.never') + ' — ' + j.problem;
    else if (!j.enabled) state = tr('cron.off');
    else if (j.next) state = tr('cron.next') + ' ' + new Date(j.next).toLocaleString();
    else state = tr('cron.never');
    el.append(cell('jnext', state));
    el.append(cell('jask', j.prompt));
    // Where it is written, so somebody can go and look — and which file, because an edit here
    // always writes the workspace's and a machine-wide job cannot be removed from this page.
    el.append(cell('jfile', j.file + (j.global ? ' · ' + tr('cron.machine') : '')));
    return el;
  });
  box.replaceChildren(cell('k', tr('field.scheduled')), ...rows);
  box.hidden = false;
}

// ── what is in its head ──────────────────────────────────────────────────────
// Fetched after the rest of the detail is already on screen, and appended when it lands: it costs a
// replay of the whole log, and the six facts above it are the ones somebody opened this page for.
//
// # Asked again only when the answer can have changed
//
// The detail panel is redrawn by every fleet poll — every three seconds, for as long as the tab is
// open. Asking for the context each time would replay the entire log each time, which is precisely
// the cost fleet.Cache exists to avoid paying per row per poll, reintroduced one layer up. The
// answer can only have moved if the transcript did, and the fleet row already carries that: steps.
// So the fetch is keyed on (companion, steps) and an idle companion is asked exactly once.
// Held rather than re-fetched: the panel is rebuilt from scratch on every redraw, so a cached
// answer has to be re-RENDERED even when it is not re-asked for. Skipping the render as well was
// the first version of this and it left the panel a field short every three seconds.
let ctxHeld = {key: '', data: null};
let ctxDraw = 0; // which redraw a pending answer belongs to
// Steps AND state. Steps alone misses the end of a turn: the finish writes the provider's real
// prompt count into the log without adding a tool call, so a panel keyed on steps alone would go
// on showing its estimate — labelled "estimated" — for as long as the companion then sat idle.
function contextKey(a) {
  return (a.peer || '') + '\u0000' + a.socket + '\u0000' + (a.steps || 0) + '\u0000' + a.state;
}

async function drawContext(a, box, grid, field) {
  const key = contextKey(a);
  let c = ctxHeld.key === key ? ctxHeld.data : null;
  if (!c) {
    const mine = ++ctxDraw;
    try { c = await (await fetch('/context' + qFor(a))).json(); }
    catch { return; } // the page is still correct without it; a failed extra must not blank the rest
    // A slower answer must not land on a panel that has since been redrawn: two polls overlap the
    // moment one of them is slow, and the late one would append a second copy of every field below
    // — or worse, older numbers under newer ones.
    if (mine !== ctxDraw) return;
    ctxHeld = {key: key, data: c};
  }
  // The panel it was drawn into may have been replaced while this was in flight. Checked on the
  // box that was passed in rather than looked up: on the stacked layout the card is legitimately
  // hidden behind the other tab, and looking it up would drop the context every time somebody was
  // reading the conversation.
  //
  // ⚠ Asked of the GRID, not of the box's children list. box.children.indexOf(...) was written
  // against the fake DOM, where children is an array — in a browser it is an HTMLCollection with no
  // indexOf, so this threw, the async function rejected with nobody awaiting it, and the whole
  // context block silently stopped rendering. The fake disagreed with the DOM in the direction that
  // makes a test pass and a page fail, which is the fourth time it has done that.
  //
  // Walked up rather than compared to the parent: the facts sit inside a wrapper now (so folding
  // can animate), and a one-level check quietly became false — which is this same failure again,
  // a guard that stops the panel drawing because the box it was written against moved.
  const inside = n => {
    for (let p = n; p; p = p.parentNode) {
      if (p === box) return true;
    }
    return false;
  };
  if (!c || !inside(grid)) return;

  // Which model, because the window below is that model's and a companion can be on one you did
  // not put it on — /route changes it mid-session and nothing else on this page would say so.
  //
  // And a way to change it, which the terminal has had as /model since it had a slash command. The
  // list is asked of the companion's own daemon, so it offers what THAT process can reach; when
  // nobody could say, the field stays the plain reading it always was.
  if (c.model) grid.append(modelField(a, c.model));
  // Said once, where somebody would otherwise wonder why there is no cache figure at all.
  if (!c.cacheReported && !c.estimated) {
    grid.append(field('field.cache', tr('context.no_cache_report')));
  }

  const size = cell('v', '');
  size.append(document.createTextNode(
    (c.estimated ? '~' : '') + (c.used || 0).toLocaleString() +
    (c.window ? ' / ' + c.window.toLocaleString() : '') + ' tokens'));
  const note = document.createElement('small');
  // Said plainly, because the difference decides what the number is worth: one is the provider's
  // own count from the last turn, the other is arithmetic over the transcript.
  note.textContent = ' ' + tr(c.estimated ? 'context.estimated' : 'context.measured') +
                     (c.messages ? ' · ' + tr('context.messages', {n: c.messages}) : '');
  // What the backend served from its own prompt cache — and only when it said. A backend that
  // reports nothing about a cache is not a backend whose cache never hits, and drawing 0% for both
  // would report a working one as broken. Measured on the default local backend: it says nothing.
  if (c.cacheReported) {
    const share = c.used ? Math.round((c.cached || 0) * 100 / c.used) : 0;
    note.textContent += ' · ' + tr('context.cached_share', {pct: share});
  }
  size.append(note);
  const f = cell('f');
  f.append(cell('k', tr('field.context')), size);
  // The lever beside the reading. magi folds by itself when the window fills past its ratio; this
  // is for the case that rule does not cover — somebody who can see the run is about to need room
  // and would rather it happened now, between turns, than in the middle of the next one.
  const fold = document.createElement('md-text-button');
  fold.className = 'fold'; fold.textContent = tr('action.compact_now');
  withMark(fold, '#i-sl-compress');
  tip(fold, tr('hint.compact'));
  // Returns its promise, for the same reason drawDetail does: a caller that wants to know when the
  // fold has landed — a test, or a later screen — has no other way, and the held reading must be
  // dropped before anything redraws or the panel keeps showing pre-fold numbers.
  fold.onclick = () => {
    fold.disabled = true;
    return post('/compact', null, a.socket, a.peer).then(() => {
      ctxHeld = {key: '', data: null};
      return loadFleet();
    });
  };
  // The bar goes under the number it is a picture of, and the lever goes after both. Ordered the
  // other way the control sat between a reading and its own gauge, so the eye crossed a button to
  // get from "108,000 / 128,000" to the line showing how full that is.
  if (c.window) {
    const pct = Math.min(100, Math.round((c.used || 0) * 100 / c.window));
    const bar = cell('bar' + (pct >= 80 ? ' tight' : ''));
    const fill = document.createElement('i');
    fill.style.width = pct + '%';
    bar.append(fill);
    f.append(bar);
  }
  f.append(fold);
  grid.append(f);

  // A compaction is the one moment a companion silently stops knowing something. Four of them in
  // one session is the reason its earlier reasoning cannot be assumed still there.
  if (c.compactions) {
    const v = cell('v', c.compactions === 1 ? tr('context.fold')
                                       : tr('context.folds', {n: c.compactions}));
    const s2 = document.createElement('small');
    s2.textContent = ' · ' + tr('context.shed', {n: (c.shed || 0).toLocaleString()}) +
                     (c.lastBefore ? ' · last ' + c.lastBefore.toLocaleString() + '→' + c.lastAfter.toLocaleString() : '') +
                     (c.lastAt ? ' at ' + c.lastAt.slice(11, 16) + 'Z' : '');
    v.append(s2);
    const cf = cell('f');
    cf.append(cell('k', tr('field.summarised_away')), v);
    if (c.topics && c.topics.length) {
      // Naming them is the difference between "the detail is not lost" as a claim and as a fact:
      // these are the subjects the companion can pull back in full.
      cf.append(cell('v', c.topics.slice(0, 6).join(' · ') +
                          (c.topics.length > 6 ? ' +' + (c.topics.length - 6) : '')));
    }
    grid.append(cf);
  }
}

// qFor is the query that names one companion: its socket, and the console it lives on.
function qFor(a) {
  const parts = ['d=' + encodeURIComponent(a.socket)];
  if (a.peer) parts.push('p=' + encodeURIComponent(a.peer));
  return '?' + parts.join('&');
}

// ── what I had to step in and say ─────────────────────────────────────────────
// On the companion it is about, and no longer a factory for rules.
//
// This began as a promotion pipeline: group what a person said mid-turn by the words, count the
// repeats, offer to promote the repeated ones into the experience store. The premise does not hold.
// What somebody says mid-turn is nearly always about THAT task — "no, the other file" is not a
// rule — the few that generalise are rare, and the grouping only ever matched identical wording,
// which people do not produce. Above all it needed somebody to visit a screen and curate, and the
// agent's own remember tool already reaches the store without that.
//
// What survives is the part that was always true and never needed the words to match: a count of
// how often this companion had to be corrected, and what was refused. That is a fact about the
// companion, so it belongs on the companion's page.
async function loadIntervened(a) {
  if (!a) { intervenedEl.hidden = true; intervenedEl.replaceChildren(); return; }
  const list = await fetchList('/interventions');
  if (!list) return;
  const mine = list.filter(m => m.socket === a.socket && (m.peer || '') === (a.peer || ''));
  if (!mine.length) { intervenedEl.hidden = true; intervenedEl.replaceChildren(); return; }

  const box = cell('');
  const steers = mine.filter(m => m.kind !== 'denied').length;
  const refused = mine.length - steers;
  const head = cell('k');
  head.textContent = tr('field.intervened') + ' · ' +
    (steers ? tr('iv.steers', {n: steers}) : '') +
    (steers && refused ? ' · ' : '') +
    (refused ? tr('iv.refused', {n: refused}) : '');
  box.append(head);
  for (const m of mine.slice(0, 12)) {
    const row = cell('iv2' + (m.kind === 'denied' ? ' denied' : ''));
    row.append(cell('when', (m.at || '').slice(0, 10)));
    row.append(cell('said', m.kind === 'denied' ? tr('iv.refused_call', {what: m.text}) : m.text));
    box.append(row);
  }
  intervenedEl.replaceChildren(box);
  intervenedEl.hidden = false;
  measureDock();
}

// ── what they have learned ───────────────────────────────────────────────────
// What the organisation shares, and the two things a person does with it: find something, and
// write something down.
//
// # Why the search is here and not only in an agent's hands
//
// A companion can ask another what it knows (magi --mcp). A person had no such thing: the screen
// listed everything in whatever order the tiers came back in, and finding one rule among two
// hundred meant reading two hundred. The same IDF ranking the agents' search uses runs here, over
// the rows already fetched — no request, no round trip, and it narrows as you type.
//
// ⚠ Lexical, not semantic. The agents' search fuses in embeddings when a model is configured;
// this one cannot, because the vectors live where the store is and this page holds only what
// /skills returned. The heading says which model the machine is set up with, since that is the one
// thing a person managing shared knowledge has to keep the same across a team — vectors from two
// models are not comparable, and a search that quietly stopped matching is the symptom.
// One readout for one destination. Two halves load independently and each used to write the whole
// line, so whichever answered second erased the other's count — the shape of every readout built
// by two writers.
const shared = {rules: 0, facts: 0, crossing: 0, servers: null, reachedFrom: 0};
function sayShared() {
  reach(true);
  const bits = [tr(shared.rules === 1 ? 'count.rule' : 'count.rules', {n: shared.rules}),
                tr('count.remembered', {n: shared.facts}),
                tr('count.crossing', {n: shared.crossing})];
  // Null until the servers have answered, which is not the same as none — a line that said "0
  // servers" while the request was in flight would be wrong for as long as it took.
  if (shared.servers !== null) {
    bits.push(tr(shared.servers === 1 ? 'count.server' : 'count.servers', {n: shared.servers}));
  }
  says(bits.join(' · '));
}

let skillQuery = '';
let mcpQuery = '';
// Which model this machine embeds with, from /console. Empty is a real answer, not a missing one.
let embedModel = '';

// The same ranking the agents' search uses, in the page's own words: rare shared words first.
//
// Ported rather than fetched. It is nine lines, it runs over rows already in hand, and asking the
// server would put a round trip between a keystroke and the list narrowing. ⚠ Two implementations
// of one formula is the shape this tree keeps finding defects in — pinned here by a test that
// checks this and internal/core/rank agree on the same corpus.
function rankByIDF(query, docs) {
  const toks = [...new Set(String(query).toLowerCase().split(/[^a-z0-9]+/).filter(w => w.length >= 3))];
  if (!toks.length || !docs.length) return docs.map((_, i) => i);
  const lower = docs.map(d => String(d).toLowerCase());
  const df = Object.fromEntries(toks.map(t => [t, lower.filter(d => d.includes(t)).length]));
  const n = docs.length;
  const hits = [];
  for (let i = 0; i < n; i++) {
    let score = 0, matched = 0;
    for (const t of toks) {
      if (lower[i].includes(t)) {
        score += Math.log(1 + (n - df[t] + 0.5) / (df[t] + 0.5));
        matched++;
      }
    }
    if (matched) hits.push({i: i, score: score, matched: matched});
  }
  hits.sort((a, b) => b.score - a.score || b.matched - a.matched || a.i - b.i);
  return hits.map(h => h.i);
}

// One find box, told which half it belongs to. Written once because two of them written twice is
// two that drift, and the only difference between the halves is where the typed text is kept.
// ── the page's one tooltip ───────────────────────────────────────────────────
// Hover OR focus, which is the half native title= never did: a keyboard user tabbing onto an
// icon-only button saw nothing at all. Placed above by default and flipped below when there is no
// room, 4dp off a control's edge. It leaves 1.5s after the pointer or focus goes, and there is only
// ever one — showing a second closes the first, because two tooltips is two answers to one
// question.
const tipEl = document.getElementById('tip');
let tipTimer = 0, tipHost = null;
function showTip(host) {
  const text = host.getAttribute('data-tip');
  if (!text) return;
  clearTimeout(tipTimer);
  tipTimer = 0;
  tipHost = host;
  tipEl.textContent = text;
  tipEl.hidden = false;
  const r = host.getBoundingClientRect(), t = tipEl.getBoundingClientRect();
  const above = r.top - t.height - 4;
  tipEl.style.top = (above >= 0 ? above : r.bottom + 4) + 'px';
  tipEl.style.left = Math.max(4, Math.min(r.left, innerWidth - t.width - 4)) + 'px';
}
function hideTip() {
  // Idempotent. This is called from pointerout AND from every pointermove that lands off the host,
  // and the first version cleared the pending timer before setting a new one — so moving the mouse
  // rewound the countdown on every frame and the tooltip never left until something else hid it.
  if (tipTimer) return;
  tipTimer = setTimeout(() => { tipEl.hidden = true; tipHost = null; tipTimer = 0; }, 1500);
}
// The tooltip outlived its button. This panel is redrawn on every fleet poll, so a control hovered
// at the wrong moment is REPLACED rather than left — and a node that is gone never fires pointerout,
// so nothing ever asked the tooltip to leave. Two guards: drop it the moment its host is off the
// document, and drop it when the pointer is somewhere else regardless of which events fired.
addEventListener('pointermove', e => {
  if (!tipHost) return;
  if (!tipHost.isConnected || !tipHost.contains(e.target)) hideTip();
}, true);
addEventListener('pointerdown', () => { if (tipHost) { clearTimeout(tipTimer); tipTimer = 0; tipEl.hidden = true; tipHost = null; } }, true);
for (const [on, fn] of [['pointerover', showTip], ['focusin', showTip], ['pointerout', hideTip], ['focusout', hideTip]]) {
  addEventListener(on, e => {
    const host = e.target.closest && e.target.closest('[data-tip]');
    if (host) fn(host); else if (fn === hideTip) hideTip();
  }, true);
}
// Set where title= used to be set. Both, for now: the native one still serves a pointer user on a
// browser that has not run the script yet.
// No title= alongside it. Setting both drew TWO tooltips on the same control — the browser's own,
// which no script can time or place, and this one. setAttribute rather than el.dataset because the
// render test's DOM has no dataset, and a tooltip is not worth teaching it one.
function tip(el, text) { el.setAttribute('data-tip', text); }

// Said to assistive tech, shown to nobody. The list visibly shrinks as you type, which is all the
// feedback a sighted user needs and none of it for anyone else.
const sayEl = document.getElementById('say');
let sayTimer = 0;
// The status line, with a way to read what got cut. It is nowrap-and-ellipsis at narrow widths,
// and the guide is explicit: do not cut text off without giving people a way to see it — a tooltip
// or a link is what it names. Now that this page draws its own tooltips, it can be the tooltip.
function says(text) {
  noteEl.textContent = text;
  if (text) tip(noteEl, text); else noteEl.removeAttribute('data-tip');
}

function say(text) {
  clearTimeout(sayTimer);
  // Cleared first: repeating the same string into a live region is not a change, so the second
  // search that lands on the same count would be silent.
  sayEl.textContent = '';
  sayTimer = setTimeout(() => { sayEl.textContent = text; }, 60);
}

function findBox(get, set) {
  const box = cell('skfind');
  const f = document.createElement('md-outlined-text-field');
  f.setAttribute('label', tr('label.find'));
  f.value = get();
  f.addEventListener('input', () => set(f.value));
  box.append(f);
  return box;
}

// A heading over each half of the shared destination. Two lists under one tab need to say which is
// which, and the destination's own name is now the pair rather than either.
function sectionHead(key, action) {
  const h = document.createElement('h2');
  h.className = 'sectionhead';
  h.append(cell('', tr(key)));
  // A section's own action belongs at its head, not under everything it holds. Adding a server sat
  // at the BOTTOM of the list, so on a console with a dozen of them the way to add one was to
  // scroll past all twelve — and on the screen where the list is empty it was the only control
  // there, below an empty state, which reads as part of the emptiness rather than the way out.
  if (action) h.append(action);
  return h;
}

// The screen's own controls: find something, and write something down.
//
// Rebuilt on every load rather than kept, because the list behind it is — and a box whose value
// survived while the rows under it were replaced is a box that lies about what it is filtering.
// The typed text is held outside, in skillQuery, which is the part that must survive.
const skillFind = () => findBox(() => skillQuery, v => { skillQuery = v; loadSkills(); });
const mcpFind = () => findBox(() => mcpQuery, v => { mcpQuery = v; loadMCP(); });

// Writing goes UNDER what you have read, not over it.
//
// Above the list it was the first thing on a screen whose job is reading what the organisation
// shares — and the first four stops of a keyboard, which had to walk a form to reach the content.
// It also reads better in that order: somebody writes a rule down BECAUSE they just looked for one
// and it was not there.
function skillWrite(all) {
  const box = cell('skwrite');

  // Where it goes. Named tiers rather than "share this": the whole decision on this screen is how
  // far something reaches, and a control that hid it would be deciding on somebody's behalf.
  const where = document.createElement('md-outlined-select');
  where.setAttribute('label', tr('label.reaches'));
  const teams = [...new Set(all.filter(s => s.tier === 'team' && s.team).map(s => s.team))].sort();
  const opts = [['global', tr('reach.every_companion')],
                ...teams.map(t => ['team:' + t, tr('reach.team', {team: t})])];
  for (const [value, label] of opts) {
    const o = document.createElement('md-select-option');
    o.value = value;
    const h = document.createElement('div');
    h.setAttribute('slot', 'headline');
    h.textContent = label;
    o.append(h);
    where.append(o);
  }
  box.append(where);

  const note = document.createElement('md-outlined-text-field');
  note.setAttribute('label', tr('label.write_down'));
  note.setAttribute('type', 'textarea');
  note.setAttribute('rows', '1');
  box.append(note);

  const save = document.createElement('md-filled-button');
  save.textContent = tr('action.write_down');
  withMark(save, '#i-sl-plus');
  // Same as the answer button: disabled while there is nothing to write down, rather than
  // pressable and inert.
  const armSave = () => save.toggleAttribute('disabled', !note.value.trim());
  armSave();
  note.addEventListener('input', armSave);
  save.onclick = () => {
    const v = note.value.trim();
    if (!v) return;
    // The select resolves its value against the option it chose, and the options are custom
    // elements — so it is read here rather than held, and defaulted rather than guessed.
    const pick = where.value || 'global';
    const body = new URLSearchParams({text: v, tier: pick.startsWith('team:') ? 'team' : 'global'});
    if (pick.startsWith('team:')) body.set('team', pick.slice(5));
    post('/remember', body).then(() => { note.value = ''; loadSkills(); });
  };
  box.append(save);

  // Which model the searches on this machine are built on. It belongs on this screen because it is
  // the one setting a person managing shared knowledge has to keep the same across a team: vectors
  // from two models are not comparable, and the symptom is a search that quietly stops matching.
  const model = cell('skmodel');
  model.textContent = tr(embedModel ? 'embed.model' : 'embed.none', {model: embedModel});
  box.append(model);
  return box;
}

async function loadSkills() {
  const list = await fetchList('/skills');
  if (!list) return;
  const crossing = list.filter(s => s.tier === 'global').length;
  const rules = list.filter(s => s.kind !== 'memory').length;
  reach(true);
  shared.rules = rules;
  shared.facts = list.length - rules;
  shared.crossing = crossing;
  sayShared();
  if (!list.length) {
    skillsEl.replaceChildren(sectionHead('nav.lessons'),
      emptyState('empty.nothing_learned', 'empty.nothing_learned_how'), skillWrite(list));
    return;
  }
  // Ranked, not filtered on a substring: "cache" should find the rule about prompt caching before
  // the one that merely mentions caches in passing, and a substring match cannot order anything.
  let shown = list;
  if (skillQuery.trim()) {
    const docs = list.map(sk => [sk.description, sk.name, sk.body, sk.source].filter(Boolean).join(' '));
    const order = rankByIDF(skillQuery, docs);
    shown = order.map(i => list[i]);
  }
  if (!shown.length) {
    skillsEl.replaceChildren(sectionHead('nav.lessons'), skillFind(),
      emptyState('empty.no_match', 'empty.no_match_how'), skillWrite(list));
    return;
  }
  if (skillQuery) say(tr('find.results', {n: shown.length}));
  skillsEl.replaceChildren(sectionHead('nav.lessons'), skillFind(), ...shown.map(sk => {
    const el = cell('sk ' + sk.tier + (sk.kind === 'memory' ? ' fact' : ''));
    const top = cell('top');
    top.append(cell('tier',
      (sk.tier === 'global' ? tr('reach.every_companion')
       : sk.tier === 'team' ? tr('reach.team', {team: sk.team})
       : tr('reach.only', {name: sk.companion})) +
      (sk.peer ? tr('reach.on_peer', {peer: sk.peer}) : '')));
    top.append(cell('what', sk.description || sk.name));
    const drop = document.createElement('md-text-button');
    drop.className = 'drop';
    tip(drop, tr('hint.forget'));
    withMark(drop, '#i-sl-eraser');
    arm(drop, tr('action.forget'), () => {
      // A rule on another console is forgotten THERE. The socket is that machine's path and the
      // peer name is how this one knows which machine to ask; a global rule has no socket and the
      // peer name alone routes it.
      const body = new URLSearchParams({name: sk.name, tier: sk.tier});
      if (sk.tier === 'team') body.set('team', sk.team);
      post('/forget', body,
           sk.tier === 'project' ? sk.socket : null, sk.peer).then(loadSkills);
    });
    top.append(drop);
    el.append(top);
    // A rule tells the companion what to do and a fact tells it what is true. Governed the same
    // way and worth reading differently — a stale fact is wrong, a stale rule is an instruction
    // still being followed.
    const bits = [sk.kind === 'memory' ? 'remembered' : 'rule', sk.name];
    // How settled it is and when it was last seen: the two facts a decision about a rule is made
    // on, and neither is visible anywhere else once the day it was written has passed.
    if (sk.kind !== 'memory' && sk.observed > 1) bits.push('seen ' + sk.observed + '×');
    // Both ends when they differ: "learned three weeks ago and still turning up" is a settled
    // rule, and "learned and last seen the same day" is a one-off that never recurred.
    if (sk.lastSeen && sk.firstSeen && sk.firstSeen !== sk.lastSeen) {
      bits.push(sk.firstSeen + ' → ' + sk.lastSeen);
    } else if (sk.lastSeen) {
      bits.push('last ' + sk.lastSeen);
    }
    if (sk.groups && sk.groups.length) bits.push('only agents in ' + sk.groups.join(', '));
    if (sk.tags && sk.tags.length) bits.push('tagged ' + sk.tags.join(', '));
    // Where it came from, which the store keeps as a line at the end of the body.
    const src = sourceOf(sk.body);
    if (src) bits.push(tr('skill.learned_from', {src}));
    el.append(cell('meta', bits.join(' · ')));

    // The rule itself. The list showed a one-line description and governed something nobody could
    // read — a screen for deciding whether a rule should exist, with the rule not on it. The body
    // is already in the answer; it was only never drawn.
    //
    // Behind a toggle rather than always open: these are paragraphs, and twenty of them expanded
    // is a page you scroll past rather than one you audit.
    const body = (sk.body || '').trim();
    if (!body) return el;
    const text = cell('body');
    text.textContent = stripSource(body);
    text.hidden = true;
    const more = document.createElement('md-text-button');
    more.className = 'fold';
    let open = false;
    more.textContent = tr('action.read');
    withMark(more, '#i-sl-file-lines');
    more.onclick = () => {
      open = !open;
      text.hidden = !open;
      more.textContent = tr(open ? 'action.collapse' : 'action.read');
    };
    top.insertBefore(more, drop);
    el.append(text);
    return el;
  }), skillWrite(list));
}

// ── what they can reach ──────────────────────────────────────────────────────
// The MCP servers each companion has, and the form to add one. Not polled: a config file does not
// change while you are looking at it, and this page is read to decide something.
async function loadMCP() {
  const list = await fetchList('/mcp');
  if (!list) return;
  const reachedFrom = new Set(list.map(s => s.companion || 'every companion here'));
  reach(true);
  shared.servers = list.length;
  sayShared();
  shared.reachedFrom = reachedFrom.size;

  // Same search, the other half. Ten servers is a screen and a half, and the half below the fold is
  // the half nobody scrolls to — the experience list got a find the moment it had four rows and
  // this one was left to grow without.
  let show = list;
  if (mcpQuery.trim()) {
    const docs = list.map(sv => [sv.name, sv.command, (sv.args || []).join(' '), sv.url,
                                 sv.companion, (sv.env || []).join(' ')].filter(Boolean).join(' '));
    show = rankByIDF(mcpQuery, docs).map(i => list[i]);
  }

  const rows = show.map(sv => {
    const el = cell('srv ' + sv.tier);
    const top = cell('top');
    // ⚠ Hardcoded English, not from the pack — so it missed the sentence-case pass and it does not
    // translate. Cased here to match the rest; the missing translation is recorded separately.
    top.append(cell('tier', sv.tier === 'global' ? 'Every companion here' : 'Only ' + sv.companion));
    top.append(cell('what', sv.name));
    // Editing one meant typing all of it into the add form again and trusting the name matched —
    // the write is by name, so a typo made a SECOND server rather than changing the first.
    const edit = document.createElement('md-text-button');
    edit.className = 'srvedit';
    edit.textContent = tr('action.edit');
    withMark(edit, '#i-sl-pen-to-square');
    tip(edit, tr('hint.edit_server', {file: sv.file}));
    edit.onclick = () => openMCP(sv);
    top.append(edit);
    const drop = document.createElement('md-text-button');
    drop.className = 'drop';
    tip(drop, tr('hint.remove_server', {file: sv.file}));
    withMark(drop, '#i-ss-trash-can');
    arm(drop, tr('action.remove'), () => {
      const body = new URLSearchParams({name: sv.name, delete: '1'});
      if (!sv.socket) body.set('tier', 'global');
      post('/mcp', body, sv.socket || null).then(loadMCP);
    });
    top.append(drop);
    el.append(top);
    // The transport, complete and unprettified: this line is the answer to "what actually runs".
    const how = sv.url ? sv.url : [sv.command, ...(sv.args || [])].join(' ');
    el.append(cell('how', how));
    const bits = [];
    // Names only. magi never sends the values, and saying which are needed is what a person needs
    // to set them up on their own machine.
    if (sv.envNames && sv.envNames.length) bits.push('needs ' + sv.envNames.join(', '));
    bits.push(sv.file);
    el.append(cell('where', bits.join(' · ')));
    return el;
  });

  // The fields, built into the dialog's form rather than into a form of their own on the page.
  const form = mcpFormEl;
  form.replaceChildren();
  // A short label that can live in the outline's notch, and the explanation underneath it. Both
  // through tr(): these five were hardcoded English while the pack carried translations for them
  // that nothing read.
  //
  // The keys are written out rather than assembled from the field's name. The check that every
  // label exists in both packs reads them out of this file, and a key built at runtime is one it
  // cannot see — which is how a label ends up rendering as its own dotted name on somebody's
  // screen. Same reason the preferences list their keys.
  // The asterisk is part of the LABEL, not a decoration beside it: the guide asks for it at the
  // end of the label and says the accessibility label must include it. The server refuses a
  // nameless server, so this is the one field that has it.
  //
  // ⚠ And they are asked in two SETS, not all at once. A server is reached over HTTP or it is a
  // process this machine starts, and the two have nothing in common: an HTTP server is a url and
  // that is the whole of it — what it can do is the server's own business, advertised over the
  // protocol, and nothing here needs to be told. A stdio server has no url and needs the command
  // that starts it. Asking for all six every time made a person filling in a url read four boxes
  // that could not apply to them and wonder which of the four they were getting wrong.
  const MCP_FIELDS = [
    ['url', 'label.mcp_url', 'hint.mcp_url', false, 'http'],
    ['command', 'label.mcp_command', 'hint.mcp_command', true, 'stdio'],
    ['args', 'label.mcp_args', 'hint.mcp_args', false, 'stdio'],
    ['env', 'label.mcp_env', 'hint.mcp_env', false, 'stdio'],
    ['name', 'label.mcp_name', 'hint.mcp_name', true, 'both'],
  ];
  const mcpField = ([name, labelKey, hintKey, must, kind]) => {
    const i = document.createElement('md-outlined-text-field');
    i.name = name;
    i.dataset.kind = kind;
    i.setAttribute('label', tr(labelKey) + (must ? ' *' : ''));
    i.setAttribute('supporting-text', tr(hintKey));
    if (must) i.setAttribute('required', '');
    return i;
  };
  // Which of the two, chosen first, because it decides what the rest of the form even is.
  const kindSel = document.createElement('md-outlined-select');
  kindSel.name = 'kind';
  kindSel.setAttribute('label', tr('label.mcp_kind'));
  for (const [v, k] of [['http', 'mcp.kind_http'], ['stdio', 'mcp.kind_stdio']]) {
    const o = document.createElement('md-select-option');
    o.value = v; o.append(cell('', tr(k)));
    kindSel.append(o);
  }
  kindSel.value = 'http';
  const who = document.createElement('md-outlined-select');
  who.name = 'who';
  who.setAttribute('label', tr('label.reach'));
  const opts = [['', tr('reach.every_companion')]].concat(
    (fleetSeen || []).filter(a => !a.peer).map(a => [a.socket, tr('reach.only', {name: a.name})]));
  for (const [v, label] of opts) {
    const o = document.createElement('md-select-option');
    o.value = v; o.append(cell('', label));
    who.append(o);
  }
  const fieldEls = MCP_FIELDS.map(mcpField);
  form.append(kindSel, who, ...fieldEls);
  // Only the ones this kind uses. Hidden rather than disabled, because a disabled field somebody
  // can see is a field they will try to fill in: these do not apply at all, they are not refused.
  // The name is the one both need — it is the key this server is written under, not a description
  // of it — and it is filled in from the url or the command so the common case is one box.
  const showKind = () => {
    for (const f of fieldEls) f.hidden = f.dataset.kind !== 'both' && f.dataset.kind !== kindSel.value;
  };
  kindSel.addEventListener('change', showKind);
  showKind();
  const nameEl = fieldEls.find(f => f.name === 'name');
  const suggestName = src => {
    if (nameEl.value.trim()) return;                 // never over somebody's own answer
    const from = String(src || '').trim();
    if (!from) return;
    const base = /^https?:/i.test(from)
      ? (from.replace(/^https?:\/\//i, '').split(/[/?#]/)[0] || '').split(':')[0].split('.')[0]
      : from.split(/[\s/]+/).filter(Boolean).pop() || '';
    nameEl.value = base.replace(/[^A-Za-z0-9_-]/g, '');
  };
  for (const f of fieldEls) {
    if (f.name === 'url' || f.name === 'command') {
      f.addEventListener('change', () => suggestName(f.value));
      f.addEventListener('blur', () => suggestName(f.value));
    }
  }
  const note = cell('note',
    'Written to that companion\'s config file. It attaches when that daemon next starts — ' +
    'this changes the file, not a running process.');
  form.append(note);
  // A dialog form closes on submit whichever button was pressed, so the value says which one it
  // was. Cancel must not write anything — a form that posts on the way out is the shape of a
  // dialog nobody trusts.
  form.onsubmit = async e => {
    if (mcpDialog.returnValue === 'cancel') return;
    e.preventDefault();
    const body = new URLSearchParams();
    for (const el of form.querySelectorAll('md-outlined-text-field')) {
      if (el.value.trim()) body.set(el.name, el.value.trim());
    }
    if (!who.value) body.set('tier', 'global');
    const fields = [...form.querySelectorAll('md-outlined-text-field')];
    for (const f of fields) { f.removeAttribute('error'); f.removeAttribute('error-text'); }
    const why = await post('/mcp', body, who.value || null, true);
    if (why) {
      // On the field the refusal is about, which is the only place a person can act on it. The
      // guide is explicit that an error goes to the field's own label with the alert role, and the
      // component does both once error-text is set. What the server refuses names its own field.
      const at = fields.find(f => why.includes(f.name)) ||
        fields.find(f => f.name === 'url' && /url|command/.test(why)) || null;
      if (at) { at.setAttribute('error', ''); at.setAttribute('error-text', why.slice(0, 120)); at.focus(); }
      else says(why.slice(0, 80));
      return;                                   // the dialog stays open, with the reason showing
    }
    mcpDialog.close('add');
    loadMCP();
  };
  // Opening it, empty for a new one and filled for an existing one. One dialog, because the two are
  // the same answers and a second form would be a second place for them to disagree.
  openMCP = (sv) => {
    const put = (n, v) => { const f = fieldEls.find(x => x.name === n); if (f) f.value = v || ''; };
    kindSel.value = sv && sv.command ? 'stdio' : 'http';
    showKind();
    put('url', sv && sv.url); put('command', sv && sv.command);
    put('args', sv && (sv.args || []).join(' ')); put('env', sv && (sv.envNames || []).join(' '));
    put('name', sv && sv.name);
    // The name is the key this server is written under. Changing it while editing would leave the
    // old one behind and make a second, which is what typing it again into the add form did.
    nameEl.toggleAttribute('readonly', !!sv);
    who.value = sv ? (sv.socket || '') : '';
    mcpDialogK.textContent = tr(sv ? 'label.edit_server' : 'label.add_server');
    mcpGo.textContent = tr(sv ? 'action.save' : 'action.add_or_replace');
    for (const f of fieldEls) { f.removeAttribute('error'); f.removeAttribute('error-text'); }
    mcpDialog.show();
  };
  // What the page shows in the form's place: one button that opens it.
  // Tonal, not filled. This screen already carries the filled button for writing something down,
  // and the guide asks for one per page — filled is the action that ENDS a flow, and there cannot
  // be two of those on one screen. Adding a server is the lower-emphasis of the two.
  const open = document.createElement('md-filled-tonal-button');
  open.className = 'mcpopen';
  open.textContent = tr('action.add_server');
  withMark(open, '#i-sl-plus');
  open.onclick = () => openMCP(null);

  if (!list.length) {
    mcpEl.replaceChildren(sectionHead('nav.connections', open), emptyState('empty.no_servers', 'empty.no_servers_how'));
    return;
  }
  if (!rows.length) {
    mcpEl.replaceChildren(sectionHead('nav.connections', open), mcpFind(),
      emptyState('empty.no_match', 'empty.no_match_how'));
    return;
  }
  mcpEl.replaceChildren(sectionHead('nav.connections', open), mcpFind(), ...rows);
}

// ── one agent ────────────────────────────────────────────────────────────────
// Follow the tail only while the reader is already at the bottom. Yanking the view down while
// somebody reads the middle of a long run is how a live page becomes unreadable.
const atBottom = () => window.innerHeight + window.scrollY >= document.body.offsetHeight - 48;

// ── markdown, as nodes ───────────────────────────────────────────────────────
//
// The terminal has rendered markdown since it existed; this page showed the source. A table arrived
// as a wall of pipes, a fenced block as three backticks and its contents run together, and the
// thing the model wrote to be read was the one thing that could not be.
//
// Every node here is built with createElement and filled with textContent. No HTML string is
// produced from a transcript, at any point, so there is nothing for a sanitiser to be right or
// wrong about. The one place the hazard reaches is markdown's raw-HTML token, which the lexer hands
// over as source text in its raw field — drawn as TEXT below, on purpose and with a test on it.

function el(tag, text) {
  const e = document.createElement(tag);
  if (text !== undefined) e.textContent = text;
  return e;
}

// inline draws marked's inline tokens into parent.
function inline(parent, toks) {
  for (const t of toks || []) {
    switch (t.type) {
      case 'strong':   { const n = el('strong'); inline(n, t.tokens); parent.append(n); break; }
      case 'em':       { const n = el('em'); inline(n, t.tokens); parent.append(n); break; }
      case 'del':      { const n = el('del'); inline(n, t.tokens); parent.append(n); break; }
      case 'codespan': parent.append(el('code', t.text)); break;
      case 'br':       parent.append(el('br')); break;
      case 'link': {
        // The href is checked here rather than trusted. A transcript can carry javascript: and
        // data: urls, and an anchor is the one node that would act on one.
        const a = el('a');
        inline(a, t.tokens);
        if (/^(https?:|mailto:)/i.test(t.href || '')) {
          a.href = t.href; a.target = '_blank'; a.rel = 'noopener noreferrer';
        }
        parent.append(a);
        break;
      }
      // Raw HTML in the source is shown as what it is. This is the line that keeps a tool result
      // full of markup from becoming markup.
      case 'html':     parent.append(document.createTextNode(t.raw)); break;
      default:         parent.append(document.createTextNode(t.raw !== undefined ? t.raw : (t.text || '')));
    }
  }
}

// blocks draws marked's block tokens into parent.
function blocks(parent, toks) {
  for (const t of toks || []) {
    switch (t.type) {
      case 'heading': {
        // Clamped to h3..h6: the page has its own heading order and a transcript must not open a
        // level above the section it sits in.
        const n = el('h' + Math.min(6, Math.max(3, (t.depth || 1) + 2)));
        inline(n, t.tokens); parent.append(n); break;
      }
      case 'paragraph': { const n = el('p'); inline(n, t.tokens); parent.append(n); break; }
      case 'text':      { const n = el('p'); if (t.tokens) inline(n, t.tokens); else n.textContent = t.text; parent.append(n); break; }
      case 'code': {
        const lang = t.lang ? String(t.lang).split(/\s+/)[0] : '';
        const pre = el('pre');
        if (lang === 'diff' || lang === 'patch' || looksLikeDiff(t.text)) {
          pre.className = 'diff';
          diffInto(pre, t.text);
        } else {
          const code = el('code', t.text);
          if (lang) code.setAttribute('data-lang', lang);
          pre.append(code);
        }
        parent.append(pre); break;
      }
      case 'blockquote': { const n = el('blockquote'); blocks(n, t.tokens); parent.append(n); break; }
      case 'hr':        parent.append(el('hr')); break;
      case 'list': {
        const list = el(t.ordered ? 'ol' : 'ul');
        if (t.ordered && t.start !== '' && t.start !== 1) list.start = t.start;
        for (const item of t.items || []) {
          const li = el('li');
          if (item.task) {
            // Drawn, not interactive: nothing here can change what a transcript recorded.
            const box = el('input'); box.type = 'checkbox'; box.checked = !!item.checked;
            box.disabled = true; li.append(box, document.createTextNode(' '));
          }
          blocks(li, item.tokens);
          list.append(li);
        }
        parent.append(list); break;
      }
      case 'table': {
        const wrap = el('div'); wrap.className = 'tablewrap';
        const table = el('table'), thead = el('thead'), hr2 = el('tr');
        (t.header || []).forEach((c, i) => {
          const th = el('th'); inline(th, c.tokens);
          if (t.align && t.align[i]) th.style.textAlign = t.align[i];
          hr2.append(th);
        });
        thead.append(hr2); table.append(thead);
        const tb = el('tbody');
        for (const row of t.rows || []) {
          const tr = el('tr');
          row.forEach((c, i) => {
            const td = el('td'); inline(td, c.tokens);
            if (t.align && t.align[i]) td.style.textAlign = t.align[i];
            tr.append(td);
          });
          tb.append(tr);
        }
        table.append(tb); wrap.append(table); parent.append(wrap); break;
      }
      case 'space':     break;
      case 'html':      parent.append(el('p', t.raw)); break;
      default:          parent.append(el('p', t.raw !== undefined ? t.raw : (t.text || '')));
    }
  }
}

// md fills a node with rendered markdown, falling back to plain text if the lexer throws.
//
// The fallback is what the page did everywhere before this, so a token stream the lexer cannot
// make sense of costs the formatting and never the content.
// hunkHeader is what makes a blob of text a unified diff rather than a list with dashes in it.
//
// Required, and not merely "starts with a plus or a minus". A tool result full of bullet points
// would otherwise come back half green and half red, which is worse than not colouring it: it
// would be saying something untrue about what changed.
const hunkHeader = /^@@ -\d+(,\d+)? \+\d+(,\d+)? @@/m;

function looksLikeDiff(text) {
  const t = String(text || '');
  return hunkHeader.test(t) || /^diff --git /m.test(t);
}

// diffInto fills a <pre> with a unified diff, a line at a time, classed by what the line does.
//
// The terminal has coloured these since it had a transcript. Here they arrived as an undifferen-
// tiated wall in which the one thing a diff is for — which lines went and which arrived — was the
// thing you had to work out by reading the first character of every row.
// pathOf is the file a call names, for the line above its diff.
function pathOf(args) {
  try {
    const a = JSON.parse(args || '{}');
    return a && typeof a.path === 'string' ? a.path : '';
  } catch { return ''; }
}

function diffInto(pre, text) {
  for (const line of String(text || '').replace(/\n$/, '').split('\n')) {
    let cls = 'dctx';
    if (/^\+\+\+/.test(line) || /^---/.test(line) || /^diff /.test(line)) cls = 'dfile';
    else if (/^@@/.test(line)) cls = 'dhunk';
    else if (line.startsWith('+')) cls = 'dadd';
    else if (line.startsWith('-')) cls = 'ddel';
    const row = el('span', line + '\n');
    row.className = cls;
    pre.append(row);
  }
  return pre;
}

function md(node, text) {
  let toks;
  try { toks = mdLex(text || ''); }
  catch { node.textContent = text || ''; return node; }
  blocks(node, toks);
  return node;
}

// foldedKinds are the rows that arrive folded: the model's own reasoning, and what a tool was
// asked and answered.
//
// Not hidden — folded, with a summary that says what is inside. The terminal has had this since it
// had a transcript, and the page had neither: a thousand-line tool result sat open between two
// sentences, and reading a conversation meant scrolling past the machinery of it. What is in them
// is the evidence for everything else on the page, so it stays one press away and never further.
const foldedKinds = { thinking: true, tool: true, result: true, failed: true, council: true, shell: true };

// summaryFor is the one line a folded row shows. It has to say enough to decide whether to open it.
// todosOf reads the plan out of a todo call's arguments, or null when the call is not one.
function todosOf(args) {
  try {
    const a = JSON.parse(args || '{}');
    return Array.isArray(a && a.todos) ? a.todos : null;
  } catch { return null; }
}

// answerLine is the one line of a result that fits beside the call that produced it: the first
// thing it said, which for a build is the headline and for a grep is the first hit.
function answerLine(out) {
  const t = String(out || '').replace(/^"|"$/g, '').trim();
  if (!t) return '';
  const first = t.split('\n').find(l => l.trim()) || '';
  return oneLine(first, 44);
}

// The mark on a folded row's summary line: how the call ended, or what kind of row this is.
//
// Returned as a pair — the symbol to draw and the character to fall back to — because both have to
// mean the same thing, and a build with no icons must still say it. ⚠ is not a failure: the call
// did what it was asked and left something to read (a post-edit hook, a language server on the
// file it just wrote), and drawing that as ✗ told somebody their file had not been written while
// it sat on disk.
function summaryMark(r) {
  if (r.who === 'tool') {
    if (r.ok === undefined) return ['#i-sl-spinner-third', '\u2699', 'spin'];
    if (r.ok) return ['#i-sl-check', '\u2713', 'ok'];
    return r.note ? ['#i-sl-triangle-exclamation', '\u26A0', 'note'] : ['#i-sl-xmark', '\u2717', 'bad'];
  }
  if (r.who === 'result') return ['#i-sl-check', '\u2713', 'ok'];
  if (r.who === 'failed') return ['#i-sl-xmark', '\u2717', 'bad'];
  return null;
}

function summaryFor(r) {
  if (r.who === 'tool') {
    // The glyph says how it ended, on the line that is visible while the row is shut. Split across
    // two rows, "did that work" could only be answered by finding the one below and opening it.
    // ⚠ is not a failure: the call did what it was asked and left something to read — a post-edit
    // hook, a language server on the file it just wrote. Those arrive marked as errors so the
    // agent reads them, and drawing that as ✗ told somebody their file had not been written while
    // it sat on disk.
    // The mark itself is built by summaryMark below and prepended by the caller; what stays here
    // is the sentence. A glyph inside the string could not become a drawing without the string
    // becoming a node, and this string is also what the fold's aria-label reads.
    const g = '';
    // A plan is a list of statuses, and its raw arguments are the worst way to read one — the same
    // JSON the panel turns into ticked lines, flattened and clipped mid-item. The count goes where
    // the argument preview would have been, exactly as the terminal does it.
    const todos = todosOf(r.args);
    if (todos) {
      const done = todos.filter(t => t.status === 'completed').length;
      return (r.tool || '') + '  ' + done + '/' + todos.length;
    }
    // What it was asked, and then what came back. The terminal has put the outcome on this line
    // since it had one, and "did that work" is only half the question — the other half is what it
    // found, which was behind a fold on a row somebody had no reason to open.
    const asked = r.diff ? pathOf(r.args) : (r.args ? oneLine(r.args, 60) : '');
    const said = r.ok === undefined ? '' : answerLine(r.out);
    return (r.tool || '') + (asked ? ' ' + asked : '') + (said ? '  \u27F6 ' + said : '');
  }
  // A council row's summary is its first line: the vote, or the outcome and the tally. What is
  // behind it is the reasoning and what the vote rested on — the same split the terminal makes,
  // where one line per member sits in the transcript and a press opens the whole thing.
  if (r.who === 'council') return String(r.text || '').split('\n')[0];
  if (r.who === 'shell') return '! ' + r.text + (r.exit === undefined ? '' : '  → ' + r.exit);
  // A result that arrived without its call — a compaction took the call away — still says how it
  // ended. The glyph is the fact; the colour repeats it for whoever reads colour.
  if (r.who === 'result') return oneLine(r.text, 88);
  if (r.who === 'failed') return oneLine(r.text, 88);
  // The label is a different word from the kind on purpose. 'thinking' is what the server calls
  // this row; the word a person reads is a translated label, and spelling them the same is how one
  // of them ends up hard-coded in English on every other locale.
  if (r.who === 'thinking') return tr('row.reasoning') + ' · ' + oneLine(r.text, 80);
  return oneLine(r.text, 90);
}

// The three named seats. A map rather than a template so nothing from the log reaches a class name.
const COUNCIL_SEATS = {melchior: 'm-melchior', balthasar: 'm-balthasar', casper: 'm-casper'};

function oneLine(s, n) {
  const t = String(s || '').replace(/\s+/g, ' ').trim();
  return t.length > n ? t.slice(0, n) + '…' : t;
}

// copyChip copies a message's SOURCE, which is the thing the page cannot otherwise give you.
//
// A plain button with a class, not the button component: this is one per prose row, the component
// is a custom element with a shadow root of its own, and the transcript had just stopped building
// hundreds of nodes it did not need. The page already builds the facts card's fold bar this way.
function copyChip(text) {
  const b = document.createElement('button');
  b.type = 'button';
  // hit48 is the page's own expander: a 48dp press area taken out of flow, so the target is a
  // target without the row growing around it. Measured before it: 9×12 pixels.
  b.className = 'copy hit48';
  b.append(iconOr('#i-sl-copy', '\u29C9'));
  b.setAttribute('aria-label', tr('action.copy'));
  tip(b, tr('action.copy'));
  b.onclick = async ev => {
    ev.preventDefault();
    ev.stopPropagation();
    try {
      await navigator.clipboard.writeText(String(text || ''));
    } catch (e) {
      // Said, not swallowed. A copy that silently did nothing is worse than one that failed
      // loudly: the next thing somebody does is paste, and by then the reason is gone.
      says(tr('copy.refused'));
      return;
    }
    // Told twice, because one of the two reaches everybody: the glyph for whoever is looking at
    // the button, the live region for whoever is not.
    b.textContent = '✓';
    b.classList.add('done');
    say(tr('copy.done'));
    clearTimeout(b._back);
    b._back = setTimeout(() => { b.textContent = '⧉'; b.classList.remove('done'); }, 1200);
  };
  return b;
}

// rowNode builds one transcript row.
function rowNode(r) {
  // The decision joins the class list, so a vote is coloured by what it says rather than all
  // council rows sharing one colour. A "continue" is a rejection and has to look like one.
  // …and who said it, so the three councillors keep the hues they have in the terminal. Lowercased
  // and matched against the three known names: a custom member falls through to the council
  // colour rather than into an undefined class, and a name out of the log never becomes a selector.
  // seated marks a row whose gutter belongs to a named councillor, so the verdict's colour stops
  // taking it. Both rules named .who at the same weight and the verdict came later in the file, so
  // every seat's hue lost to red or green — the three colours were declared, applied, and never
  // once seen. Measured: melchior's gutter came out the error red.
  const seat = COUNCIL_SEATS[String(r.member || '').toLowerCase()] || '';
  const d = el('div'); d.className = 'row ' + r.who + (r.decision ? ' v-' + r.decision : '')
    + (r.abandoned ? ' abandoned' : '')
    + (seat ? ' seated ' + seat : '')
    + (r.who === 'tool' && r.ok === false && !r.note ? ' toolfail' : '')
    + (r.who === 'tool' && r.note ? ' toolnote' : '')
    + (r.who === 'tool' && r.ok === true ? ' toolok' : '')
    + (r.pending ? ' pending' : '');
  // What magi itself said to the agent is a distinct voice, and calling it "system" in the gutter
  // names the mechanism rather than the speaker. The rows are the orchestrator's nudges, the
  // compaction summaries and the hook output — magi talking to the agent about the work.
  let w = el('div', whoWord(r)); w.className = 'who';
  // A seat's name is pressable: it leads to what that member was judging, which is the half that
  // makes a vote checkable and the half a transcript row has no room for.
  if (r.who === 'council' && r.member && r.round) {
    const name = document.createElement('button');
    name.type = 'button';
    name.className = 'who whoin hit48';
    name.textContent = whoWord(r);
    name.setAttribute('aria-label', tr('detail.evidence') + ': ' + r.member);
    tip(name, tr('detail.evidence'));
    name.onclick = ev => { ev.stopPropagation(); goVerdict(r.round, r.member); };
    w = name;
  }
  // And when. Under the name in the gutter rather than beside the text, so it costs no width in
  // the column the conversation is read in and lines up down the page as a column of its own.
  // Local time, HH:MM, and nothing at all for a row whose message carries no stamp — an older log
  // has none, and "00:00" would be a time this never happened at.
  if (hhmm(r.at)) {
    const when = el('div', hhmm(r.at));
    when.className = 'when';
    w.append(when);
  }
  // What was actually said, on the two rows that are prose. Selecting and pressing copy gets the
  // RENDERED text — a table comes out as its cells run together and a fenced block loses its
  // fence — and what somebody pasting an answer somewhere else wants is the source it was written
  // in. The terminal has had this since it had a transcript.
  //
  // Always there rather than on hover: a control that appears under the pointer is a control a
  // touch screen never shows and a keyboard never reaches. It is quiet instead.
  if ((r.who === 'user' || r.who === 'assistant') && String(r.text || '').trim()) {
    w.append(copyChip(r.text));
  }

  // A council seat opens into its own screen, and the way in is the NAME.
  //
  // It was a button beside the row, on the reasoning that the row already folds and one gesture
  // doing two things is what people press by accident. In use the button reads as furniture on
  // every council row, and the name is what somebody points at when they want to know what that
  // member saw. So the name is the control and the fold is still the fold — two targets, both
  // reachable by keyboard, neither guessing which was meant.
  const deeper = null;

  if (foldedKinds[r.who]) {
    const det = el('details'); det.className = 'txt fold';
    // Remembered per kind, so somebody who wants to watch tool calls is not re-opening them all
    // turn. A failed one starts open: it is the row you came to read.
    // A failure is the row somebody came to read, whether it arrived as its own row or folded
    // into the call that produced it.
    det.open = r.who === 'failed' || r.ok === false || localStorage.getItem('fold.' + r.who) === 'open';
    // A note is worth opening too: the whole point of it is that something wants reading.
    // One click opens ONE row.
    //
    // It used to open every row of the same kind that was on screen, on the theory that the
    // per-kind preference should apply to what you are looking at and not only to what arrives
    // next. In use that is not what it reads as: you click a tool call to see what it ran and the
    // page moves under you — everything above and below expands, the row you clicked is somewhere
    // else now, and the thing you wanted to read has scrolled away. A disclosure control discloses
    // the thing it is attached to.
    //
    // The preference is still remembered per kind, and still decides how the NEXT rows arrive.
    det.addEventListener('toggle', () => {
      localStorage.setItem('fold.' + r.who, det.open ? 'open' : 'shut');
    });
    det.dataset.kind = r.who;
    const head = el('summary');
    const mk = summaryMark(r);
    if (mk) {
      head.append(iconOr(mk[0], mk[1], 'mk ' + mk[2]), document.createTextNode(' '));
    }
    head.append(document.createTextNode(summaryFor(r)));
    det.append(head);
    const body = el('div'); body.className = 'foldbody';
    // A tool call is its arguments; a result is its output. Neither is prose, so both are drawn as
    // preformatted text rather than run through markdown that would eat their brackets.
    const plan = r.who === 'tool' ? todosOf(r.args) : null;
    if (plan) {
      // Drawn the way the panel draws it — same glyphs, same strikethrough — so the transcript and
      // the panel agree about what the plan says.
      body.append(planRows(plan));
    } else if (r.who === 'tool' || r.who === 'result' || r.who === 'failed' || r.who === 'shell') {
      // What it was asked, and what it answered — each said in words above its own block.
      //
      // These were one blob with a rule drawn between them: "args ── ⟶ ── output". A line of box
      // characters is not a label; it tells a reader that something changed there and leaves them
      // to work out what, which for a failed call is the difference between the command and the
      // reason. Two labelled blocks say it.
      //
      // An edit is shown as the change it makes. Its arguments are the old and new text escaped
      // into one JSON line, which is the least readable form of the most important thing an agent
      // does; the path stays, because that is what the rest of the arguments were for.
      const parts = [];
      if (r.diff) {
        if (pathOf(r.args)) parts.push(['fold.asked', pathOf(r.args), false]);
        parts.push(['fold.changed', r.diff, true]);
      } else if (r.args) {
        parts.push(['fold.asked', r.args, looksLikeDiff(r.args)]);
      }
      if (r.out) parts.push(['fold.answered', r.out, looksLikeDiff(r.out)]);
      // A row with neither — a result whose call was compacted away — is just its text.
      if (!parts.length) parts.push(['', r.text, looksLikeDiff(r.text)]);
      for (const [key, text, asDiff] of parts) {
        // One block needs no label: with nothing to tell it apart from, a word above it is noise.
        if (key && parts.length > 1) body.append(cell('foldk', tr(key)));
        if (asDiff) {
          const pre = el('pre');
          pre.className = 'diff';
          body.append(diffInto(pre, text));
        } else {
          body.append(el('pre', text));
        }
      }
    } else if (r.who === 'council') {
      // Everything after the summary line, which the summary already showed.
      md(body, String(r.text || '').split('\n').slice(1).join('\n').trim());
    } else {
      md(body, r.text);
    }
    det.append(body);
    // The call that is running, said on the call itself. Indeterminate because there is no
    // denominator — a tool does not report how far through it is — and that is honest here in a
    // way a page-level bar would not be: this one names WHICH call, which is a fact the page has
    // and was not showing. It is only ever on the last row (see markPending), so nothing spins for
    // a call that ended without a result.
    if (r.pending) {
      const bar = document.createElement('md-linear-progress');
      bar.indeterminate = true;
      bar.className = 'runbar';
      bar.setAttribute('aria-label', tr('row.working'));
      det.append(bar);
      // And what the call last said about itself, when it says anything. The bar means "still
      // going", which after four minutes is the part you already believe; this is the part that
      // decides whether to wait or interrupt. It reaches the browser on the fleet poll rather than
      // with this row — the note is not in the log — so it is read from there at draw time.
      if (liveNote) {
        const n = el('div', '⏳ ' + liveNote);
        n.className = 'note';
        det.append(n);
      }
    }
    d.append(w, det);
    if (deeper) d.append(deeper);
    return d;
  }

  // Said, not only greyed: a request nothing will ever answer is a fact about the conversation,
  // and colour alone is a fact some readers are not told. The log says which of the two happened
  // no more precisely than this, so neither does the note.
  if (r.abandoned) {
    const tag = el('span', ' · ' + tr('row.abandoned'));
    tag.className = 'pendtag';
    w.append(tag);
  }
  if (r.pending) {
    // Said in words as well as drawn. A state carried only by a bar is a state some readers are
    // not told, and this one is the answer to "is it working on what I just asked".
    //
    // Appended, not written over the gutter: rewriting textContent here took the row's timestamp
    // with it, so the one row you are most likely to be watching was the one with no time on it.
    const tag = el('span', ' · ' + tr('row.working'));
    tag.className = 'pendtag';
    w.append(tag);
  }
  const t = el('div'); t.className = 'txt';
  // The user's own words are shown as written. Rendering them would mean a prompt containing a
  // pipe table came back looking like something they did not type.
  // An error leads with the mark, not with the colour. Red alone is a state told only in ink.
  if (r.who === 'error') t.textContent = '✗ ' + r.text;
  else if (r.who === 'user') t.textContent = r.text;
  else md(t, r.text);
  d.append(w, t);
  return d;
}

// ── the shape of a report ────────────────────────────────────────────────────
// What this companion must fill in before it may put a decision to somebody. The sections are a
// contract — ask_user refuses a report with one missing — and the person the report is for is the
// one who knows what belongs in it. Until now changing it meant writing a markdown file into a
// workspace over ssh, so it stayed at whatever three sections the default picked.
const fmtDialog = document.getElementById('fmtDialog');
const fmtK = document.getElementById('fmtK'), fmtForm = document.getElementById('fmtForm');
const fmtCancel = document.getElementById('fmtCancel'), fmtGo = document.getElementById('fmtGo');
let fmtFor = null;

async function drawReportFormat(a) {
  const box = document.getElementById('reportfmt');
  if (!a) { box.hidden = true; box.replaceChildren(); return; }
  const f = await fetchOne('/report-format' + qFor(a));
  if (!f || !f.sections) { box.hidden = true; box.replaceChildren(); return; }
  const rows = (f.sections || []).map(sec => {
    const row = cell('f');
    row.append(cell('k', sec.key), cell('v', sec.prompt || ''));
    return row;
  });
  // Where it came from, because "edit" means something different in each: yours to change here,
  // shared with every companion under this console, or not written down anywhere yet.
  // Literal keys in a lookup, not a key built by concatenation: a key the pack check cannot see is
  // the one that ships missing and renders as its own dotted name.
  const FROM = {workspace: 'fmt.from_workspace', console: 'fmt.from_console', default: 'fmt.from_default'};
  const head = cell('k', tr('field.report_format') + ' · ' + tr(FROM[f.from] || FROM.default));
  const edit = el('button');
  edit.type = 'button';
  edit.className = 'deeper hit48';
  // A plain button, so the mark is a child rather than a slot — same shape, one level down.
  const em = icon('#i-sl-pen-to-square', {cls: 'mk'});
  if (em) edit.append(em, document.createTextNode(' '));
  edit.append(document.createTextNode(tr('action.edit')));
  edit.onclick = () => openFormat(a, f);
  box.replaceChildren(head, ...rows, edit);
  box.hidden = false;
}

// openFormat is the editor: one row per section, which is the pair a contract is made of.
function openFormat(a, f) {
  fmtFor = a;
  // A headline that says what the dialog does, not what area of the app it belongs to. "Report
  // format" is a heading on a card; on a dialog it leaves the person to work out what saving will
  // change, which is the thing the guide asks the headline to answer.
  fmtK.textContent = tr('fmt.headline');
  fmtForm.replaceChildren();
  // Supporting text, which is the part of a dialog the guide asks for and this one did without: a
  // headline states the subject and the sentence under it says what pressing save will mean. Here
  // that is worth saying outright — these are not preferences, they are what the agent will be
  // refused for leaving out.
  fmtForm.append(cell('dlgsup', tr('fmt.about')));
  // Text buttons for the two low-emphasis actions inside the content, and an icon button for
  // removal — the M3 vocabulary for "an action on this row" rather than a glyph in a link.
  const more = document.createElement('md-text-button');
  // Neither control inside this form submits it. A button in a form defaults to submit, the form
  // is method="dialog", and submitting it CLOSES the dialog — so pressing the ✕ to drop a section
  // threw away every other edit on the way out, and the row was removed from a form nobody could
  // save. Reported as "the dialog shuts before I can save".
  more.setAttribute('type', 'button');
  // With the plus, because a button as wide as the rows above it is read as a row until something
  // says otherwise, and the glyph says it before the word is read.
  more.textContent = '+ ' + tr('fmt.add_section');
  const add = (key, prompt) => {
    const row = cell('fmtrow');
    const k = document.createElement('md-outlined-text-field');
    k.setAttribute('label', tr('fmt.key'));
    k.name = 'key';
    k.value = key || '';
    const p = document.createElement('md-outlined-text-field');
    p.setAttribute('label', tr('fmt.prompt'));
    // A sentence, so it gets the shape of one. As a single line the prompt scrolled sideways
    // inside a field narrower than the text it holds, which is the field you cannot read while
    // you edit it.
    p.setAttribute('type', 'textarea');
    // Tall enough for what is in it, up to four lines. A fixed two put a scrollbar inside a field
    // three lines long, which hides the end of the sentence somebody is editing; a fixed four
    // leaves three short rows looking like a form with holes in it.
    // 26 is the column's measured capacity at the dialog's width, not a guess at one: the field is
    // about 290px of monospace inside a 560dp dialog.
    p.setAttribute('rows', String(Math.min(4, Math.max(2, Math.ceil(String(prompt || '').length / 26)))));
    p.name = 'prompt';
    p.value = prompt || '';
    // Removing a section has a consequence — the agent stops being asked for it — so it is a
    // control of its own rather than clearing the field and hoping somebody notices.
    const drop = document.createElement('md-icon-button');
    drop.setAttribute('type', 'button');   // see the note on `more`: the default submits and closes
    drop.className = 'fmtdrop';
    drop.setAttribute('aria-label', tr('action.remove'));
    drop.innerHTML = '<svg data-i="#i-sl-trash-can" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">' +
      '<path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="1.8" ' +
      'stroke-linecap="round" fill="none"/></svg>';
    dressIcons(drop);
    tip(drop, tr('action.remove'));
    drop.onclick = () => row.remove();
    row.append(k, p, drop);
    fmtForm.insertBefore(row, more);
  };
  more.onclick = () => add('', '');
  fmtForm.append(more);
  for (const sec of (f.sections || [])) add(sec.key, sec.prompt);
  fmtCancel.textContent = tr('action.cancel');
  withMark(fmtCancel, '#i-sl-xmark');
  fmtGo.textContent = tr('action.save');
  withMark(fmtGo, '#i-sl-floppy-disk');
  fmtDialog.show();
}

// saveFormat writes the sections back to the companion's own workspace.
async function saveFormat() {
  if (!fmtFor) return;
  const body = new URLSearchParams();
  for (const row of fmtForm.children) {
    if ((row.className || '') !== 'fmtrow') continue;
    const fields = (row.children || []).filter(c => c.name === 'key' || c.name === 'prompt');
    const key = (fields.find(c => c.name === 'key') || {}).value || '';
    const prompt = (fields.find(c => c.name === 'prompt') || {}).value || '';
    if (!String(key).trim()) continue;
    body.append('key', key);
    body.append('prompt', prompt);
  }
  const why = await post('/report-format', body, fmtFor.socket, fmtFor.peer);
  if (!why) drawReportFormat(fmtFor);
}

// ── what is running beside the turn ──────────────────────────────────────────
// A command left in the background, a child that was spawned. Neither is in the log — a background
// command is a PID in the daemon's process and a session log cannot say whether a child is still
// going — so both are read from the daemon, on the poll that already runs.
//
// Drawn as a strip in the dock rather than as buttons inside the facts card. The card is a thing
// you open to check on something; this is the answer to "is anything happening", and it has to be
// on screen without being asked for. The terminal has kept it along the bottom since it had one.
const stripEl = document.getElementById('strip');

function jobChip(kind, name, say, opts) {
  // A child is a way in; a background command is a fact. So one is pressable and the other is not
  // a control at all: a pressable-looking thing that does nothing when pressed is worse than a
  // plain line of text.
  //
  // A button element, not the button component. What the guide gives for "a small pressable thing
  // in a row of small things" is an assist chip, and that component is not in the vendored bundle;
  // md-text-button forced into a chip's box was one component dressed as another — its own spec is
  // a 40dp pill — and it brought a shadow root whose label box could not be reached from here (the
  // host took padding:0 and the spacing had to be set through its tokens). Built here it is the
  // chip's own measurements, with the hover and press state layers M3 asks for.
  const el0 = document.createElement(opts.go ? 'button' : 'div');
  if (opts.go) { el0.type = 'button'; el0.onclick = opts.go; }
  el0.className = 'job' + (opts.go ? ' press' : '') + (opts.running ? ' live' : ' done') + (opts.bad ? ' bad' : '');
  // One line, and for the pressable one a single text node.
  //
  // The button component renders whatever it is given inside its own label box, which is laid out
  // for a word: handed three elements it stacked them and the long one spilled out of the chip and
  // off the left edge of the page — measured. So the control gets a label, and the running state
  // is carried by the chip's own outline rather than by a dot it cannot place.
  const words = oneLine(name, 28) + (say ? ' · ' + oneLine(say, 44) : '');
  if (opts.go) {
    el0.textContent = words;
  } else {
    if (opts.running) el0.append(cell('jdot', ''));
    el0.append(cell('jname', oneLine(name, 28)));
    if (say) el0.append(cell('jsay', oneLine(say, 44)));
  }
  el0.setAttribute('aria-label', kind + ': ' + name + (say ? ' — ' + say : ''));
  return el0;
}

// lastLine is what a background command has said most recently. The chip has one line of room and
// the interesting end of a log is the bottom of it.
function lastLine(s) {
  const lines = String(s || '').split('\n').filter(l => l.trim());
  return lines.length ? oneLine(lines[lines.length - 1], 60) : '';
}

// drawQueued is what this companion has NOT started yet, in the order it will.
//
// Two queues stand behind it and they stay apart — one refuses past a handful, the other must
// never refuse the person in front of the thing — but there is one agent and it does one thing at
// a time, so what a reader needs is one list in the order it will actually be taken. Showing only
// the handovers said a companion had nothing waiting while the correction somebody typed sat in
// the other queue.
function drawQueued(items) {
  const box = document.getElementById('queued');
  if (!items || !items.length) { box.hidden = true; box.replaceChildren(); return; }
  const rows = items.map((q, i) => {
    const row = cell('qrow' + (q.kind === 'person' ? ' mine' : ''));
    // The position, because "waiting" is a different fact from "waiting behind three others".
    row.append(cell('qn', String(i + 1)));
    const what = cell('qwhat');
    what.append(cell('qwho', q.kind === 'person' ? tr('queued.you') : (q.from || tr('queued.handed'))));
    what.append(cell('qsaid', oneLine(q.text || '', 120)));
    row.append(what);
    return row;
  });
  box.replaceChildren(cell('k', tr('field.queued', {n: items.length})), ...rows);
  box.hidden = false;
}

async function loadJobs(a) {
  if (!a) { stripEl.hidden = true; stripEl.replaceChildren(); drawQueued(null); return; }
  const j = await fetchList('/jobs' + qFor(a)) || {};
  drawQueued(j.queued);
  const kids = j.children || [], bg = j.background || [];
  if (!kids.length && !bg.length) { stripEl.hidden = true; stripEl.replaceChildren(); return; }
  const chips = [];
  for (const c of kids) {
    // A child opens into the screen that already exists for it, which is the whole reason the
    // strip is worth having: one press from "something is running" to what it is doing.
    chips.push(jobChip(tr('detail.subagent'), c.tool || tr('detail.subagent'), oneLine(c.task || '', 48),
      {running: c.running, bad: !!c.err, go: () => goDeep('sub', c.id)}));
  }
  for (const b of bg) {
    chips.push(jobChip(tr('job.command'), oneLine(b.command || '', 40), lastLine(b.tail),
      {running: b.running, bad: !b.running && b.exit !== 0}));
  }
  stripEl.replaceChildren(...chips);
  stripEl.hidden = false;
}

// pastRows is the transcript of the finished session currently open, for the screens that read a
// row out of it rather than fetching their own copy.
let pastRows = [];

// localRows are the shell runs this console has done, in the order it did them.
//
// They are not in the log. The daemon does not record a bang-command and neither does the terminal,
// so a transcript rebuilt from the log would drop them on the next frame — which arrives two and a
// half times a second. Kept here, appended after whatever the log says, and gone when the page is
// reloaded, which is exactly as long as they are true for.
const localRows = [];
// The last thing the log said, so a shell run can redraw without waiting for the next frame.
let lastRows = [];
// liveNote is what a still-running tool last reported on this companion, or ''.
//
// It arrives on the fleet poll and not on the transcript stream, because it is not in the log: a
// progress note is a transient event on the daemon's own bus, and this process reads files. So it
// is kept here and picked up by the next draw rather than threaded through the frame — a frame
// carrying it would have to be re-sent every three seconds to keep it current, which is a rebuild
// of the whole transcript for one line of text.
let liveNote = '';
// userName is what this companion calls the person, or '' when nobody has renamed them.
//
// A plugin can — an SSO bridge puts the authenticated username there — and the terminal has shown
// it since the hook existed. It is not in the log and never was: it is set in the daemon's memory
// and announced on the daemon's bus, so the console had no way to know and said "user" while the
// window beside it said who they had signed in as. It rides the fleet poll, like the note above.
let userName = '';

// whoWord is the word in the gutter: who said this.
//
// The role is what the server calls the row; the word a person reads is a different thing, and
// spelling them the same is how one of them ends up hard-coded in English on every locale. Three
// of them are not the role at all — the person has a name or is "you", magi's own voice is not
// "system", and the assistant is the companion, which is what the terminal has always called it.
function whoWord(r) {
  // A councillor's row is that councillor's: the gutter says WHO said it, and "council" three
  // times over says only which machinery produced them. The name is also the way in, so a reader
  // presses the thing they were already looking at.
  if (r.who === 'council' && r.member) return r.member;
  if (r.who === 'user') return userName || tr('row.you');
  // Which part of magi wrote it, when the log says: the orchestrator's nudge and a mined spec are
  // different facts, and one word for both is the word that says neither.
  if (r.who === 'system') return r.by || tr('row.system');
  if (r.who === 'assistant') return 'magi';
  return r.who;
}

// shown is what is currently in the log, row by row, beside the nodes showing them.
//
// The transcript is re-sent whole two and a half times a second, and it used to be re-BUILT whole
// just as often: every row of an hour-long session thrown away and made again, markdown and all,
// four hundred times a minute. On a long conversation that is tens of thousands of nodes per
// second, and the page spent its time rebuilding what had not changed.
//
// It cost more than time. A fold is a node, so its open state died with it: you pressed one open
// and 400ms later the frame that replaced it read the per-kind preference back and opened every
// row of that kind — the thing the disclosure comment above says explicitly does not happen.
const shown = {rows: [], nodes: []};

// ── the window ───────────────────────────────────────────────────────────────
// Only the tail of a long transcript is in the page. Everything above it is one empty box as tall
// as the rows it stands in for.
//
// The frame-by-frame rebuild went first (rows are reused now) and offscreen rows stopped costing
// layout and paint (content-visibility), but every row a session ever produced was still a subtree
// in the document — reported from a console after a long day, and it is the count itself that
// hurts: memory, the cost of every query that walks the log, and the browser's own bookkeeping.
//
// The trim happens in CHUNKS, not per row. Sliding the window by one on every arrival would break
// the reuse match at the first row and rebuild the whole window each time, which is worse than
// what it replaces — so it is allowed to grow past the cap by rowSlack and then drops back to it.
const rowCap = 150;   // rows kept in the page at rest
const rowSlack = 50;  // how far past the cap it may grow before trimming, so this is amortised
const rowReach = 100; // rows brought back when the reader arrives at the top of the window
// What a row is assumed to be worth in pixels when it has never been rendered. The first frame of a
// long session drops hundreds of rows that were never in the page, so there is nothing to measure
// and the box above them would be flat — a scrollbar claiming the conversation is one screen long.
// It is the same number the stylesheet gives content-visibility for the same reason, and a row that
// HAS been rendered is remembered at its real height instead.
const rowGuess = 56;
// winFrom is the index (in the transcript) of the oldest row that is in the page, and above is the
// height of everything before it. droppedH remembers each dropped row's height by index so
// bringing rows back subtracts exactly what removing them added.
let winFrom = 0, above = 0;
// keepRows is how many the window is currently willing to hold. It GROWS when the reader asks for
// more and never shrinks while they are on the same companion — without that the next frame's trim
// took back what scrolling up had just brought, and the window snapped shut under the reader.
let keepRows = rowCap;
const droppedH = [];
// The box that stands in for what is not there. Its height is the only thing keeping the scrollbar
// honest, and a reader who drags the scrollbar to the middle of a long session lands in it.
const spacer = document.createElement('div');
spacer.className = 'above';
spacer.setAttribute('aria-hidden', 'true');

// reachUp brings the next chunk of older rows back. Called when the top of the window comes near
// the viewport, which is what "scrolling up" means here.
function reachUp() {
  if (winFrom === 0) return false;
  keepRows += rowReach;
  return true;
}

// same reports whether a row can keep the node it already has. Field by field rather than by
// stringifying: a tool result is eight kilobytes and this runs on every row of every frame.
function same(a, b) {
  return a.who === b.who && a.text === b.text && a.args === b.args && a.out === b.out
    && a.ok === b.ok && a.pending === b.pending && a.at === b.at && a.round === b.round
    && a.member === b.member && a.decision === b.decision && a.exit === b.exit
    && a.tool === b.tool;
}

function draw(rows) {
  const stick = atBottom();
  const want = [...(rows || []), ...localRows];
  // Where the window wants to start, and the two ways it moves.
  //
  // Trimming happens in chunks — allowed to grow rowSlack past the cap and then dropped back to it
  // — because sliding by one on every arrival would break the reuse match at the first row and
  // rebuild the whole window each time, which is worse than what this replaces. Reaching up is the
  // reader asking, so it is honoured immediately.
  const target = Math.max(0, want.length - keepRows);
  if (target > winFrom) {
    if (want.length - winFrom > keepRows + rowSlack) {
      for (let i = winFrom; i < target; i++) {
        const n = shown.nodes[i - winFrom];
        const h = (n && n.offsetHeight) || droppedH[i] || rowGuess;
        droppedH[i] = h;
        above += h;
      }
      winFrom = target;
    }
  }
  // How many rows are coming back this frame. Their height is taken off the box above AFTER they
  // are in the page and can be measured — subtracting what was recorded when they left leaves the
  // estimate's error in the scroll position, and for rows that were never rendered the record is
  // only a guess. Measured: a reader was moved 2,500 pixels by one reach.
  let recovered = 0;
  if (target < winFrom) {
    recovered = winFrom - target;
    winFrom = target;
  }
  const win = want.slice(winFrom);
  // A transcript grows at the end. So the unchanged head is kept as it is, and everything from the
  // first difference on is rebuilt — which for the usual frame is the last row and nothing else.
  // A compaction rewrites history and breaks the match early; that rebuild is correct and rare.
  // Measured before anything moves, for the anchoring at the end.
  const wasAt = window.scrollY, wasTall = document.body.scrollHeight;
  let i = 0;
  while (i < win.length && i < shown.rows.length && same(win[i], shown.rows[i])) i++;
  while (shown.nodes.length > i) log.removeChild(shown.nodes.pop());
  for (let j = i; j < win.length; j++) {
    const n = rowNode(win[j]);
    shown.nodes.push(n);
    log.append(n);
  }
  shown.rows = win;
  if (recovered) {
    let back = 0;
    for (let k = 0; k < recovered && k < shown.nodes.length; k++) {
      back += shown.nodes[k].offsetHeight || droppedH[winFrom + k] || rowGuess;
    }
    above = Math.max(0, above - back);
  }
  // The box above is resized BEFORE the page is measured again. Sized after, the rows brought back
  // had already made the document taller while the box still claimed their old space, and the
  // anchor below then scrolled by that phantom growth — measured, and it threw the reader to the
  // bottom in three frames.
  spacer.style.height = above ? above + 'px' : '';
  if (above && spacer.parentNode !== log) log.prepend(spacer);
  else if (!above && spacer.parentNode === log) log.removeChild(spacer);
  // Anchored: anything that changes height ABOVE the viewport moves everything under it, and a
  // reader mid-sentence would be somewhere else.
  //
  // Not to the pixel, and it cannot be. A row that comes back is offscreen, and an offscreen row
  // under content-visibility reports the intrinsic size the stylesheet gives it rather than what it
  // will actually be — the browser has not laid it out and measuring is what this avoids. Measured
  // over one reach of a hundred rows: 2,500 pixels of drift when the box above was shrunk by what
  // was recorded on the way out, ~600 when it is shrunk by what the recovered rows report now.
  // The rest is what Chrome's own scroll anchoring absorbs as they come into view.
  if (stick) window.scrollTo(0, document.body.scrollHeight);
  else if (i === 0 && document.body.scrollHeight !== wasTall) {
    window.scrollTo(0, wasAt + (document.body.scrollHeight - wasTall));
  }
}

// The reader arriving at the top of the window is the ask for more of it.
//
// On scroll rather than on a control: what is above is not a page of results, it is the same
// conversation, and a button saying "earlier" in the middle of it would be furniture explaining a
// mechanism nobody asked about. The margin is a screen and a half, so the rows are there before the
// empty box is.
addEventListener('scroll', () => {
  if (!above || !spacer.parentNode) return;
  if (spacer.getBoundingClientRect().bottom > -window.innerHeight * 1.5) {
    if (reachUp()) draw(lastRows);
  }
}, {passive: true});

// runShell sends a command to the daemon and shows what it wrote.
async function runShell(cmd) {
  const pending = {who: 'shell', text: cmd, args: tr('shell.running')};
  localRows.push(pending);
  draw(lastRows);
  let r;
  try {
    r = await fetch('/shell' + q(), {method: 'POST', body: new URLSearchParams({cmd: cmd})});
  } catch {
    pending.args = tr('error.unreachable');
    draw(lastRows);
    return;
  }
  if (!r.ok) {
    pending.args = (await r.text().catch(() => '')).trim() || tr('error.unreachable');
    draw(lastRows);
    return;
  }
  const out = await r.json().catch(() => null);
  if (!out) { pending.args = tr('error.unreachable'); draw(lastRows); return; }
  // The exit code is on the summary because it is the answer to "did that work" and the output is
  // the answer to "what happened" — one of those is wanted every time and the other sometimes.
  pending.exit = out.exit;
  pending.args = String(out.out || '').trim() || tr('shell.nooutput');
  draw(lastRows);
}

let es, fleetTimer, boardSub;
function connect() {
  es = new EventSource('/events' + q());
  es.onopen = () => { conn('live'); says(tr('state.live')); };
  es.onmessage = e => { lastRows = JSON.parse(e.data); draw(lastRows); };
  // The daemon outliving this page is normal, and so is the reverse. Reconnect quietly rather
  // than making a restart look like a failure.
  es.onerror = () => { conn('lost'); says(tr('state.reconnecting'));
                       es.close(); if (sock()) setTimeout(connect, 1500); };
}

// ── routing ──────────────────────────────────────────────────────────────────
// Same document either way: entering an agent is a pushState, and the browser's back button lands
// on the fleet without a reload. Every view switch tears the other one's polling down — two of
// them running at once is how a page keeps costing after you leave it.
// paint puts the labels on the parts of the page that are written in the markup rather than built
// by a function. Called once at startup and again whenever the pack changes.
function paint() {
  painted = true;
  // Built once and kept, so it is not redrawn by the thing that redraws the rest of that card.
  if (permField.el) paintPerm(permField.el);
  tabFleet.querySelector('.lbl').textContent = tr('nav.companions');
  tabSkills.textContent = tr('nav.shared');
  document.getElementById('ptabTalk').textContent = tr('panel.talk');
  document.getElementById('ptabState').textContent = tr('panel.state');
  // label, not placeholder. Material Web floats the LABEL into the outline's notch when the field
  // takes focus or holds a value; a placeholder is the grey hint inside an empty one and never
  // moves. Written as placeholders here, the fields had no notch and nothing to float — which is
  // what "the placeholder looks wrong" was. The longer sentence becomes supporting text, which is
  // where an explanation belongs and where it does not have to fit in a notch.
  // Through answerMode, so a language change does not quietly turn the answer field back into the
  // request field while the agent is still waiting on the question above it.
  answerMode(answering);
  const stopBtn = document.getElementById('stop');
  stopBtn.textContent = tr('action.interrupt');
  withMark(stopBtn, '#i-ss-circle-stop');
  railMenu.setAttribute('aria-label', tr('nav.menu'));
  // A secondary tab's indicator spans the tab; a primary tab's hugs its label. The bundle keeps
  // that as a reactive @state with no attribute behind it, so it is set as a property — assigning
  // it re-renders the tab with the indicator on the button instead of on the content.
  for (const id of ['ptabTalk', 'ptabState']) document.getElementById(id).fullWidthIndicator = true;
  // The waiting badge changes parent with the rail, per the spec: on the icon while collapsed,
  // beside the label once there is one.
  sideToggle.setAttribute('aria-label', tr(document.body.getAttribute('side') === 'shut' ? 'side.show' : 'side.hide'));
  tip(sideToggle, tr(document.body.getAttribute('side') === 'shut' ? 'side.show' : 'side.hide'));
  placeRailBadge();
  // Two navigation landmarks on one page have to be told apart, and the label must not repeat the
  // role — a screen reader already says "navigation". Named one at a time rather than swept with a
  // selector: the phrase pack's own test reads literal tr('…') calls to find phrases nobody asks
  // for, and a lookup through a data attribute is invisible to it.
  railEl.setAttribute('aria-label', tr('nav.destinations'));
  document.getElementById('crumbs').setAttribute('aria-label', tr('nav.where'));
  // The label beside it, and the one it announces. Both from the pack: the row in the dialog is a
  // preference like the two below it and has to say which.
  document.getElementById('themeK').textContent = tr('pref.theme');
  themeToggle.setAttribute('aria-label', tr('pref.theme'));
  prefsEl.setAttribute('aria-label', tr('nav.preferences'));
  prefsClose.textContent = tr('action.close');
  withMark(prefsClose, '#i-sl-xmark');
  prefsK.textContent = tr('nav.preferences');
  consoleK.textContent = tr('nav.this_console');
  for (const [el, key] of [[railFleet, 'nav.companions'], [railSkills, 'nav.shared']]) {
    // The word is on the item whether or not it is drawn: collapsed, the icon is all there is to
    // see, and a rail nobody can read aloud is not a navigation. The icon itself is markup and is
    // not touched here — a shape does not need translating, and rebuilding it on every language
    // change would throw away four elements to replace them with the same four.
    el.setAttribute('aria-label', tr(key));
    el.querySelector('.lbl').textContent = tr(key);
  }
  mcpDialogK.textContent = tr('label.add_server');
  mcpCancel.textContent = tr('action.cancel');
  withMark(mcpCancel, '#i-sl-xmark');
  // Closed by hand. A dialog's form closes on the button that submitted it and remembers which one
  // in returnValue — but a custom element is not that button: md-text-button carries form= and
  // value= and neither reaches the native <dialog>, so pressing cancel left it open with an empty
  // returnValue. Measured, after somebody pressed it.
  mcpCancel.onclick = () => mcpDialog.close('cancel');
  // Closed by hand, for the reason written above: form= and value= on a custom element never reach
  // the native dialog, so a cancel that relied on them left it open with nothing decided.
  fmtCancel.onclick = () => fmtDialog.close('cancel');
  fmtGo.onclick = () => { fmtDialog.close('save'); saveFormat(); };
  mcpGo.textContent = tr('action.add_or_replace');
  withMark(mcpGo, '#i-sl-floppy-disk');
  paintChoice(langEl, 'lang');
  if (consoleEl.children.length) loadConsole();   // its two labels are words too
  paintNotify();

  // The lists are drawn by functions, and a function's words are read at draw time. A pack that
  // lands after the list did leaves it in the old language until somebody navigates — measured on
  // the lessons page, which showed an English "forget" beside a Korean "정말?" on the same control.
  //
  // Only the LISTS. The transcript and the detail panel are not repainted, because a pack can land
  // mid-interaction and re-rendering there is what once wiped a panel somebody was reading. A list
  // replaces its own container and nothing else, so redrawing it costs the reader nothing.
  //
  // Guarded on a first paint having happened, or this would run before the loaders are declared.
  if (!repaintable) return;
  if (view() === 'skills') { loadSkills(); loadMCP(); }

  else if (view() === 'board') loadBoard();
  else if (!sock()) loadFleet();
  // The crumb and the tab title are written by render() and are words too. The title is the one a
  // reader sees without looking at the page at all, which makes it the last place worth leaving in
  // a language they did not pick.
  back.textContent = sock() ? SECTION.fleet : (SECTION[view()] || tr('nav.companions'));
  paintDeepCrumb();
  // And the screen under the crumb, when there is one and nothing is being typed into it.
  //
  // A deep screen is drawn once, by render(), and a pack landing a moment later left it in the
  // seeded English while the crumb above it and the rail beside it were Korean — measured on the
  // decision screen, whose whole job is to be read. Redrawn here, once the words exist.
  //
  // Not while somebody is writing. The rule the transcript follows is that a late pack must never
  // take away what a person is in the middle of; the answer field is the only thing on these
  // screens that can hold work, so an empty one is the licence to redraw.
  if (deepIn()) {
    const box = document.getElementById('agentdetail');
    const typed = [...box.querySelectorAll('md-outlined-text-field')].some(f => String(f.value || '').trim());
    if (!typed) {
      const s2 = sock();
      const known = (fleetSeen || []).find(x => x.socket === s2 && (x.peer || '') === peerOf());
      drawDeep(known || {socket: s2, peer: peerOf()});
    }
  }
  retitle(lastWaiting);
}
// True once the page has drawn itself at least once. paint() runs before that on the first pass,
// when the loaders exist but the view has not been decided.
let repaintable = false;

// A select whose options are the same list every time and whose words are not. Rebuilt on a pack
// change rather than translated in place: an md-select-option holds its label as slotted content,
// so there is nothing to reach in and rewrite.
function paintChoice(el, kind) {
  const c = CHOICES[kind];
  el.setAttribute('label', tr(c.label));
  el.replaceChildren(...c.options.map(([v, key]) => {
    const o = document.createElement('md-select-option');
    o.value = v;
    o.append(cell('', tr(key)));
    return o;
  }));
  // Told through the SELECT, and told again once it has rendered.
  //
  // Two things had to be got right here. Marking the option selected on the way in left the field
  // showing the raw value — "dark" where the option said "다크" — because a select reads its
  // display text from the option it CHOSE, and it had chosen nothing: it had been handed children
  // that already claimed to be chosen. That is the same shape as the tabs, where writing the state
  // onto the child instead of asking the parent cost the animation.
  //
  // And the options are custom elements, which are not upgraded in the tick they are appended. A
  // select told its value in that tick has nothing to resolve it against and renders an empty field
  // over a value it is holding — measured: value "dark", displayText "". So it is told once now,
  // for the case where the elements are already upgraded, and again after the render that upgrades
  // them.
  const want = prefOf(kind);
  el.value = want;
  if (el.updateComplete) el.updateComplete.then(() => { el.value = want; });
}

// paint does NOT redraw the view, and that is the whole point. A pack can land at any moment — mid
// fetch, mid interaction — and re-rendering there wipes whatever was on screen: caught here with a
// detail panel that lost its context block because the language arrived during the await. The
// labels written in the markup are repainted; everything drawn by a function reads tr() when it
// next draws, which is soon enough for a word that just changed.

// reveal restarts an entrance animation on an element that has just become visible.
//
// The class has to come OFF and the layout be read before it goes back on: an animation already
// running does not restart because the class is set again, so without the reflow the second visit
// to a destination arrives with no motion at all. Reading offsetWidth is what forces it.
function reveal(el, how) {
  if (!el || el.hidden) return;
  el.classList.remove('enter', 'rise', 'slideL', 'slideR');
  void el.offsetWidth;
  el.classList.add(how || 'enter');
}

// drawnFor is the companion the screen is currently drawn for, so a navigation can tell whether it
// is leaving one.
let drawnFor = '';

// clearCompanionView empties everything on screen that belongs to ONE companion.
//
// Third time for this shape. The pane's cards were a hand-kept list of ids until somebody added a
// card and forgot the list, so the fix was to walk the pane — and the strip in the dock was
// outside that walk, so it stayed up over the fleet list showing the children of a companion you
// had left. A hand-kept list cannot fail a build; what keeps this honest is that there is ONE
// place, and anything drawn for a companion is emptied in it.
//
// It clears the transcript's memory too, not only its nodes: the rows are reused by position now,
// and rows left over from another conversation are rows the next frame would try to keep.
function clearCompanionView() {
  for (const card of document.getElementById('side').children) card.hidden = true;
  for (const el of [stripEl, document.getElementById('prompt'), document.getElementById('detail')]) {
    el.hidden = true;
    el.replaceChildren();
  }
  log.replaceChildren();
  shown.rows = [];
  shown.nodes = [];
  winFrom = 0;
  above = 0;
  keepRows = rowCap;
  droppedH.length = 0;
  lastRows = [];
  localRows.length = 0;
  liveNote = '';
  userName = '';
  sidEl.textContent = '';
}

function render() {
  if (es) { es.close(); es = null; }
  if (fleetTimer) { clearInterval(fleetTimer); fleetTimer = null; }
  if (boardSub) { boardSub.unsubscribe(); boardSub = null; }
  const s = sock();
  const v = s ? '' : view();
  // Where you are, in the masthead: magi / lessons, or magi / companions / design. The crumb that
  // names the section IS the way back to it, so "where am I" and the way out are one thing.
  //
  // It names the SECTION, not always the fleet. A crumb that read "fleet" while you stood in the
  // connections tab answered a question nobody asked and offered a way back to somewhere you had
  // not been.
  const section = s ? tr('nav.companions') : SECTION[v] || tr('nav.companions');
  retitle(0);
  back.textContent = section;
  back.setAttribute('href', at(s ? '' : HREF[v] || ''));
  crumbSep.hidden = !s;
  crumbHere.textContent = s ? nameOf(s) : '';
  // One level in, the companion's own name becomes a way BACK to its conversation and the third
  // crumb says where you are. Without that the only way out of a detail screen is the browser's
  // back button, which is not a control the page put there.
  const deep = deepIn();
  crumbHere.setAttribute('href', s ? at('?d=' + encodeURIComponent(s) + (peerOf() ? '&p=' + encodeURIComponent(peerOf()) : '')) : '');
  crumbHere.className = deep ? '' : 'here';
  crumbSep2.hidden = !deep;
  paintDeepCrumb();
  // Past work has a level of its own inside it — the list, and one session out of it — so the third
  // crumb becomes the way back to the list and a fourth says which session. Without that, opening
  // one left the crumb saying "past work" while showing a transcript, with no way back to the list
  // short of the browser's own button.
  const leaf = pastOn() && pastOf();
  crumbDeep.setAttribute('href', leaf
    ? at('?d=' + encodeURIComponent(s) + (peerOf() ? '&p=' + encodeURIComponent(peerOf()) : '') + '&past=')
    : '');
  crumbDeep.className = leaf ? '' : 'here';
  crumbSep3.hidden = !leaf;
  crumbLeaf.textContent = leaf ? pastOf() : '';
  back.className = s ? '' : 'here';
  tabsEl.hidden = !!s;
  // Which kind of page this is, for the rules that differ between them. On a companion's page the
  // tabs are gone, so anything that leans on them being there has to know.
  document.body.setAttribute('at', s ? 'agent' : 'list');
  document.body.setAttribute('view', s ? '' : v);
  // Which tab is current is asked of md-tabs, not written onto the tabs. Both leave the right tab
  // selected, but only one of them moves: the indicator is animated inside Tabs.activateTab and
  // nowhere else — it measures the outgoing tab's indicator and slides the incoming one from there
  // — and activateTab runs only when the component is the one changing the selection. Setting
  // each tab's active property, which this page did, arrived at the same picture with no motion
  // between the two.
  tabsEl.activeTabIndex = Math.max(0, TABS.indexOf(v));
  // The rail says the same thing the tabs do. A list item has no selected state of its own, so
  // this is an attribute of ours and the stylesheet draws it — said once here rather than at each
  // of the four click handlers, which is how the two used to fall out of step.
  // A companion's page is INSIDE the companions destination, so that is the one that stays lit.
  // Marked by view alone it went dark the moment you opened a row, and the rail then said you were
  // nowhere — on the screen you reach it from most often.
  for (const [el, key] of RAILS) el.toggleAttribute('selected', s ? key === 'fleet' : v === key);
  fleetEl.hidden = !!s || v !== 'fleet';
  summaryEl.hidden = !!s || v !== 'fleet';
  skillsEl.hidden = !!s || v !== 'skills';
  boardEl.hidden = !!s || v !== 'board';
  mcpEl.hidden = !!s || v !== 'skills';
  // Only on a companion's own page. Addressing one by typing its name into a box, from a list where
  // it is already on screen and one click away, is a second way to do the thing the list does — and
  // the harder one: it asks somebody to spell a name they can see.
  // The conversation and everything that acts on it belong to the companion's page, not to a
  // screen about one piece of what happened there. Standing in a verdict, "send" would put a
  // message into a conversation that is not on screen.
  const deepNow = deepIn();
  document.getElementById('agentdetail').hidden = !deepNow;
  streamEl.hidden = !!deepNow;
  f.hidden = !s || deepNow;
  document.getElementById('stop').hidden = !s || deepNow; // nothing to interrupt from the fleet view
  // And the strip under it, when the screen you are on IS the decision. Standing on the decision
  // screen the same question was drawn twice — its report in full above, its report again in the
  // dock, with a button offering to open the screen already open. The dock's copy is the one that
  // exists because the screen might not be.
  if (deepNow) document.getElementById('prompt').hidden = true;
  // Leaving a companion resets the panel: the next one is arrived at for its conversation, and
  // landing on the facts of an agent you just opened is a screen nobody asked for.
  if (!s) panel = 'talk';
  drawPanels();
  // Leaving a companion, or arriving at a different one, empties everything drawn for the last.
  if (!s || s !== drawnFor) clearCompanionView();
  drawnFor = s;
  // Whichever body of content this navigation arrived at. One of them, not all of them: reveal on a
  // hidden element does nothing, so the list is the page's destinations and the right one answers.
  for (const el of [fleetEl, skillsEl, boardEl, mcpEl, streamEl]) reveal(el);
  measureDock();
  if (s && !deepNow) { draw([]); connect(); }
  else if (!s) { conn(''); says(''); }
  if (deepNow) {
    // The fleet poll is still what says which companion this is — the detail needs its socket and
    // its peer — so the row is taken from the last poll when there is one, and asked for when
    // there is not.
    const known = (fleetSeen || []).find(x => x.socket === s && (x.peer || '') === peerOf());
    drawDeep(known || {socket: s, peer: peerOf()});
    return;
  }
  if (v === 'board') {
    // Live, like the fleet beside it. A board that showed the day as it stood when you opened it
    // went stale the moment an agent finished something — and the day you watch it is the day work
    // is happening. rxjs because the page already speaks it, and because the guard belongs in the
    // pipe rather than in a flag somebody has to remember to clear.
    loadBoard();
    boardSub = timer(3000, 3000).pipe(
      switchMap(() => from(boardSig())),
      onlyWhen(Boolean),
      distinctUntilChanged(),
      // A field with the caret in it is a field somebody is using. Rebuilding the header under
      // them would take the focus and the half-typed date with it.
      onlyWhen(() => !boardEl.contains(document.activeElement)),
    ).subscribe(() => loadBoard());
    return;
  }
  if (v === 'skills') {
    // Both halves of the same story, in the order it happens: what has been said often enough to
    // become a rule, then the rules. Not polled — this is read and thought about, and a list that
    // reorders itself under the cursor while somebody decides what to promote is worse than one a
    // minute old.
    //
    // BOTH halves, from here. There used to be a separate v === 'mcp' branch above this one, and
    // it could not run: view() folds mcp into skills (RENAMED), so the test never matched while the
    // element beside it was shown by the same fold. The servers arrived only when something else
    // happened to call loadMCP — a language change, or adding one — which is why the list was there
    // on one visit and empty on the next.
    loadSkills();
    // The server picker names companions, so the fleet is read before the list is drawn.
    fetchList('/fleet').then(list => { if (list) fleetSeen = list; loadMCP(); });
    return;
  }
  // The other two poll: the fleet for its rows, a companion's page for the facts about itself that
  // its log cannot carry.
  loadFleet();
  fleetTimer = setInterval(loadFleet, 3000);
}

// nameOf is the crumb for a socket before the fleet has been fetched — the file name carries the
// workspace's base name, which is what a person calls the agent.
function nameOf(socket) {
  const base = socket.replace(/^.*\//, '').replace(/^daemon-/, '').replace(/\.sock$/, '');
  return base.replace(/-[a-z0-9]{8}$/, '');
}

function go(s, peer) {
  history.pushState({}, '', at(s ? '?d=' + encodeURIComponent(s) + (peer ? '&p=' + encodeURIComponent(peer) : '') : ''));
  render();
}
// The crumb goes where it SAYS it goes. It names the section you are standing in, so sending it to
// the fleet regardless made the label and the click disagree — you would read "lessons" and land on
// companions. render() keeps its href current; this just follows it.
back.onclick = e => {
  e.preventDefault();
  const url = back.getAttribute('href') || '/';
  history.pushState({}, '', url);
  render();
};
// The rail's four, addressed the same way the tabs are. They are md-list-item with an href, so the
// component draws a real anchor: the click is intercepted like every other in-page link, and a
// middle click or a copied address still lands.
const RAILS = [[railFleet, 'fleet'], [railSkills, 'skills']];
for (const [el, key] of RAILS) {
  el.href = at(HREF[key]);
  el.onclick = e => {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button) return;  // let the browser have it
    e.preventDefault();
    // Pressing the destination you are already on scrolls back to the top, which is what the guide
    // asks a re-selected destination to do. Without it the press did nothing at all — the url was
    // already this one — and a control that answers nothing reads as broken.
    if (!sock() && view() === key) { scrollTo({top: 0, behavior: 'smooth'}); return; }
    history.pushState({}, '', at(HREF[key]));
    render();
  };
}

// Widening the rail is a wide-screen idea only. On a phone the rail is not a drawer — it is a
// section at the foot of the page — so there is nothing to open and nothing to close.
// The side pane's own control. Remembered, because a pane you shut should stay shut when you open
// the next companion — reopening it every time would make the button feel like it did nothing.
//
// SHUT unless somebody opened it. It used to be the other way round, and the pane took 352px of the
// best place on the page from the moment there was room for it — at a 900px window that left the
// conversation 44 characters a line, and at 840 it left 37. What is in there is reference: the
// plan, the handoffs, what was intervened in. Things you go and look at, not things you read. The
// conversation is what the page is for.
//
// Stored as the word "open" rather than as an empty string, so the default can be read off the
// absence of a value. An empty string and "never chosen" were the same thing before, which is why
// the default could not be changed without also forgetting everybody's choice.
const sideToggle = document.getElementById('sideToggle');
if (localStorage.getItem('side') !== 'open') document.body.setAttribute('side', 'shut');
// Said once, from the state, rather than written into the markup and hoped to stay true. The
// attribute in the markup is what a screen reader reads before any of this runs, and it was
// "expanded" while the pane was shut — a control announcing the opposite of what it does.
sideToggle.setAttribute('aria-expanded', String(document.body.getAttribute('side') !== 'shut'));
sideToggle.onclick = () => {
  const shut = document.body.getAttribute('side') !== 'shut';
  document.body.setAttribute('side', shut ? 'shut' : '');
  localStorage.setItem('side', shut ? 'shut' : 'open');
  sideToggle.setAttribute('aria-expanded', String(!shut));
  paint();
};

const scrimEl = document.getElementById('scrim');
// Collapsed the badge sits on the icon's upper right; expanded it moves beside the label, which
// is where the spec puts it once there is a label to sit beside. It is a different PARENT, not a
// different offset — and it has to be said from wherever the rail changes width, not only from
// paint(): paint does not run on a nav toggle, so the reparent never happened and a stylesheet
// rule was quietly doing the work with a calc(100% + 9.2rem) nobody could derive. One mechanism.
function placeRailBadge() {
  // With the shape, not with the width: the badge belongs in the row exactly while the item IS a
  // row, which on the way out lasts a quarter second longer than the drawer does.
  const wide = document.body.hasAttribute('nav-wide');
  const home = wide ? railFleet : railFleet.querySelector('.icwrap');
  // Beside the label means AFTER it, in the flow, which is a thing layout already knows how to do:
  // in the item's default slot the badge follows the label's last character in any language, with
  // nothing measured and nothing to keep in step. The version this replaces pushed it with a fixed
  // calc(100% + 9.2rem), and that is where the language dependence came from — the badge stood 52px
  // past an English label and 87px past a Korean one, in the same place both times.
  // The trailing edge (slot="end") was tried and is worse: a count 100px from the word it counts.
  railBadge.removeAttribute('slot');
  if (home && railBadge.parentNode !== home) home.append(railBadge);
}
// Opening and closing are not mirror images, and trying to make them one was the flinch.
//
// The items change SHAPE, not size over time: collapsed a destination is an icon with the word
// beneath it, expanded it is a row, and flex-direction is state rather than motion.
//
// OPENING, the new shape goes on immediately. The rail hides its horizontal overflow, so the row is
// drawn at full width behind the edge and is uncovered as the rail widens — which is the movement
// a drawer is supposed to have.
//
// CLOSING, the same trick is wrong: the row would become a column while the rail is still wide, and
// a centred column in a 235px rail puts the icon at x=117 — measured — so the icons swing right and
// come back. The shape waits for the width instead, and changes once, at the end, in a rail that is
// already narrow.
//
// So nav says how wide, nav-wide says which shape, and only the closing direction separates them.
const RAIL_MS = 250;
let railTimer = 0;
const closeNav = () => {
  clearTimeout(railTimer);
  document.body.removeAttribute('nav');        // the width goes now
  railMenu.setAttribute('aria-expanded', 'false');
  railTimer = setTimeout(() => {
    // Unless it was opened again inside the quarter second, in which case the shape it is wearing
    // is the one it wants.
    if (document.body.getAttribute('nav') !== 'open') {
      document.body.removeAttribute('nav-wide');
      placeRailBadge();
    }
  }, RAIL_MS);
};
scrimEl.onclick = closeNav;
railMenu.onclick = () => {
  if (document.body.getAttribute('nav') === 'open') { closeNav(); return; }
  clearTimeout(railTimer);
  document.body.setAttribute('nav', 'open');
  document.body.setAttribute('nav-wide', '');  // shape first, the clip covers it
  railMenu.setAttribute('aria-expanded', 'true');
  placeRailBadge();
};

// One door to the preferences, at every width. The rail's hamburger is a different thing: it
// widens the navigation, and it no longer opens anything.
prefsEl.onclick = () => prefsDialog.show();
// Painted when it OPENS, not before. A dialog does not render what is slotted into it until then,
// so a select told its value while the dialog was closed had no options to resolve it against and
// showed an empty field over a value it was holding.
prefsDialog.addEventListener('opened', () => { if (painted) paint(); paintNotify(); });
// The toggle writes the SAME preference the select does, so the two are one setting with two
// controls rather than two settings. Pressing it leaves 'system' behind on purpose: asking for the
// other theme is a choice, and pretending it was still deferring to the machine would mean the
// next OS change silently undid it.
themeToggle.onclick = () => {
  localStorage.setItem('theme', showing() === 'dark' ? 'light' : 'dark');
  applyTheme();
};
// Escape narrows it again, the way Escape leaves anything that has been opened.
addEventListener('keydown', e => { if (e.key === 'Escape') closeNav(); });

// A preference is written down and acted on immediately. Language re-fetches rather than reloads:
// the pack is the only thing that changes, and a reload would throw away the transcript.
langEl.addEventListener('change', () => {
  localStorage.setItem('lang', langEl.value);
  loadPack();
});

for (const [el, key] of [[tabFleet, 'fleet'], [tabSkills, 'skills']]) {
  // The href is set as well as the click: a middle-click or a copied link has to reach the same
  // place, and on a project site an absolute one does not.
  el.setAttribute('href', at(HREF[key]));
  // Nothing is prevented here. A tab is a custom element and not a link, so there is no navigation
  // to stop — and md-tabs skips its indicator animation on any click whose default was prevented,
  // which is the second way this page had of standing still.
  el.onclick = () => { history.pushState({}, '', at(HREF[key])); render(); };
}
addEventListener('popstate', render);

async function post(path, body, socket, peer, quiet) {
  // Either half can stand alone: a companion is named by its socket, a console by its peer name,
  // and a global rule on another console has only the second. With neither, the action is about
  // whatever the page is already looking at.
  const parts = [];
  if (socket) parts.push('d=' + encodeURIComponent(socket));
  if (peer) parts.push('p=' + encodeURIComponent(peer));
  const target = parts.length ? '?' + parts.join('&') : q();
  const r = await fetch(path + target, {method:'POST', body});
  if (r.ok) return '';
  // A refusal is not a disconnection. The daemon answered — it answered NO — and painting the
  // connection dot red for that says the console cannot hear a machine it is talking to.
  const why = (await r.text()).trim();
  // Returned so the caller can put it where it belongs. Said out loud only when nobody takes it:
  // a form has a field to hang this on and a fleet action does not.
  if (!quiet) says(why.slice(0, 80));
  return why;
}

const t = document.getElementById('t');
// The field grows itself: it is a component with its own textarea in a shadow root, so measuring
// scrollHeight from out here reads the host and not the text. All that is left to do is re-measure
// the dock, because the transcript reserves whatever the dock is actually occupying.
const grow = () => measureDock();

// The transcript reserves whatever the dock is actually occupying. Its height changes with the
// composer as you type and with the prompt bar appearing, and a guessed constant either wastes a
// screen or hides the last thing the agent said — on a phone, the second.
const dock = document.getElementById('dock');
function measureDock() {
  document.documentElement.style.setProperty('--dock', (dock.offsetHeight || 0) + 'px');
}
if (typeof ResizeObserver === 'function') new ResizeObserver(measureDock).observe(dock);
t.addEventListener('input', grow);
f.onsubmit = e => {
  e.preventDefault();
  const v = t.value.trim(); if (!v) return;
  if (answering) {
    const a = answering;
    t.value = ''; grow();
    post('/answer', new URLSearchParams({call: a.askId, kind: a.askKind, text: v}), a.socket, a.peer)
      .then(loadFleet);
    return;
  }
  // A leading bang runs the rest as a command, where the daemon is. The terminal has read this
  // prefix since it existed, and a console that sent "!ls" to the model as a prompt was quietly
  // doing something else with the same keystrokes.
  if (v.startsWith('!')) {
    const cmd = v.slice(1).trim();
    if (!cmd) return;
    t.value = ''; grow();
    runShell(cmd);
    return;
  }
  // The composer is only on a companion's page, so there is one place the work can go.
  t.value = ''; grow(); post('/submit', new URLSearchParams({text: v}));
};
// Enter sends on a keyboard and inserts a newline on a phone: a soft keyboard's return key is the
// only way to break a line there, and hijacking it leaves no way to write a second paragraph.
const touch = matchMedia('(hover: none)').matches;
t.onkeydown = e => { if (e.key === 'Enter' && !e.shiftKey && !touch) { e.preventDefault(); f.requestSubmit(); } };
document.getElementById('stop').onclick = () => confirmStop(nameOf(sock()), () => post('/interrupt', null));

// The markup's own drawings give way to the baked ones, once, before the first paint. Not on every
// render: these eight elements are in the document from the start and outlive every redraw.
dressIcons();
paint();
render();
repaintable = true;
loadConsole();