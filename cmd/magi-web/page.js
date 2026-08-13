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
  paintTheme();
}

// THEMES is the order the toggle walks, and it starts where the console does. One control with
// three stops rather than a toggle and a select beside it: the two would be one setting with two
// controls, and the one that could not reach "system" was the one people pressed.
// Spelled out rather than built from the value: the pack scanner reads this file for the keys it
// must contain, and a key assembled at runtime is one it cannot see — the label would render as
// its own name the first time somebody looked, in whichever language was missing it.
const THEMES = [
  ['system', 'pref.theme.system'],
  ['light', 'pref.theme.light'],
  ['dark', 'pref.theme.dark'],
];

// paintTheme names the mode on the control, because a cycling button cannot show the stops it is
// not on. The sky says which one is set; the tooltip says what it is called and the label says
// what the button is for.
function paintTheme() {
  // Asked of the document rather than closed over the const below it. applyTheme runs while this
  // module is still evaluating — that is what puts the theme on the page before the first paint —
  // and a const declared further down is not merely undefined then, it throws on being read.
  const btn = document.getElementById('themeToggle');
  if (!btn) return;
  const now = prefOf('theme');
  const key = (THEMES.find(([v]) => v === now) || THEMES[0])[1];
  const said = tr('pref.theme') + ': ' + tr(key);
  btn.setAttribute('aria-label', said);
  if (typeof tip === 'function') tip(btn, said);
  // And in the row itself, because the sky is a picture and a picture cannot say "system" — the
  // supporting line is where somebody reads which of the three is actually set.
  const why = document.getElementById('themeWhy');
  if (why) why.textContent = tr(key);
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

// What this person may do here, or null when nobody has been configured and the answer is
// "everything" — which is what a loopback console with one operator has always been.
//
// This is NOT the check. The server refuses regardless, and it must: hiding is courtesy and the
// gate is security, and a page that confuses the two is how a console somebody called read-only
// turns out to have been writable by anybody who opened the network tab. What this buys is the
// other half — a composer for somebody who may not prompt is a box that swallows what they typed,
// and a button that only ever answers 403 teaches people to distrust the screen.
let myCan = null;
const may = c => !myCan || myCan.includes(c);

// applyMay hides the controls this person has no use for.
//
// Declared in the markup or on the element (data-may="configure") rather than listed here, for the
// reason the route table is a table: a control added later is covered by existing, and the person
// adding it says what it needs at the moment they know.
// mayEl is the same question asked of an element that declares its own requirement. A control
// whose visibility is COMPUTED — the interrupt button is not drawn on the fleet screen at all —
// folds this into that computation, because a pass afterwards is a pass the next layout undoes.
const mayEl = el => !el || !el.getAttribute('data-may') || may(el.getAttribute('data-may'));

function applyMay(root) {
  for (const el of (root || document).querySelectorAll('[data-may]')) {
    // It only ever TAKES AWAY. Written as an assignment it also un-hid things the page had hidden
    // for its own reasons — the interrupt button is not drawn on the fleet screen at all, and a
    // gate that granted as well as refused put it back there.
    if (!may(el.getAttribute('data-may'))) el.hidden = true;
  }
}

// loadMe asks once, at startup, and paints what it learns.
//
// A console with nobody configured answers "everything", so this changes nothing there — which is
// the property that matters: the ordinary single-operator console must not grow a permission model
// it did not ask for.
async function loadMe() {
  const me = await fetchList('/me');
  if (!me) return;                      // unreachable: leave everything drawn, the server still refuses
  myCan = Array.isArray(me.can) ? me.can : null;
  document.body.toggleAttribute('no-read', !may('read'));
  applyMay(document);
  // The composer, and nothing else. Everything on a companion's page is redrawn by the poll a
  // moment later anyway; a full paint() from here hung the page instead — worth knowing rather
  // than working around, so: paint() is for a language pack landing, and it re-enters enough of
  // the page that calling it from a fetch that resolves during startup never came back.
  if (typeof composerReach === 'function') composerReach();
}

// Whose console this is. Not an account — magi has no users to log in — but the two facts that
// answer "am I looking at the right machine": the host it runs on and the config directory it
// reads. A supervisor with three of these open in three tabs has asked that question.
function loadConsole() {
  fetchList('/console').then(c => {
    if (!c) return;
    consoleEl.replaceChildren();
    embedModel = c.embedModel || '';
    // The masthead's copy of the same fact. Written from here rather than fetched again: one read,
    // one answer, and the two places that show it cannot come to disagree.
    whereamiEl.textContent = instanceOf(c.user, c.host);
    // The machine, the directory, and the two builds. The console's own version is the process the
    // reader just loaded; the daemons' is what their companions are actually running, and the two
    // come apart the moment somebody upgrades without restarting anything.
    const daemons = (c.daemons || []).join(', ');
    for (const [k, val] of [['field.host', c.host], ['field.config', c.configDir],
                            ['field.console_version', c.version],
                            ['field.daemon_version', daemons]]) {
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
const notifySwitch = document.getElementById('notifySwitch');
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

// A switch shows the STATE, where the button showed the next action.
//
// The difference is the whole reason it changed: "켜기" on a control that is already on is a
// sentence somebody has to read twice, and the guide keeps switches for exactly this — a setting
// that is on or off, taking effect immediately. So nothing here writes a verb any more; it writes
// whether this browser is subscribed, and the line underneath says why when it cannot be.
async function paintNotify() {
  const why = (key, can) => {
    notifyWhy.textContent = tr(key);
    notifySwitch.toggleAttribute('disabled', !can);
  };
  document.getElementById('notifyK').textContent = tr('notify.k');
  notifySwitch.setAttribute('aria-label', tr('notify.k'));
  // Off, and staying off, for every reason it cannot be on. Set before the checks rather than in
  // each of them: a switch left standing at "on" beside a line explaining why it is impossible is
  // the readout disagreeing with itself.
  notifySwitch.selected = false;
  // The static demo has no console behind it and does not export the worker. Checked first, because
  // every reason below it would be the browser's and this one is the page's.
  if (globalThis.MAGI_DEMO) return why('notify.demo', false);
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) {
    return why('notify.unsupported', false);
  }
  if (!window.isSecureContext) return why('notify.insecure', false);
  if (Notification.permission === 'denied') return why('notify.denied', false);
  const sub = await currentSub();
  notifySwitch.selected = !!sub;
  why(sub ? 'notify.is_on' : 'notify.how', true);
}

notifySwitch.addEventListener('change', async () => {
  // The prompt is asked for FIRST, before anything is awaited. requestPermission needs transient
  // user activation, and an await hands the turn back to the event loop — the activation is spent
  // by the time the call is reached, and it resolves 'default' without ever showing a prompt. That
  // is exactly what "it does not ask for permission" looks like: a button that does nothing.
  //
  // Harmless when a subscription already exists, which is the other thing this button does: a
  // permission already granted resolves immediately and shows nobody anything.
  const asked = 'Notification' in window && Notification.permission !== 'granted'
    ? Notification.requestPermission() : Promise.resolve('granted');
  notifySwitch.toggleAttribute('disabled', true);
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
    // Whatever happened, the switch is redrawn from what IS rather than left where the press put
    // it. A refused permission or a failed subscribe must snap it back: a switch that stays on
    // while nothing is subscribed is the worst version of this control, because it is also the
    // most believable.
    paintNotify();
  }
});

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
    log.hidden = !s;
    // The facts card is not simply "shown on a companion's page" any more: it shares its slot with
    // an open file, and which of the two is showing is the tab strip's answer. Said through
    // showCard so there is one place that decides, rather than three that have to agree.
    if (!s && detailEl) detailEl.hidden = true;
    else showCard();
    return;
  }
  const talk = panel === 'talk';
  log.hidden = !talk;
  sideEl.hidden = talk;
  if (talk) detailEl.hidden = true;
  else showCard();
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
const mapEl = document.getElementById('map');
const meetEl = document.getElementById('meet');
const mcpEl = document.getElementById('mcp');
const accessEl = document.getElementById('access');
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
const whereamiEl = document.getElementById('whereami');
const railFleet = document.getElementById('railFleet');
const railSkills = document.getElementById('railSkills');
const railMeet = document.getElementById('railMeet');
const railAccess = document.getElementById('railAccess');
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
const SECTION_KEY = {fleet: 'nav.companions', skills: 'nav.shared', board: 'nav.board',
                     access: 'nav.access', map: 'nav.map', meet: 'nav.meet'};
const SECTION = new Proxy({}, {get: (_, v) => tr(SECTION_KEY[v] || 'nav.companions')});

const HREF = {fleet: '', skills: '?v=skills', board: '?v=board', access: '?v=access',
              map: '?v=map', meet: '?v=meet'};
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

// shownAgent is the fleet row for the companion on screen, or nothing when the poll has not
// answered yet. The record it carries is where the CURRENT session comes from — the page never
// decides that for itself.
function shownAgent() {
  const s = sock();
  if (!s) return null;
  return (fleetSeen || []).find(x => x.socket === s && (x.peer || '') === peerOf()) || null;
}

// movingTo is the conversation the composer would have to move the companion to before it could
// send, or empty when sending needs no move.
//
// One predicate for the whole question, because there is one rule: a composer is live when the
// session on screen IS the one the record names. It covers the ordinary page (which shows the
// current session and needs no move), a session opened from the board or the dropdown (which
// needs one), and a page whose companion moved away while somebody was reading it — that last one
// used to be the quiet failure, a screen showing one conversation and a box that typed into
// another.
function movingTo() {
  if (!(pastOn() && pastOf())) return '';
  const a = shownAgent();
  if (a && a.session === pastOf()) return '';
  return pastOf();
}
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
// `remote` survives as a state for one case only: a record from a daemon too old to say what its
// companion was doing. "We were told nothing" and "we were told it is idle" are different facts,
// and drawing them the same way would invent the second from the first. Everything else about
// being on another machine is the row's `elsewhere`, not its state.
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
  // The mark it already has goes first. The composer's button changes between a paper plane and a
  // reply arrow every time the companion starts or stops asking something, and it is written as
  // label() then withMark() — label KEEPS the mark so the word can be rewritten without losing it,
  // so a second withMark put the new one in front of the old and the button carried both marks for
  // the rest of the session. Replacing is also what a caller asking for a mark means: one.
  for (const k of [...(btn.children || [])]) {
    if (k.getAttribute && k.getAttribute('slot') === 'icon') k.remove();
  }
  const m = icon(ref);
  if (!m) return btn;
  m.setAttribute('slot', 'icon');
  btn.prepend(m);
  return btn;
}

// withGlass puts the search mark in a field's own leading slot.
//
// Four fields narrow a list by typing — the log search, the board's, and the two on the shared
// destination — and all four are the same act. A field takes its icon in a slot of its own rather
// than as a child, which is why this is not withMark.
function withGlass(f) {
  const g = icon('#i-sl-magnifying-glass');
  if (g) { g.setAttribute('slot', 'leading-icon'); f.append(g); }
  return f;
}

// label sets a control's word without throwing away the mark in front of it.
//
// textContent replaces EVERYTHING a component was given, slotted icon included. Three controls
// learned that the hard way in one afternoon — the two armed ones on the lessons page lost their
// mark on the first write and again on every disarm, and the composer's button lost its one every
// time it changed between "send" and "answer". Written once here so the fourth caller does not
// have to learn it too.
function label(btn, word) {
  const mark = [...(btn.children || [])].find(k => k.getAttribute && k.getAttribute('slot') === 'icon');
  btn.textContent = word;
  if (mark) btn.prepend(mark);
  return btn;
}

// markedKey is a card's heading with the mark that names what the card holds.
//
// The panel is a column of headings in one weight and one colour — scheduled, handed out, queued,
// the report's shape — and finding the one you want meant reading all of them. A mark in front is
// how a column of prose becomes a list you can skim, and it costs nothing where a build has no
// sprite: the heading is then exactly the words it always was.
//
// cls, because the same shape is wanted on the team header, whose two parts are styled as tname
// and thub rather than as a card's k — the marking is the same act either way.
function markedKey(ref, text, cls) {
  // The word goes in as the element's own text and the mark is PREPENDED, rather than both being
  // appended as nodes. A heading is read by anything that asks for its textContent — the tests do,
  // and so does a screen reader taking the accessible name — and split across two child nodes it
  // reads as empty in the fake DOM and as the icon plus the word everywhere else. One of those is
  // a test that lies.
  const k = cell(cls || 'k', text);
  const m = icon(ref, {cls: 'mk hk'});
  if (m) k.prepend(m);
  return k;
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
  if (!a.elsewhere) {
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
  //
  // Otherwise the INSTANCE — account@host — and not the machine. Two accounts on one host are two
  // config directories, two policies and two session stores; their companions cannot touch each
  // other's, and a row naming only the machine said they were one fleet. A daemon too old to say
  // which account it runs as still gives its host, which is what this said before.
  name.textContent = a.peer ? a.peer : (a.instance || a.host || tr('map.here'));
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
  confirmThis({
    head: who ? tr('stop.headline', {name: who}) : tr('stop.headline_plain'),
    body: tr('stop.body'),
    keep: tr('action.keep_running'), keepMark: '#i-sl-play',
    doIt: tr('action.interrupt'), doMark: '#i-ss-circle-stop',
    go: go,
  });
}

// confirmThis is the dialog itself, whichever question is being asked in it.
//
// One element for both, because it is a MODAL: two of them could never be on screen at once, and a
// second copy of the markup is a second place for the dismissive action to drift to the wrong side
// of the confirming one. What differs between the questions is words and two marks, which is
// exactly what this takes.
//
// The shape is the guide's and does not vary: a headline that poses the question concretely rather
// than "Are you sure?", the dismissive action on the LEFT, and a confirming label that says what
// will happen instead of "OK".
function confirmThis(q) {
  stopK.textContent = q.head;
  stopBody.textContent = q.body;
  stopCancel.textContent = q.keep;
  withMark(stopCancel, q.keepMark);
  stopGo.textContent = q.doIt;
  withMark(stopGo, q.doMark);
  // The dismissive action can have work to do as well: a control that has already moved — a menu
  // that switched to the branch somebody then decided against — has to be put back, and only the
  // caller knows what back is.
  stopCancel.onclick = () => { stopDialog.close('cancel'); if (q.onKeep) q.onKeep(); };
  stopGo.onclick = () => { stopDialog.close('go'); q.go(); };
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
  // The four states a LOCAL companion moves between, and nothing for the ones elsewhere.
  //
  // There was a fifth tile counting them, and it filtered on a fact the row already carries in its
  // host column — sam@studio, you@buildbox. Worse, it was the one tile that could not be a state:
  // nothing here dials a companion on another instance, so "elsewhere" says where it is and
  // nothing at all about what it is doing, sitting in a row of chips that all mean what something
  // is doing. Two ways to say where, and none of them the thing the tile row is for.
  const counts = {waiting: 0, working: 0, idle: 0, gone: 0};
  // Counted only where somebody here dialled it. A companion elsewhere reports its own state
  // through gossip, which is worth showing on its row — and putting it in a tally beside four
  // numbers this console measured would mix a reading with a sighting a minute old.
  for (const a of list) {
    if (a.elsewhere) continue;
    counts[GROUP[a.state] || 'idle']++;
  }
  box.replaceChildren(...Object.entries(counts)
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
  // The ways OUT of this list, in one place and all the same shape.
  //
  // They were not. The board was an icon button here beside the counting chips; the map was a text
  // button in a bar of its own above the table; the meetings screen arrived and took a third
  // shape. Three controls that do one kind of thing — leave this list for another view of the same
  // fleet — drawn three ways in three places, so nothing about any of them said they were
  // siblings.
  //
  // Icons, not words: the row they sit in is four counting chips, and a word-shaped control reads
  // as a fifth count. Each keeps its name in the tooltip and its aria-label, because an icon alone
  // is a guess for anybody who has not pressed it once. Drawn paths as the fallback and the baked
  // sprite where there is one, the same bargain the markup's icons strike — see dressIcons.
  //
  // …and only when there is something to look at. On a machine with no companions the board can
  // never have held anything and a map is a box with a box in it — a control that can be pressed
  // to reach a blank screen is worse than one that is not there, the same rule the zero tiles
  // above already follow.
  const local = list.filter(a => !a.elsewhere && !a.peer).length;
  const ways = [
    ['nav.board', '#i-sl-chart-kanban', HREF.board, list.length > 0,
     '<path d="M4 5.5h5v13H4zM9.5 5.5h5v8h-5zM15 5.5h5v10.5h-5z" fill="none" ' +
     'stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/>'],
    ['nav.map', '#i-sl-share-from-square', HREF.map, list.length > 1,
     '<path d="M12 4.2a2 2 0 1 1 0 4 2 2 0 0 1 0-4M6 15.8a2 2 0 1 1 0 4 2 2 0 0 1 0-4M18 15.8a2 ' +
     '2 0 1 1 0 4 2 2 0 0 1 0-4M12 8.2v3.6M12 11.8H6v4M12 11.8h6v4" fill="none" ' +
     'stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/>'],
    // The meetings screen is a destination in the rail as well. Here too because the rail is not
    // drawn at all below 600px, and because this is where the participants are: the act belongs
    // beside the list you would pick them from.
    ['nav.meet', '#i-sl-comments', HREF.meet, local > 1 && mayEl(meetEl),
     '<path d="M9.5 4h6.8A2.7 2.7 0 0 1 19 6.7v4.1a2.7 2.7 0 0 1-2.7 2.7H15l-3 2.6v-2.6H9.5a2.7 ' +
     '2.7 0 0 1-2.7-2.7V6.7A2.7 2.7 0 0 1 9.5 4M5 9.4v5.9a2.7 2.7 0 0 0 2.7 2.7H9v2.4l2.8-2.4h2" ' +
     'fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" ' +
     'stroke-linejoin="round"/>'],
  ];
  let first = true;
  for (const [key, ref, href, when, path] of ways) {
    if (!when) continue;
    const b = document.createElement('md-icon-button');
    // The first of them takes the space; the rest sit beside it. Said of the group rather than of
    // one control, so removing whichever happens to be first does not strand the row.
    b.className = 'toview' + (first ? ' lead' : '');
    first = false;
    tip(b, tr(key));
    b.setAttribute('aria-label', tr(key));
    b.innerHTML = '<svg data-i="' + ref + '" viewBox="0 0 24 24" width="20" height="20" ' +
      'aria-hidden="true">' + path + '</svg>';
    dressIcons(b);
    b.onclick = () => { history.pushState({}, '', at(href)); render(); };
    box.append(b);
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
function arm(btn, word, act) {
  let armed = false, timer = 0;
  // label(), not textContent: the word changes twice per press and the mark in front of it must
  // survive both.
  label(btn, word);
  const reset = () => { armed = false; btn.className = btn.className.replace(' armed', ''); label(btn, word); };
  btn.onclick = () => {
    if (armed) { clearTimeout(timer); reset(); act(); return; }
    armed = true;
    btn.className += ' armed';
    label(btn, tr('action.confirm'));
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

// textAnswer is a field and a send button that carry one typed reply.
//
// Its own function because it is now the SECOND way to answer a question rather than the only one:
// a question with a list of options is answered by pressing one, and this is what "none of those"
// opens. Written twice, the two would drift on the details that matter — the disabled state, and
// the clicks that must not reach the row underneath.
function textAnswer(send) {
  const i = document.createElement('md-outlined-text-field');
  i.label = tr('label.answer');
  const b = document.createElement('md-filled-button'); b.textContent = tr('action.answer');
  withMark(b, '#i-ss-paper-plane');
  // Disabled until there is something to send, rather than pressable and inert. The guide is
  // explicit that an action which cannot happen is DISABLED and not hidden, and the third state
  // — drawn as pressable and then doing nothing — is the one it does not offer: a press that
  // answers nothing reads as broken, and there is no way to tell it from a page that has died.
  const arm = () => b.toggleAttribute('disabled', !i.value.trim());
  arm();
  const go = e => { e.preventDefault(); e.stopPropagation(); if (i.value.trim()) send(i.value.trim()); };
  b.onclick = go;
  i.addEventListener('input', arm);
  // The box sits inside a row that is a link, and inside the ask screen it sits under one. Neither
  // press is a navigation.
  i.onclick = e => { e.preventDefault(); e.stopPropagation(); };
  i.onkeydown = e => { if (e.key === 'Enter') go(e); };
  return [i, b];
}

// answerBox is the reply to a blocked agent, next to the question it answers.
//
// The buttons stop the click from opening the agent (the row is a link) — reading and answering are
// different intentions and the same tap must not do both.
function answerBox(a, freeText) {
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
  if (a.askKind === 'question' && (a.askOptions || []).length) {
    // A set of choices, centred, because that is what the row IS. Left-aligned it reads as a
    // toolbar — a strip of things you may do to the thing above — and these are not actions on the
    // question, they are the answer to it. Centred they read as one group being offered, which is
    // also how they sit under the report on the ask screen.
    box.classList.add('choices');
    // The agent offered a list, so the answer is one of these and typing a fourth thing is not an
    // answer to what was asked. The console drew a free-text box regardless — asking somebody to
    // retype an option they could see in the terminal and could not see here, and to guess its
    // exact wording, since the answer travels as text.
    //
    // Outlined, not filled tonal: the three permission buttons are the console's highest-stakes
    // control and are drawn at that weight, and a choice between design options is not that. They
    // are all one weight as a group, though, for the same reason those are — a console that
    // emphasised one option would be answering for the person.
    for (const opt of a.askOptions) {
      const b = document.createElement('md-outlined-button');
      b.textContent = opt;
      b.onclick = e => { e.preventDefault(); e.stopPropagation(); send(opt); };
      box.append(b);
    }
    // A list is what the agent expects and not all it will take: the answer travels as text, so
    // something outside the list is deliverable and is sometimes the true answer ("neither — the
    // column is nullable"). It is behind a press rather than beside the options, because a field
    // standing open next to three buttons asks "type something" as loudly as they ask "pick one",
    // and the list is the offer.
    if (freeText) {
      const more = el('md-text-button', tr('ask.other'));
      more.onclick = e => {
        e.preventDefault(); e.stopPropagation();
        const [i, b] = textAnswer(send);
        more.replaceWith(i, b);
        i.focus();
      };
      box.append(more);
    }
  } else if (a.askKind === 'question') {
    box.append(...textAnswer(send));
  } else {
    // "always" is not a mode and does not touch one: it grants THIS tool for THIS session, in the
    // daemon's memory, and the approval mode is exactly where it was. The label said "Always",
    // which reads as a promise about every tool and every run — the terminal's own list is
    // clearer, because it puts the project rule beside it as a separate choice.
    // The mark matters more here than anywhere else on the page, and it is the last place that got
    // one. Three buttons the same size, the same colour and the same weight, told apart only by a
    // word in whichever language the pack is in — and the press is not undoable. A tick, a barred
    // circle and a struck-through bell are apart at a glance and stay apart when the words are
    // long: "그만 묻기" and "허용" are three characters and two, side by side, at speed.
    // `key`, not `label`: label() is the helper that writes a word without throwing away the mark
    // in front of it, and a loop variable of that name would shadow it exactly where it is needed.
    // Four, since the terminal has always had four and this console offered three: `persist` is
    // the one that OUTLIVES the session, and leaving it out meant the only way to stop being asked
    // after a restart was to go and find a terminal. It writes into this companion's own settings
    // — not the workspace, which is the team's — so the grant is "here, on this machine".
    for (const [key, decision, mark] of [['action.allow', 'allow', '#i-sl-check'],
                                         ['action.always', 'always', '#i-sl-bell-slash'],
                                         ['action.keep', 'persist', '#i-sl-floppy-disk'],
                                         ['action.deny', 'deny', '#i-sl-ban']]) {
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
      const b = label(withMark(document.createElement('md-filled-tonal-button'), mark), tr(key));
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
    box.hidden = true; box.replaceChildren(); promptWasUp = false;
    // And the composer goes back to being the composer. It is one field in two roles, and the
    // role was set when the question appeared and cleared nowhere: interrupt the turn, or let
    // somebody else answer from their own console, and the box in front of you still said "your
    // answer" and still posted to /answer — with the call id of a question that no longer exists.
    // Typing a fresh request there went nowhere and looked like the page had stopped listening.
    answerMode(null);
    measureDock();
    return;
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
  //
  // A question that came WITH a list is the exception, and was the worst of both: the options
  // reached the page — the report argues about them, since the default contract asks what each one
  // costs and which one the agent leans to — and the only way to answer was to retype one of them
  // into the composer, exactly, from memory of prose scrolled off the top. The list is drawn here
  // and the composer stays what it was, which makes it the other half of the same offer: press one,
  // or write something that is not on it.
  if (a.askKind !== 'question' || (a.askOptions || []).length) inner.append(answerBox(a, false));
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
  note.textContent = a ? tr((a.askOptions || []).length ? 'answer.or_pick' : 'answer.instead') : '';
  note.hidden = !a;
  // The word AND the mark, and both change with the mode: a paper plane for putting something into
  // the conversation, a reply arrow for answering the question above it.
  const send = document.getElementById('send');
  label(send, tr(a ? 'action.answer' : 'action.send'));
  withMark(send, a ? '#i-sl-reply' : '#i-ss-paper-plane');
  // The old text was addressed at magi and the new question is not the same subject. Carrying it
  // over would put a half-written request in front of somebody as though it were their answer.
  if (!!a !== wasAnswering) { t.value = ''; }
  wasAnswering = !!a;
  composerReach();
}

// composerReach says which conversation the box in front of somebody would reach, and takes it
// away when it would reach none.
//
// Two states beyond the ordinary one, and both were failures before this existed. Standing in a
// session the companion is not in, the field looked exactly like the live one and typing into it
// sent the words to whichever conversation the companion happened to be in — off screen. And a
// companion mid-turn cannot be moved at all, so a box that accepted a sentence there was taking
// something it could not deliver.
function composerReach() {
  // Two capabilities meet at one box. Answering what the agent is blocked on and giving it new
  // work are different permissions on purpose — somebody trusted to unblock a companion is not
  // necessarily somebody who decides what it works on — and this is the control both would use.
  // So it stays for either, and refuses in the mode the person has no capability for.
  const canAnswer = may('answer'), canPrompt = may('prompt');
  f.hidden = f.hidden || (!canAnswer && !canPrompt);
  if (answering ? !canAnswer : !canPrompt) {
    t.toggleAttribute('disabled', true);
    document.getElementById('send').toggleAttribute('disabled', true);
    t.setAttribute('label', tr(answering ? 'may.not_answer' : 'may.not_prompt'));
    return;
  }
  const to = movingTo();
  const a = shownAgent();
  // Idle is the daemon's state, not the session's: a session that is not current is never running,
  // and what has to be still is the companion. The same predicate the dropdown greys itself with.
  const idle = !a || a.state === 'idle' || a.state === 'stopped';
  const blocked = !!to && !idle;
  const sendBtn = document.getElementById('send');
  t.toggleAttribute('disabled', blocked);
  sendBtn.toggleAttribute('disabled', blocked);
  if (!to) return;                       // the ordinary page: answerMode has already said its part
  t.setAttribute('label', blocked ? tr('move.busy') : tr('move.into', {to: to}));
  const note = document.getElementById('cnote');
  note.textContent = blocked ? tr('move.busy_why') : tr('move.will_ask');
  note.hidden = false;
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
  withGlass(input);
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
  const find = withGlass(document.createElement('md-outlined-text-field'));
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
        // The companion itself travels with the card, not just its name. Looked up again later by
        // name, a lane holding two companions called the same thing — this console's `docs` and a
        // federated one's — resolved every card to whichever came first, so a card for the remote
        // session addressed the local daemon. That daemon then refused a conversation that is not
        // in its workspace, correctly, and the console reported it as a machine that could not be
        // reached.
        if (dayOf(h.started) <= boardDay && dayOf(h.ended) >= boardDay) {
          work.push({...h, who: who.name, owner: who});
        }
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
      const owner = h.owner || a;
      // Into the SESSION, not just the companion. A card on this board IS one conversation — it
      // has an id, a title, a start and an end — and pressing it used to land on whatever that
      // companion is doing now, throwing away the one thing the card knew. The address carries the
      // session too, so a middle click and a copied url open the same conversation.
      const what = document.createElement('a');
      what.className = 'wwhat';
      what.href = href(owner) + (h.id ? '&past=' + encodeURIComponent(h.id) : '');
      what.textContent = h.title || tr('history.untitled');
      what.onclick = e => {
        e.preventDefault();
        go(owner.socket, owner.peer);
        if (h.id) goDeep('past', h.id);
      };
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
    // The workspace beside the conversation. Redrawn on the poll like everything else on this
    // page: a file appearing in a directory while somebody watches is the thing a tree is for.
    lastDrawnFor = mine;
    // The tree, unless somebody is looking at search results — a poll that redrew the tree under a
    // reader every three seconds would take their results away mid-scroll.
    if (!findQ.trim()) loadTree(mine);
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
    // An empty list and a list you may not see are different facts, and the same blank screen was
    // both. Somebody a gateway let in but nobody gave a role to was shown "no companions yet" —
    // which is a lie about the fleet, and sends them looking for a daemon to start.
    if (!here) {
      fleetEl.append(may('read')
        ? emptyState('empty.no_agents', 'empty.no_agents_how')
        : emptyState('may.nothing', 'may.nothing_how'));
    }
    return;
  }
  // Trouble first, then movement, then quiet, then gone; most recently active within each. A list
  // you have to read to find the problem is a list that hides it.
  const rows = list
    // A tile counts what this console dialled, so it filters the same set: a companion elsewhere
    // reports its own state and is not part of the tally that chip is showing.
    .filter(a => !filter || (!a.elsewhere && GROUP[a.state] === filter))
    // Elsewhere last, whatever it says it is doing. A remote companion waiting on a person is
    // waiting on somebody at ANOTHER console — sorting it above the local work would put another
    // team's problem at the top of this one's screen.
    .sort((x, y) => (!!x.elsewhere - !!y.elsewhere) ||
                    (ORDER[x.state] - ORDER[y.state]) || (x.idle - y.idle));
  // Whatever is no longer in the fleet is no longer worth remembering: a companion that was shut
  // down should not leave its row in a map that grows for the life of the tab.
  const alive = new Set(list.map(a => (a.peer || '') + ' ' + a.socket));
  for (const key of [...shownCards.keys()]) if (!alive.has(key)) shownCards.delete(key);
  // The ways to the other views of this destination sit in the summary row with the board's, where
  // they are one row of matching controls rather than three shapes in three places. See summarise.
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
  h.append(markedKey('#i-sl-people-group', name || tr('team.none'), 'tname'));
  // Every companion claiming to speak for the team, not the first one found. Two is a
  // misconfiguration — a team answers with one voice or the question of who answers is open — and
  // naming one of them would draw a settled team over an unsettled one.
  const hubs = members.filter(a => a.hub).map(a => a.name);
  if (hubs.length) {
    // A crown, because "who answers for this team" is the one fact on the header that is about
    // rank rather than about count, and the sentence beside it is long enough to be skipped.
    h.append(markedKey('#i-sl-crown', tr('team.spoken_for', {name: hubs.join(', ')}), 'thub'));
  }
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
  // Changing how a companion runs is `configure`; reading which mode it is on is not. So the field
  // is drawn and disabled rather than removed: a viewer who cannot see the approval mode cannot
  // tell a companion that stops for everything from one that stops for nothing.

  const f = cell('f');
  f.append(cell('k', tr('field.permission')));
  const v = cell('v');
  let sel = permField.el;
  if (sel) sel.toggleAttribute('disabled', !may('configure'));
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
  // Drawn and disabled rather than removed: a reader who cannot see which approval mode a
  // companion is on cannot tell one that stops for everything from one that stops for nothing.
  sel.toggleAttribute('disabled', !may('configure'));
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
  const key = (a.peer || '') + '\0' + a.socket;
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
  sel.toggleAttribute('disabled', !may('configure'));
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
  const same = (sel._painted || []).join('\0') === want.join('\0');
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
    field('field.host', (a.instance || a.host || tr('map.here')) + (a.addr ? ' · ' + a.addr : '') +
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
  // The caret is a chevron the stylesheet turns 90° when the panel shuts, so it has to be one
  // element either way — iconOr returns a node in both builds and the rotation is on .caret.
  const caret = iconOr('#i-sl-chevron-down', '▾', 'caret');
  bar.append(caret, cell('k', tr('field.facts')),
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
    // The report format sits with them rather than in a row of its own. It is the same kind of
    // thing they are — something about this companion you go and look at — and as a row it cost
    // the card a whole line to show three words that change about as often as the model does.
    {
      const b = el('button', tr('field.report_format'));
      b.type = 'button';
      b.className = 'deeper hit48';
      b.onclick = () => openFormat(a);
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
  // Shown unless something else is standing in its place. This card is rebuilt by the fleet poll
  // every three seconds, and an unconditional `hidden = false` here put it back over an open file
  // on every one of them — the file stayed in the DOM underneath and the card kept reappearing on
  // top of it. Whatever the tab strip says is what shows; see showCard().
  showCard();
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
  // With the free-text path, which on this screen is the only one there is: a deep screen hides the
  // composer, so an answer outside the list would otherwise mean going back to say it.
  act.append(answerBox(mine, true));
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

// showSide draws or withdraws one of the pane's cards, and tells the control that opens the pane.
//
// One funnel rather than five `box.hidden = …`, because the fact the control needs is not about
// any one card: it is whether ANY of them has something in it. Five assignments would be five
// places to remember, and the one that got forgotten would leave a button that opens an empty
// column — which reads as broken, not as empty.
function showSide(box, on) {
  if (!box) return;
  box.hidden = !on;
  refreshSideToggle();
}

// refreshSideToggle disables the control when the pane it opens has nothing in it.
//
// A control that does something invisible is one somebody presses twice and then stops trusting.
// Disabled rather than hidden: the pane comes and goes with what a companion is doing, and a
// button that disappears from the masthead moves everything beside it.
function refreshSideToggle() {
  if (typeof sideToggle === 'undefined' || !sideToggle || !sideEl) return;
  // Array.from, because children is an HTMLCollection in a browser and an HTMLCollection has no
  // .some — the page would throw here, and this runs inside paint(), so everything paint() had not
  // reached yet stayed blank. That is how the rail lost its labels: not a styling problem, an
  // exception three lines above the loop that writes them.
  let any = false;
  for (const c of Array.from(sideEl.children || [])) {
    if (!c.hidden) any = true;
  }
  sideToggle.disabled = !any;
  // What it would do, or why it will not. Said on the tooltip and to a screen reader, because a
  // greyed-out control with no explanation is the least useful state a control can be in.
  const word = !any ? 'side.nothing' : (document.body.getAttribute('side') === 'shut' ? 'side.show' : 'side.hide');
  sideToggle.setAttribute('aria-label', tr(word));
  tip(sideToggle, tr(word));
}

async function drawPlan(a) {
  const box = document.getElementById('plan');
  const todos = await fetchList('/plan' + qFor(a));
  if (!todos || !todos.length) { showSide(box, false); box.replaceChildren(); return; }
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
  showSide(box, true);
}

// ── what it handed to the others ─────────────────────────────────────────────
// A companion answers in its own transcript and the asker reads it — cheap and honest, and it
// leaves somebody clicking through five pages to find out whether the work is done. This is that
// walk, done once, under the transcript of whoever handed it out.
async function drawHandoffs(a) {
  const box = document.getElementById('handoffs');
  const list = await fetchList('/handoffs' + qFor(a));
  if (!list || !list.length) { showSide(box, false); box.replaceChildren(); return; }
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
  box.replaceChildren(markedKey('#i-sl-share-from-square', tr('field.handed_out')), ...rows);
  showSide(box, true);
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
  if (!list || !list.length) { showSide(box, false); box.replaceChildren(); return; }
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
  box.replaceChildren(markedKey('#i-sl-calendar-clock', tr('field.scheduled')), ...rows);
  showSide(box, true);
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
  if (!a) { showSide(intervenedEl, false); intervenedEl.replaceChildren(); return; }
  const list = await fetchList('/interventions');
  if (!list) return;
  const mine = list.filter(m => m.socket === a.socket && (m.peer || '') === (a.peer || ''));
  if (!mine.length) { showSide(intervenedEl, false); intervenedEl.replaceChildren(); return; }

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
  showSide(intervenedEl, true);
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
  const f = withGlass(document.createElement('md-outlined-text-field'));
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
// Who may use this console, and how much.
//
// A row per person, and the two things a row can be changed to: a different role, or a narrower
// set of companions. The capabilities are drawn beside the role because a role NAME is a promise
// and the capabilities are the promise itself — reading them together is how somebody notices that
// "responder" does not include what they assumed.
//
// The screen is drawn only for an admin (the markup carries data-may), and the server refuses
// regardless: hiding is for a person who would otherwise be offered a control that answers 403.
async function loadAccess() {
  // fetchList is the one fetch helper: it decodes whatever JSON came back, array or object, and
  // answers null when the server did not — see its note on why a refusal is not an exception.
  const got = await fetchList('/access');
  if (!got) return;
  const roles = (got.roles || []).map(r => r.name);
  const head = sectionHead('nav.access', addPersonButton(roles));
  // Whose list this is, before the list. Drawn on both branches: "nobody is listed" is a statement
  // about one instance too, and on a page that shows companions from several machines it was the
  // branch most likely to be read as a statement about all of them.
  const whose = instanceLine(got.instance);
  if (!got.configured) {
    // Not an empty table: a console with nobody listed is the one-operator console, and which of
    // the two this is answers "was my file read".
    accessEl.replaceChildren(head, ...whose, emptyState('access.nobody', 'access.nobody_how'));
    return;
  }
  // Groups first, because on a console wired to a directory they are the roster and the people
  // below them are the exceptions — one person given something their group does not have. Drawn
  // read-only: membership is maintained where somebody is hired and let go, and a console that
  // offered to edit it would be offering to disagree with the directory.
  const kids = [head, ...whose, cell('accsay', tr('access.lead'))];
  // Narrowed to one capability when a chip is pressed, and said in words above the lists: a filter
  // that only shows itself as a chip somewhere in the middle of a row is a screen that has quietly
  // stopped being the whole roster.
  const has = row => !capFilter || (row.can || []).includes(capFilter);
  if (capFilter) kids.push(capNote());
  const groups = (got.groups || []).filter(has);
  const people = (got.people || []).filter(has);
  if (groups.length) {
    kids.push(rosterHead('access.groups', 'access.groups_why'),
              accList(groups.map(g => groupRow(g))));
  }
  if (people.length || !capFilter) {
    kids.push(rosterHead('access.exceptions', 'access.exceptions_why'),
              accList(people.map(p => personRow(p, roles))));
  }
  // What the words on the chips mean, once, under the thing they are on — reference goes below
  // what it explains. Once rather than per row: seven sentences repeated beside every person is a
  // screen nobody reads, and the same seven under it is a screen somebody reads once.
  kids.push(rosterHead('access.legend'), capLegend(everyCap(got)));
  accessEl.replaceChildren(...kids);
}

// everyCap is every capability this console knows about, in the order the roles list them rather
// than alphabetically: the roles are written from least to most, and the legend reading that way
// says something the alphabet cannot.
function everyCap(got) {
  const seen = [];
  for (const r of (got.roles || [])) {
    for (const c of (r.can || [])) if (!seen.includes(c)) seen.push(c);
  }
  return seen;
}

// Literal keys in a lookup, not a key built by concatenation: a key the pack check cannot see is
// the one that ships missing and renders as its own dotted name. It also decides what happens to a
// capability this page has never heard of — nothing, rather than a dotted key beside a chip.
const CAP_SAY = {
  read: 'cap.read', answer: 'cap.answer', prompt: 'cap.prompt', curate: 'cap.curate',
  configure: 'cap.configure', admin: 'cap.admin', shell: 'cap.shell',
};

// What the screen is showing while a chip is pressed, and the way back to all of it.
function capNote() {
  const box = cell('capnote');
  box.append(cell('', tr('access.only', {cap: capFilter})));
  const all = label(withMark(document.createElement('md-text-button'), '#i-sl-xmark'),
                    tr('access.show_all'));
  all.onclick = () => { capFilter = null; loadAccess(); };
  box.append(all);
  return box;
}

function capLegend(caps) {
  const box = cell('caplegend');
  for (const c of caps) {
    if (!CAP_SAY[c]) continue;
    const row = cell('capdef');
    row.append(capChip(c), cell('capsay', tr(CAP_SAY[c])));
    box.append(row);
  }
  return box;
}

// A subheading over a list, which is what the two halves of this screen needed and did not have.
//
// title-small on the muted role, per the type scale: it is a heading and it is the smallest one —
// the section already has the page's h2, and a second heading at the same weight would say the two
// are peers. h3 because it IS one: assistive tech navigates by heading, and these are the two
// landmarks on this screen.
function rosterHead(key, whyKey) {
  const h = document.createElement('h3');
  h.className = 'rosterhead';
  h.textContent = tr(key);
  // A sentence under the heading when the heading needs one. Both of these do: "groups" and
  // "exceptions" name the two halves without saying why a console has both, and that is the part
  // somebody arriving at this screen for the first time does not know.
  if (whyKey) {
    const say = document.createElement('span');
    say.className = 'why';
    say.textContent = tr(whyKey);
    h.append(say);
  }
  return h;
}

// Which capability the roster is narrowed to, or null for all of them.
//
// One filter for the whole screen, which is what makes a chip in a row the same control as the
// chip with the same word three rows down: press "configure" anywhere and the question being asked
// is the same one — who here can configure things. The guide's rule that a page's chip sets are
// all one selection mode falls out of there being one selection.
let capFilter = null;

// A capability, as the component rather than a div wearing its shape.
//
// It was a div, on the argument that every chip variant the library ships is a control and a
// capability does nothing when pressed. The argument was right about chips and wrong about this
// one: pressing a capability CAN mean something, and it is the question somebody reading this
// screen already has — "who else can do this?". So it is a filter chip, filtering the roster, which
// is exactly what filter chips are for. The selected state is real, shared, and says what is being
// asked; a person who presses one and gets the answer never has to learn a second control.
function capChip(word) {
  const c = document.createElement('md-filter-chip');
  c.className = 'capchip';
  c.setAttribute('label', word);
  c.setAttribute('data-cap', word);
  // The chip's own property, not an attribute of ours: it toggles itself and the list is rebuilt
  // from capFilter, so the drawing and the state cannot drift.
  c.selected = capFilter === word;
  c.onclick = () => {
    capFilter = capFilter === word ? null : word;
    loadAccess();
  };
  return c;
}

// The list itself is a container, so the rows inside it are spaced with a gap rather than ruled
// off from one another — the lists guidance keeps dividers for uncontained or complex lists and
// asks for gap otherwise. The people below carry a form each, which is the "complex" case, and
// they say so with a hairline of their own.
function accList(rows) {
  const box = cell('acclist');
  box.append(...rows);
  return box;
}

// One row of the roster, in list anatomy: who it is on the headline, what that buys underneath,
// and the scope after it. Groups and people are built by the same two helpers so a field never
// moves between one row and the next — which is the one thing the lists guidance asks of a list
// that mixes two kinds of item.
function whoLine(who, trailing) {
  const line = cell('accwho');
  line.append(cell('who', who));
  if (trailing) line.append(trailing);
  return line;
}

function capsLine(can, companions) {
  const box = cell('acccaps');
  // The capability words are NOT translated: they are what goes into auth.toml, and a screen that
  // showed one word while the file wanted another would teach somebody the wrong name for the
  // thing they are editing.
  const caps = cell('caps');
  for (const c of (can || [])) caps.append(capChip(c));
  box.append(caps);
  // A group's scope stays on the line, because a group has no sub-section: it is read-only here —
  // membership belongs to the directory — and a section with nothing to press in it is a heading.
  if (companions && companions.length) {
    box.append(cell('scope', tr('access.scoped', {list: companions.join(', ')})));
  }
  return box;
}

function groupRow(g) {
  const row = cell('acc');
  row.append(whoLine('@' + g.who, cell('role', g.role)), capsLine(g.can, g.companions));
  return row;
}

// instanceOf is user@host, or whichever half a console could tell us. The same shape the server
// builds for the access screen, and the same reasoning: the pair is what everything here belongs
// to, and half of it is better than a guess at the other half.
function instanceOf(user, host) {
  if (user && host) return user + '@' + host;
  return host || user || '';
}

function instanceLine(inst) {
  if (!inst || (!inst.who && !inst.configDir)) return [];
  const line = cell('instance');
  if (inst.who) {
    const b = document.createElement('b');
    b.textContent = inst.who;
    line.append(b);
  }
  if (inst.configDir) line.append(document.createTextNode((inst.who ? '  ·  ' : '') + inst.configDir));
  line.append(cell('why', tr('access.instance_why')));
  return [line];
}

function personRow(p, roles) {
  const row = cell('acc person' + (p.me ? ' now' : ''));
  row.append(whoLine(p.who, p.me ? cell('you', tr('access.you')) : null),
             capsLine(p.can));

  const controls = cell('acccontrols');
  const pick = document.createElement('md-outlined-select');
  pick.setAttribute('label', tr('access.role'));
  for (const r of roles) {
    const o = document.createElement('md-select-option');
    o.value = r;
    if (r === p.role) o.selected = true;
    const t = document.createElement('div');
    t.slot = 'headline';
    t.textContent = r;
    o.append(t);
    pick.append(o);
  }
  // The role is the only thing this control changes now: which companions it applies to is its own
  // sub-section below, where each one can be taken away on its own.
  pick.addEventListener('change', () => setPerson(p.who, pick.value, (p.companions || []).join(',')));
  const drop = withMark(document.createElement('md-text-button'), '#i-sl-trash-can');
  label(drop, tr('action.remove'));
  drop.onclick = () => confirmThis({
    head: tr('access.remove_head', {who: p.who}),
    body: tr('access.remove_body'),
    keep: tr('action.cancel'), keepMark: '#i-sl-xmark',
    doIt: tr('action.remove'), doMark: '#i-sl-trash-can',
    go: () => post('/access', new URLSearchParams({who: p.who, remove: '1'}), '', '')
      .then(why => { if (!why) loadAccess(); }),
  });
  controls.append(pick, drop);
  row.append(controls, scopeSection(p));
  return row;
}

// Which companions a person's role applies to — a sub-section of theirs, not a field.
//
// # Why it is not a chip with an × on the capabilities
//
// It was asked for there, and it cannot go there: a capability is not granted to a PERSON. It comes
// from the role, and taking `configure` off one person while leaving them an operator is a sentence
// auth.toml has no way to write. Roles exist so that grants do not accumulate per person one
// exception at a time.
//
// What IS per person is this: the same role, narrowed to named companions. So the × belongs here,
// where pressing it removes something that exists — and a list of them is a section rather than a
// comma-separated text field, which is what it was: a box somebody had to retype in full to drop
// one name from three.
function scopeSection(p) {
  const box = cell('scopes');
  const on = p.companions || [];
  box.append(cell('scopek', tr(on.length ? 'access.only_on' : 'access.everywhere')));
  const chips = document.createElement('md-chip-set');
  for (const name of on) {
    // An INPUT chip, which is the variant for a piece of information a person put there and can
    // take away — and it is the one the guide gives a trailing remove action.
    const c = document.createElement('md-input-chip');
    c.setAttribute('label', name);
    c.className = 'scopechip';
    c.addEventListener('remove', () => {
      setPerson(p.who, p.role, on.filter(n => n !== name).join(','));
    });
    chips.append(c);
  }
  // And the way to add one. A name rather than a menu of what is running: a person can be scoped
  // to a companion that is not up at the moment, and a menu would refuse to say so.
  const add = withGlass(document.createElement('md-outlined-text-field'));
  add.setAttribute('label', tr('access.add_companion'));
  add.addEventListener('keydown', ev => {
    if (ev.key !== 'Enter') return;
    const name = String(add.value || '').trim();
    if (!name || on.includes(name)) return;
    setPerson(p.who, p.role, on.concat([name]).join(','));
  });
  box.append(chips, add);
  return box;
}

function addPersonButton(roles) {
  const b = label(withMark(document.createElement('md-text-button'), '#i-sl-plus'),
                  tr('access.add'));
  b.onclick = () => {
    const who = prompt(tr('access.add_who'));
    if (!who || !who.trim()) return;
    setPerson(who.trim(), roles.includes('viewer') ? 'viewer' : (roles[0] || ''), '');
  };
  return b;
}

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
// Which box the transcript scrolls IN.
//
// Beside a companion on a wide screen the page itself does not scroll: the three columns each
// scroll their own contents, so the cards above the conversation and the panes either side stay
// where they are while the transcript moves under them. Everywhere else — the fleet list, a phone,
// a deep screen — the window is the scroller, as it has always been.
//
// One accessor rather than a flag each site checks: everything below measures a scroll position,
// and two of them disagreeing is a transcript that jumps.
const logScrolls = () => wide.matches && !!sock();
const scroller = () => (logScrolls() ? log : (document.scrollingElement || document.documentElement));

// Follow the tail only while the reader is already at the bottom. Yanking the view down while
// somebody reads the middle of a long run is how a live page becomes unreadable.
const atBottom = () => {
  if (!following()) return false;
  const s = scroller();
  return s.scrollHeight - s.scrollTop - s.clientHeight <= 48;
};

// following reports whether there is a transcript on screen to follow.
//
// Both of these act on the WINDOW when the transcript does not scroll itself — which is right on a
// phone and wrong everywhere there is no transcript at all. On the permissions screen the dock
// measurement ran at load, found "the bottom" of a page that had not been drawn yet, and sent the
// window to it: the screen opened 1,126 pixels down, with its heading off the top. The fleet and
// the board are the same shape and were the same bug waiting.
const following = () => (log.clientHeight || 0) > 0;

// toBottom goes to the foot of the page, and then again once the browser has laid out what it just
// put there.
//
// A row is `content-visibility:auto` with an intrinsic size of 3.5rem, so a row that has only just
// been appended reports THAT height until the browser lays it out — which it does a frame later,
// because the row is at the bottom of the viewport. One scroll therefore aims at a document that
// is about to get taller, and the reader lands short of the end by the difference.
//
// The rows this bites are the tall ones, which is exactly the ones worth reading to the end of: a
// tool call carries its command, its output and a fold, and is many times 3.5rem. Measured on a
// live console — an answer with two tool calls left the page a few hundred pixels above the last
// line, every time.
//
// Two extra frames rather than a loop with a condition. The growth happens on the frame after the
// insert; a third pass is there for the row that grows again as its own content settles, and a
// watcher that kept going would be a scroll that fights a person trying to scroll away.
function toBottom(frames) {
  if (!following()) return;
  const s = scroller();
  s.scrollTop = s.scrollHeight;
  const left = frames === undefined ? 2 : frames;
  if (left > 0 && typeof requestAnimationFrame === 'function') {
    requestAnimationFrame(() => toBottom(left - 1));
  }
}

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

// jsonPairs reads a tool's arguments as what they are: a flat object with known keys.
//
// They were printed as the JSON they arrived in, which is the form they have because a wire needs
// one — a call to write a file is its path and its whole body escaped onto a single line, and the
// thing somebody opened the fold to read is in there behind \n and \". The shape is not a mystery;
// it is a small object whose keys are the tool's own parameter names.
//
// Only an object, and only when it parses. An array is a list of things with no names to put
// beside them, and anything that is not JSON at all — most tool OUTPUT — is prose or a diff and
// belongs in the block it already had.
function jsonPairs(text) {
  const t = String(text || '').trim();
  if (!t.startsWith('{')) return null;      // cheap reject before handing a transcript to the parser
  let v;
  try { v = JSON.parse(t); } catch { return null; }
  if (!v || typeof v !== 'object' || Array.isArray(v)) return null;
  const out = [];
  for (const k of Object.keys(v)) {
    const raw = v[k];
    // A string keeps its own newlines and quotes — that is the whole gain. Anything else is
    // re-encoded, because "[object Object]" is worse than the JSON it came from.
    out.push([k, typeof raw === 'string' ? raw : JSON.stringify(raw)]);
  }
  return out.length ? out : null;
}

// pairsInto draws those pairs: the name in a gutter, the value beside it, and a value that runs to
// several lines in a block of its own underneath.
function pairsInto(pairs) {
  const box = cell('args');
  for (const [k, v] of pairs) {
    box.append(cell('argk', k));
    // Preformatted either way: an argument is a path, a command or a body, and none of those want
    // their whitespace collapsed. The one-liners just do not need a block of their own.
    const val = el('pre', v);
    val.className = 'argv' + (v.includes('\n') ? ' tall' : '');
    box.append(val);
  }
  return box;
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
        const pairs = asDiff ? null : jsonPairs(text);
        if (asDiff) {
          const pre = el('pre');
          pre.className = 'diff';
          body.append(diffInto(pre, text));
        } else if (pairs) {
          body.append(pairsInto(pairs));
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

// ── how the fleet is laid out, and what is crossing between its parts ────────
//
// The table answers "what is each companion doing". This answers the other question a person has
// once there is more than one machine in it: WHERE is all this running, and what is actually
// travelling between the parts. Both are the same rows — this one groups them by the instance they
// belong to and draws the traffic that the table can only list one companion at a time.
//
// Two fetches and no new endpoint: /fleet is who exists and /handoffs is what was handed to whom.
// A third endpoint assembling a graph server-side would be a second place that decides what a
// companion IS, and the first one already answers this page four times a minute.
async function loadMap() {
  const [rows, hands] = await Promise.all([fetchList('/fleet'), fetchList('/handoffs')]);
  if (!rows) return;
  const head = sectionHead('nav.map', toTable());
  const boxes = cell('places');
  // Two boundaries, two boxes. The outer one is the MACHINE, which is what a network reaches and
  // what goes down; the inner one is the account on it, which is what owns a config directory, a
  // policy, a key and a session store. Nesting them says both without a word of explanation — and
  // it says the thing a flat list of "you@studio, sam@studio, you@buildbox" makes a reader
  // assemble for themselves: that two of those share a kernel and two share an owner.
  const rank = {own: 0, admitted: 1, unknown: 2};
  const machines = new Map();
  for (const a of rows) {
    // A peer is a console somebody put in front of another machine's fleet. What comes back is
    // already grouped by that console, and this one has no idea what host it runs on — so the
    // peer's name IS the machine here, which is also what every other screen calls it.
    const host = a.peer ? a.peer : (a.host || tr('map.here'));
    const who = accountOf(a) || tr('map.here');
    if (!machines.has(host)) machines.set(host, new Map());
    const inner = machines.get(host);
    if (!inner.has(who)) inner.set(who, []);
    inner.get(who).push(a);
  }
  const order = list => rank[trustOf(list)] ?? 3;
  const placed = [...machines.entries()].sort((x, y) => {
    const rx = Math.min(...[...x[1].values()].map(order));
    const ry = Math.min(...[...y[1].values()].map(order));
    return rx - ry || (x[0] < y[0] ? -1 : 1);
  });
  for (const [host, accounts] of placed) boxes.append(machineBox(host, accounts, order));
  // The wires are drawn over the boxes, so the element has to exist before anything is measured —
  // and it is measured only in a browser: there is no layout under the test harness, and a wire
  // between two boxes that are both at 0,0 is a line of noise pretending to be information.
  const wires = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  wires.setAttribute('class', 'wires');
  wires.setAttribute('aria-hidden', 'true');
  const legend = cell('maplegend');
  for (const [cls, key] of [['ok', 'map.edge_ok'], ['flight', 'map.edge_working'],
                            ['down', 'map.edge_down']]) {
    const item = cell('mapkey');
    item.append(cell('wirekey ' + cls), cell('', tr(key)));
    legend.append(item);
  }
  const canvas = cell('mapcanvas');
  canvas.append(wires, boxes);
  mapEl.replaceChildren(head, cell('accsay', tr('map.lead')), canvas, legend);
  drawWires(canvas, wires, rows, hands || []);
}

// accountOf is the half of an instance that is not the machine. The machine is already the box
// around it, and repeating "you@buildbox" inside a box labelled buildbox is the label saying the
// same thing twice.
function accountOf(a) {
  const inst = a.instance || '';
  const at = inst.indexOf('@');
  if (at > 0) return inst.slice(0, at);
  return inst;
}

// One machine, with the accounts running magi on it.
//
// Its own freshness rather than each account's: gossip crosses between machines, so "nothing heard
// for nine minutes" is a fact about the link to that box and not about one account inside it. The
// console's own machine says nothing — it is reading its own directory, and there is no link to
// be up or down.
function machineBox(host, accounts, order) {
  const box = cell('machine');
  const top = cell('machinetop');
  top.append(cell('machinename', host));
  const all = [].concat(...[...accounts.values()]);
  const addr = (all.find(a => a.addr) || {}).addr;
  if (addr) top.append(cell('machineaddr', addr));
  box.append(top);
  // Only the bad news lives up here. How fresh each companion's record is belongs on the companion
  // — a gossip round carries every member's own sighting, so "heard 30 seconds ago" is a fact
  // about that companion and not about the box around it. What IS about the box is silence: when
  // nothing inside it has been heard from at all, the link is what to look at, not five rows each
  // reporting the same minute.
  if (trustOf(all) && trustOf(all) !== 'own' && !all.some(a => a.live)) {
    const fresh = all.reduce((n, a) => Math.min(n, a.idle >= 0 ? a.idle : 1e9), 1e9);
    box.append(cell('placeseen down', tr('map.unseen', {ago: ago(fresh === 1e9 ? -1 : fresh)})));
  }
  const inner = cell('accounts');
  for (const [who, list] of [...accounts.entries()].sort((x, y) => order(x[1]) - order(y[1]))) {
    inner.append(placeBox(who, list));
  }
  box.append(inner);
  return box;
}

// trustOf is the relationship a whole instance has with this console: its companions all live
// under one key, so any row of the group answers for the group.
function trustOf(list) {
  for (const a of list) if (a.trust) return a.trust;
  return '';
}

// toTable is the way back, and the fleet's own head grows the way here. One control each, because
// these are two views of one destination rather than two destinations.
function toTable() {
  const b = label(withMark(document.createElement('md-text-button'), '#i-sl-layer-group'),
                  tr('map.as_table'));
  b.onclick = () => { history.pushState({}, '', at(HREF.fleet)); render(); };
  return b;
}

// ── meetings ─────────────────────────────────────────────────────────────────
//
// Companions talking to each other, watched from here.
//
// The console holds the floor and drives the turns (see meet.go); this screen is a window onto
// that, plus the two things a person does in a room — say something, and end it. It polls rather
// than streams: a turn is a minute of model time, so there is nothing an event stream would
// deliver sooner than the next tick.
//
// Nothing on this screen changes a workspace. The conclusions arrive as a list and stay there
// until somebody presses send on one, which is the seam the whole feature is built around.

// What is being filled in, held outside the render because the render happens on a timer: a topic
// half typed would be wiped by the poll that redraws the meetings under it.
let meetPick = new Set();
// The convene button, held rather than looked up: it is built here and never in the markup, so
// there is no id for getElementById to find — and an id that only exists at runtime is one the
// page's own "every element it reaches for exists" check cannot vouch for.
let meetGoBtn = null;
// What has already been sent, by meeting and by whom. Kept for the life of the page: the room is
// redrawn on a timer, and "you already sent this one" is not a fact the server carries — handing a
// task out is an ordinary prompt in that companion's session, indistinguishable afterwards from
// any other.
const meetHanded = new Set();
// The two fields somebody types into. Held for one question only — "is the caret in this?" — which
// is what decides whether the poll may rebuild the screen under them.
let meetTopicField = null;
let meetSayField = null;
// What is being typed into the room. Held out here for the same reason the topic is: the room is
// rebuilt every two seconds, and a sentence that lived only in the field was thrown away by the
// next tick unless the caret happened to be in it. Watched live: the floor was taken, the words
// were gone by the time Say it was pressed, and the press did nothing at all.
let meetSaying = '';

// typingIn reports whether the caret is in that field.
//
// The field, not the screen. Both draws used to skip when the focus was anywhere inside #meet, and
// a link is focusable: clicking a meeting in the list left the caret on the row you had just
// clicked, so the room refused to draw and the list stayed where it was. The address had already
// changed, so it looked like one particular meeting could not be opened at all.
//
// A text field puts its own focus in a shadow root, and document.activeElement reports the HOST —
// which is exactly the element held here.
function typingIn(field) { return !!field && document.activeElement === field; }
let meetTopic = '';
// The meeting the reader is in, and whether the last look found it. A meeting lives in the console
// that convened it, so one that has gone is a normal thing to arrive at from a bookmark.
const meetOf = () => new URLSearchParams(location.search).get('m') || '';
let meetGone = false;

// meetGet is a quiet read: no red dot, no status line.
//
// fetchList speaks up because the screens that use it are the console's own state — if the fleet
// cannot be read, something is wrong with the console. A meeting that ended is not that: it is an
// address that no longer names anything, and the screen says so in the place the meeting would
// have been.
async function meetGet(path) {
  try {
    const r = await fetch(path);
    if (!r.ok) return null;
    return await r.json();
  } catch { return null; }
}

async function loadMeet() {
  const id = meetOf();
  if (!id) {
    const [list, open] = await Promise.all([fetchList('/fleet'), meetGet('/meet')]);
    drawConvene(list || [], open || []);
    return;
  }
  const m = await meetGet('/meet?id=' + encodeURIComponent(id));
  if (!m) { meetGone = true; drawMeetGone(); return; }
  meetGone = false;
  drawRoom(m);
}

// The form: what to ask, and who to ask.
function drawConvene(list, open) {
  // Not while somebody is writing the question. The poll behind this form is here for the list of
  // open meetings at the bottom, and rebuilding the form to refresh that list would take the caret
  // out of a topic somebody is halfway through.
  if (typingIn(meetTopicField)) return;
  const box = cell('meetbox');
  box.append(sectionHead('meet.title', toFleet()));
  box.append(cell('meetwhy', tr('meet.why')));

  const topic = document.createElement('md-outlined-text-field');
  topic.className = 'meettopicfield';
  topic.setAttribute('label', tr('meet.topic'));
  topic.setAttribute('type', 'textarea');
  topic.setAttribute('rows', '2');
  topic.value = meetTopic;
  meetTopicField = topic;
  // Kept as it is typed, not read at the end: the poll under this form redraws it every two
  // seconds, and a field whose value lived only in the DOM would lose a sentence mid-word.
  topic.addEventListener('input', () => { meetTopic = topic.value; armConvene(); });

  // Only what this console can actually ask. A companion on another machine is a row this console
  // has never dialled — see Elsewhere — so putting it in the room would be offering a turn nobody
  // here can spend.
  const here = (list || []).filter(a => !a.elsewhere && !a.peer);
  const who = document.createElement('md-chip-set');
  who.className = 'meetwho';
  // A chip set of one is not a set — the guidance says so twice, once about chips never standing
  // alone and once about a filter that offers a single choice. With fewer than two companions
  // there is no room to fill, and the line below says that in words instead.
  for (const a of (here.length > 1 ? here : [])) {
    const c = document.createElement('md-filter-chip');
    // The name alone. A chip label is capped at twenty characters by the guidance and these were
    // running to sixty — "design — the design system: component specs and visual review" is a
    // sentence wearing a chip. What it is for belongs in the tooltip, where the rest of this page
    // puts the same fact.
    c.setAttribute('label', a.name);
    if (a.role) tip(c, a.role);
    c.selected = meetPick.has(a.socket);
    c.onclick = () => {
      // The chip owns its own selected state and flips it after the click; what this reads is what
      // it is ABOUT to become, so the set and the drawing cannot disagree. Same lesson the pane
      // handles learned the hard way.
      if (meetPick.has(a.socket)) meetPick.delete(a.socket); else meetPick.add(a.socket);
      armConvene();
    };
    who.append(c);
  }

  const go = label(withMark(document.createElement('md-filled-button'), '#i-sl-comments'),
                   tr('meet.start'));
  go.className = 'meetgo';
  meetGoBtn = go;
  go.onclick = async () => {
    const body = new URLSearchParams();
    body.set('topic', meetTopic);
    for (const s of meetPick) body.append('who', s);
    const r = await fetch('/meet', {method: 'POST', body});
    if (!r.ok) { says((await r.text()).trim().slice(0, 120)); return; }
    // The answer is the meeting, and a console that accepted the request without making one — the
    // demo, which answers every POST with "would have sent" — leaves the reader where they were
    // rather than at an address that names nothing.
    const m = await r.json().catch(() => null);
    if (!m || !m.id) { loadMeet(); return; }
    // The form is emptied, both halves of it. The topic was cleared and the picks were not, so
    // coming back to the list left every chip still selected and the button one press away from
    // convening the same room about nothing.
    meetTopic = '';
    meetPick = new Set();
    history.pushState({}, '', at(HREF.meet + '&m=' + encodeURIComponent(m.id)));
    render();
  };

  // What this is, how it ends, then the form, then the one control that starts it. The button was
  // in the middle — between the chips and two lines of explanation — which put the act before the
  // half of the explanation that says what pressing it commits to, and left the notes hanging off
  // nothing.
  //
  // How it ends is said once and near the top, because it is not a thing anybody sets: the
  // participants stop when they have nothing left to add, and the console's ceiling is there for
  // the room that will not. Asking a convener for a number was asking them to guess the length of
  // a discussion before it had happened.
  box.append(cell('meetends', tr('meet.ends')), topic, cell('meetlbl', tr('meet.who')), who);
  // The reason it cannot start yet, on the same line as the control it is about rather than as a
  // refusal from the server after the fact. Two is the floor: with one companion this is a
  // conversation, and its own page does that better.
  //
  // One line, note leading and action trailing. The button was flush against the left edge on a
  // line of its own, which is where a form's fields begin and not where its action belongs — it
  // read as another field that had lost its label, and the note under it hung off nothing.
  box.append(go, cell('meetnote', here.length < 2 ? tr('meet.need_two') : ''));

  // Two lists, because they are two different things to a reader: one is a discussion they can
  // walk into, the other is a decision waiting for somebody to act on it. Under one heading that
  // said "going on now", the finished ones read as a pile of meetings that would not end.
  const going = (open || []).filter(m => !m.closed);
  const done = (open || []).filter(m => m.closed && (m.tasks || []).length);
  for (const [key, rooms] of [['meet.open', going], ['meet.finished', done]]) {
    if (!rooms.length) continue;
    box.append(sectionHead(key));
    const l = cell('meetlist');
    for (const m of rooms) l.append(meetRow(m));
    box.append(l);
  }
  meetEl.replaceChildren(box);
  armConvene();
}

// armConvene keeps the button honest about whether there is a meeting to start.
//
// A control that can be pressed and then refuses is worse than one that says why it cannot: the
// press is the moment somebody learns, and by then they have written a topic.
function armConvene() {
  const go = meetGoBtn;
  if (!go) return;
  const ready = meetTopic.trim() !== '' && meetPick.size >= 2;
  go.toggleAttribute('disabled', !ready);
  const note = meetEl.querySelector('.meetnote');
  if (note && !note.dataset.fixed) {
    note.textContent = ready ? '' :
      meetPick.size < 2 ? tr('meet.need_two') : tr('meet.need_topic');
  }
}

function meetRow(m) {
  const a = document.createElement('a');
  a.className = 'meetrow state';
  a.href = at(HREF.meet + '&m=' + encodeURIComponent(m.id));
  a.onclick = e => {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button) return;
    e.preventDefault();
    history.pushState({}, '', a.getAttribute('href'));
    render();
  };
  a.append(cell('meettitle', m.topic));
  a.append(cell('meetmeta', meetWhere(m) + ' · ' +
    (m.speakers || []).filter(s => !s.person).map(s => s.name).join(', ')));
  return a;
}

// meetWhere is the one line that says what stage a meeting is at.
function meetWhere(m) {
  // The floor being held is the reason nothing is happening, so it is the thing to say. Without
  // it the room reads "Round 2 of 5" and looks stuck, which is exactly what it is not.
  if (!m.closed && m.held) return tr('meet.yours');
  if (m.collecting) return tr('meet.collecting');
  if (m.closed) {
    if (!(m.tasks || []).length) return tr('meet.closing');
    // Two ways to be finished, and they mean different things to somebody reading the
    // conclusions: a room that ran out of things to say has answered the question, and a room the
    // ceiling stopped may have been mid-argument.
    return m.spent ? tr('meet.done_spent') : tr('meet.done');
  }
  return tr('meet.round', {n: m.round, of: m.max});
}

function drawMeetGone() {
  const box = cell('meetbox');
  box.append(sectionHead('meet.title', toFleet()));
  box.append(emptyState('meet.gone', 'meet.gone_how'));
  meetEl.replaceChildren(box);
}

// upNextName is whoever the console is waiting on, for the one line that says so.
function upNextName(m) {
  const next = (m.speakers || []).find(sp => sp.next);
  return next ? next.name : '';
}

// The room.
function drawRoom(m) {
  // While somebody is typing into the room, the room holds still. Taking the floor is what makes
  // that safe: nothing else can be said, so a redraw would change nothing except where the caret
  // is. Asked of the say box rather than of the screen — every other thing in here is focusable
  // too, and a rule about "focus is somewhere on this screen" is a rule that fires on a link.
  if (typingIn(meetSayField)) return;
  const box = cell('meetbox');
  box.append(sectionHead('meet.title', toBack()));
  // The question is the headline of this screen, so it is a heading and not a styled line: with
  // only the section's own h2 above it, everything below — the roster, the transcript, the
  // conclusions — hung off "Meeting" and a reader moving by headings never met the topic.
  const topic = document.createElement('h3');
  topic.className = 'meettopic';
  topic.textContent = m.topic;
  box.append(topic);
  box.append(cell('meetmeta', meetWhere(m)));
  // Something is happening and it takes a minute. Without this the room is a still page between
  // turns — the same picture as a room that has stopped — and the guidance is explicit that a wait
  // whose length nobody can predict gets an indeterminate indicator rather than nothing.
  if (!m.closed || m.collecting) {
    const bar = document.createElement('md-linear-progress');
    bar.indeterminate = true;
    bar.className = 'meetbar-progress';
    // Named for what is being waited on, not "loading": whoever has the floor is the answer to
    // "why is nothing on the screen changing".
    bar.setAttribute('aria-label', m.collecting ? tr('meet.collecting')
      : tr('meet.waiting_on', {who: m.holder || upNextName(m) || ''}));
    box.append(bar);
  }
  // What went wrong, where it happened, rather than in a log nobody has open. A participant whose
  // daemon has gone is a fact about this meeting.
  if (m.trouble) box.append(cell('meettrouble', tr('meet.trouble', {why: m.trouble})));

  box.append(roster(m));
  box.append(transcript(m));
  meetSayField = null;
  if (!m.closed) box.append(sayBox(m));
  if (m.closed && (m.tasks || []).length) box.append(conclusions(m));
  meetEl.replaceChildren(box);
}

function toBack() {
  const b = label(withMark(document.createElement('md-text-button'), '#i-sl-chevron-left'),
                  tr('meet.back'));
  b.onclick = () => { history.pushState({}, '', at(HREF.meet)); render(); };
  return b;
}

// And out of the meetings screen entirely, to where the participants are.
//
// Not the map's control, which is what this used to borrow: that one is labelled "as a table"
// because there it switches between two views of one destination. Here it is a way out, and a way
// out labelled as a view switch tells somebody the wrong thing about where they are.
function toFleet() {
  const b = label(withMark(document.createElement('md-text-button'), '#i-sl-chevron-left'),
                  tr('nav.companions'));
  b.onclick = () => { history.pushState({}, '', at(HREF.fleet)); render(); };
  return b;
}

// Who is in the room, and what each of them is doing about it.
//
// The token is the whole mechanism, so the screen draws it: whoever holds the floor is marked, and
// so is whoever is next. Without that, "why is nothing happening" has no answer on the screen —
// the transcript only says what has already been said.
function roster(m) {
  // A row of chips, not a stack of rows. Four participants took four lines and most of the width
  // to say one word each about their state — and the state is the kind of thing a colour says.
  // Pressing one calls on that participant: it is the same act as writing "@ops" in a sentence,
  // and pressing your own is taking the floor. So the roster is also how the token is moved, which
  // is what the widest thing on the screen should be for.
  const box = document.createElement('md-chip-set');
  box.className = 'meetroster';
  for (const s of (m.speakers || [])) {
    const holding = m.holder === s.name;
    // Assist chips: each one does something. Not filter chips — nothing here is being filtered, and
    // a filter chip's tick would say "included" about a participant who is in the room either way.
    // A filter chip, because the bundle has those and assist chips are not in it — and the
    // semantic survives the substitution: the set has exactly one selected member, and the
    // selected one is whoever holds the floor. Pressing another is changing that selection.
    const c = document.createElement('md-filter-chip');
    // Speaking wins over next. While a companion is composing it is BOTH — it holds the floor and
    // it is still the one whose turn this round is — and a chip wearing two markers at once says
    // neither: the eye wants "this one now, that one after".
    c.className = 'meetsp' + (holding ? ' holding' : '') + (s.next && !holding ? ' next' : '') +
                  (s.person ? ' person' : '') + (s.passes >= 2 ? ' resting' : '');
    c.setAttribute('label', s.name);
    c.selected = holding;
    // The state in words as well as in colour, where a colour alone would be the only telling.
    const what = holding ? tr('meet.holding')
      : s.next ? tr('meet.next')
      // Two passes and the rules stop asking. Said here rather than left as silence: a reader
      // watching a companion be skipped needs to know it is a rule and not a fault — and that
      // naming it brings it back, which pressing this does.
      : s.passes >= 2 ? tr('meet.resting')
      : s.person ? tr('meet.you')
      : '';
    tip(c, what ? s.name + ' — ' + what : tr('meet.call', {who: s.name}));
    c.setAttribute('aria-label', what ? s.name + ' — ' + what : s.name);
    if (!m.closed) {
      c.onclick = async () => {
        await fetch('/meet-say', {method: 'POST',
          body: new URLSearchParams({id: m.id, call: s.name})});
        loadMeet();
      };
    } else {
      c.setAttribute('disabled', '');
    }
    box.append(c);
  }
  return box;
}

// Everything said, in order, attributed.
function transcript(m) {
  const box = cell('meetsaid');
  let round = 0;
  for (const u of (m.said || [])) {
    if (u.round !== round) {
      round = u.round;
      box.append(cell('meetlap', tr('meet.lap', {n: round})));
    }
    const line = cell('meetline' + (u.pass ? ' passed' : ''));
    line.append(cell('meetwho2', u.who));
    // A pass is a contribution: somebody read the room and had nothing to add, which is worth
    // seeing. Drawn quieter than a sentence, and never dropped.
    line.append(cell('meettext', u.pass ? (u.text ? tr('meet.passed_why', {why: u.text})
                                                  : tr('meet.passed')) : u.text));
    box.append(line);
  }
  if (!(m.said || []).length) box.append(cell('meetwait', tr('meet.waiting')));
  return box;
}

// The person's own box: typing takes the floor, sending gives it back.
function sayBox(m) {
  const box = cell('meetsay');
  const f = document.createElement('md-outlined-text-field');
  f.setAttribute('label', tr('meet.say'));
  f.setAttribute('type', 'textarea');
  f.setAttribute('rows', '2');
  f.id = 'meetSay';
  f.value = meetSaying;
  meetSayField = f;
  // Taken on the first keystroke and not on every one: the hush is a state, and re-posting it per
  // character would be a request per character. Given back by sending, or by leaving the box empty.
  let held = false;
  const hold = async on => {
    if (held === on) return;
    held = on;
    const body = new URLSearchParams({id: m.id});
    if (on) body.set('hold', '1');
    await fetch('/meet-say', {method: 'POST', body});
  };
  f.addEventListener('input', () => { meetSaying = f.value; hold(f.value.trim() !== ''); });
  f.addEventListener('blur', () => { if (f.value.trim() === '') hold(false); });
  const send = label(withMark(document.createElement('md-filled-button'), '#i-sl-paper-plane-top'),
                     tr('meet.send'));
  send.onclick = async () => {
    const text = (f.value || meetSaying).trim();
    if (!text) return;
    const body = new URLSearchParams({id: m.id, text});
    const r = await fetch('/meet-say', {method: 'POST', body});
    if (!r.ok) { says((await r.text()).trim().slice(0, 120)); return; }
    f.value = '';
    meetSaying = '';
    held = false;
    loadMeet();
  };
  const stop = label(withMark(document.createElement('md-text-button'), '#i-sl-flag-checkered'),
                     tr('meet.wrap'));
  // Ending it is the convener's call and not a rule: the rounds are a ceiling, not a plan, and a
  // meeting that has answered the question in one lap should not spend two more.
  stop.onclick = async () => {
    await fetch('/meet-close', {method: 'POST', body: new URLSearchParams({id: m.id})});
    loadMeet();
  };
  // One row: the box, the thing that sends it, and the thing that ends the meeting. They were a
  // full-width field with its buttons stranded underneath — three parts of one control, drawn as
  // two unrelated things. The field takes the room and the buttons sit at the end of it, which is
  // the shape the page's own composer has had all along.
  box.append(f, send, stop);
  return box;
}

// What each participant leaves with, and the one control that makes any of it happen.
function conclusions(m) {
  const box = cell('meettasks');
  box.append(sectionHead('meet.tasks'));
  for (const t of (m.tasks || [])) {
    const row = cell('meettask' + (t.what ? '' : ' nothing'));
    row.append(cell('meettaskwho', t.who));
    // Nothing to do is an outcome and is drawn as one. A participant missing from this list would
    // read as one nobody asked.
    row.append(cell('meettaskwhat', t.what || tr('meet.task_none')));
    if (t.what && meetHanded.has(m.id + '|' + t.who)) {
      row.append(cell('meetsent', tr('meet.handed')));
    } else if (t.what) {
      const go = label(withMark(document.createElement('md-text-button'), '#i-sl-paper-plane-top'),
                       tr('meet.hand'));
      go.onclick = async () => {
        const r = await fetch('/meet-hand',
          {method: 'POST', body: new URLSearchParams({id: m.id, who: t.who})});
        if (!r.ok) { says((await r.text()).trim().slice(0, 120)); return; }
        // Remembered, not just drawn. The room is rebuilt by the poll every two seconds, so a
        // receipt written into the row it was pressed on vanished on the next tick — and a reader
        // who cannot see what they have already sent sends it twice.
        meetHanded.add(m.id + '|' + t.who);
        go.replaceWith(cell('meetsent', tr('meet.handed')));
      };
      row.append(go);
    }
    box.append(row);
  }
  return box;
}

// One account on that machine: a magi, with its companions.
//
// The inner box is the boundary everything else here is scoped to — one config directory, one
// policy, one key, one session store. Two accounts side by side inside one machine box is the
// picture that says sharing a kernel shares nothing: neither can read the other's sessions, and
// work between them crosses the door exactly as it would between two cities.
function placeBox(who, list) {
  const box = cell('place ' + (trustOf(list) || 'unsaid'));
  const top = cell('placetop');
  top.append(cell('placename', who));
  const t = trustOf(list);
  if (t) top.append(cell('placetrust ' + t, tr(TRUST_SAY[t] || 'map.trust_unsaid')));
  box.append(top);
  // Grouped by team inside the box, because a team is the third boundary on this screen and the
  // only one that CUTS ACROSS the other two: frontend can be three companions in one account,
  // backend can be one here, one on another account and one on another machine. Inside a box it is
  // a heading; across boxes it is the dotted line from whoever answers for the team.
  const teams = new Map();
  for (const a of list) {
    const t = a.team || '';
    if (!teams.has(t)) teams.set(t, []);
    teams.get(t).push(a);
  }
  // Named teams first, in name order; the companions in no team last, under no heading — a
  // heading reading "no team" is a label for the absence of one.
  const named = [...teams.keys()].filter(Boolean).sort();
  for (const t of named) {
    box.append(cell('teamlabel', t));
    for (const a of teams.get(t)) box.append(mapNode(a));
  }
  for (const a of (teams.get('') || [])) box.append(mapNode(a));
  return box;
}

const TRUST_SAY = {own: 'map.trust_own', admitted: 'map.trust_admitted', unknown: 'map.trust_unknown'};

// A companion, as a node: what it is called and what it is doing. The same state vocabulary the
// table uses — a second set of words for the same five states would be two things to learn.
function mapNode(a) {
  // A companion on another machine is drawn and does NOT link, for the reason the table gives: its
  // socket is a path on ITS filesystem, and this console would resolve it against its own — which
  // on two machines set up by one person is frequently a real companion, the wrong one.
  const remote = !!a.elsewhere;
  const n = document.createElement(remote ? 'div' : 'a');
  n.className = 'node state ' + (a.state || '') + (remote ? ' faroff' : '');
  n.setAttribute('data-sock', a.socket || '');
  if (!remote) {
    n.href = href(a);
    n.onclick = e => { e.preventDefault(); go(a.socket, a.peer); };
  }
  const mark = iconOr(STATE_MARK[GROUP[a.state] || ''] || '', '•', 'nodemark');
  if (mark) n.append(mark);
  n.append(cell('nodename', a.name || ''));
  if (a.hub) n.append(cell('nodehub', tr('team.speaks')));
  // How old this companion's record is — but only for the ones that arrived as records. On a local
  // row the same number means "last activity", which is a different fact with the same shape, and
  // the state word beside it already says what that row is doing.
  if (remote) n.append(cell('nodeage' + (a.live ? '' : ' down'), ago(a.idle)));
  n.append(cell('nodestate', stateWord(a.state)));
  return n;
}

// drawWires puts the traffic on top of the layout.
//
// Measured rather than laid out: the boxes are ordinary flow content, so where a node ended up is
// something only the browser knows. Under the test harness there is no layout at all and every box
// measures 0×0 — so nothing is drawn, and the model underneath is what the tests check. A wire
// drawn between two zeroes would be a line saying something nobody established.
//
// # Why the lines go round rather than across
//
// A straight line between two nodes in different machines crosses everything between them, and a
// diagram whose edges pass through its own boxes is one where you cannot tell which pair a line
// joins. So a crossing edge leaves its box at the side, drops into a lane under all of them,
// travels there, and comes up into the other one — orthogonal, one lane per edge so two of them
// never sit on top of each other. Inside a box the two nodes are stacked a few pixels apart and a
// short curve to the side is unambiguous, so that stays a curve.
function drawWires(canvas, svg, rows, hands) {
  if (typeof canvas.getBoundingClientRect !== 'function') return 0;
  const frame = canvas.getBoundingClientRect();
  if (!frame.width || !frame.height) return 0;
  svg.setAttribute('viewBox', '0 0 ' + frame.width + ' ' + frame.height);
  const box = el => {
    if (!el || typeof el.getBoundingClientRect !== 'function') return null;
    const r = el.getBoundingClientRect();
    return {l: r.left - frame.left, r: r.right - frame.left,
            t: r.top - frame.top, b: r.bottom - frame.top,
            y: r.top - frame.top + r.height / 2};
  };
  const nodeOf = sock => {
    const el = canvas.querySelector('[data-sock="' + String(sock).replace(/"/g, '') + '"]');
    if (!el) return null;
    const at = box(el);
    if (!at) return null;
    // The machine box it sits in, which is what an edge has to go around rather than through.
    let outer = el.parentNode;
    while (outer && !String(outer.className || '').split(' ').includes('machine')) outer = outer.parentNode;
    at.machine = box(outer) || at;
    at.el = el;
    return at;
  };
  const bySock = new Map(rows.map(a => [a.socket, a]));
  const byName = new Map(rows.map(a => [String(a.name || '').toLowerCase(), a]));
  // The lane under everything. The canvas reserves the room for it; without that the lowest wire
  // would be drawn over the legend.
  let lane = 0;
  const laneY = () => frame.height - 8 - (lane++ % 6) * 7;
  let drawn = 0;
  const pairs = [];
  for (const h of hands) {
    const from = byName.get(String(h.from || '').toLowerCase());
    const to = bySock.get(h.socket) || byName.get(String(h.to || '').toLowerCase());
    if (!from || !to || from === to) continue;
    // Three states, and they are the three a person acts on differently: in flight, answered, and
    // one that cannot be reached at all — which is what a handover to a companion nobody has seen
    // for five minutes is, however healthy the row looked when it was sent.
    pairs.push([from, to, !to.live ? 'down' : (h.state === 'working' ? 'flight' : 'ok')]);
  }
  // Teams are NOT drawn as lines. A hub is an addressing convention — name the team and the one
  // that answers for it gets it — and nothing routes through it, so a line from the hub to each
  // member is a picture of traffic that does not exist. Drawn once and taken out again: on a fleet
  // where a team spans three boxes it read as the busiest thing on the diagram while being the one
  // thing on it that never carries a byte. The team is a heading inside each box instead, which
  // says the same membership without claiming a route.
  for (const [from, to, cls] of pairs) {
    const a = nodeOf(from.socket), b = nodeOf(to.socket);
    if (!a || !b) continue;
    const together = a.machine.l === b.machine.l && a.machine.t === b.machine.t;
    if (together) curve(svg, a, b, cls);
    else around(svg, a, b, laneY(), cls);
    drawn++;
  }
  return drawn;
}

// Two nodes in one box: a short curve out of the trailing edge and back in.
function curve(svg, a, b, cls) {
  const x1 = a.r, y1 = a.y, x2 = b.r, y2 = b.y;
  const out = Math.max(16, Math.abs(y2 - y1) / 2);
  path(svg, 'M' + x1 + ' ' + y1 + ' C' + (x1 + out) + ' ' + y1 + ' ' +
             (x2 + out) + ' ' + y2 + ' ' + x2 + ' ' + y2, cls);
}

// Two nodes in different boxes: out of the side, down into the lane, along it, and up.
function around(svg, a, b, y, cls) {
  const leave = a.machine.r + 10, enter = b.machine.l - 10;
  const p = ['M' + a.r + ' ' + a.y, 'H' + leave, 'V' + y, 'H' + enter, 'V' + b.y, 'H' + b.l];
  path(svg, p.join(' '), cls);
}

function path(svg, d, cls) {
  const p = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  p.setAttribute('d', d);
  p.setAttribute('class', 'wire ' + cls);
  svg.append(p);
  return p;
}

// ── the workspace, on the page ───────────────────────────────────────────────
//
// A companion is bound to a directory, and until now the console could say the path and nothing
// about what was in it. Reading the files beside the conversation is what the terminal has always
// had and what every agent IDE is arranged around — the difference being that this console does
// not open them: it asks the companion, whose own read-only tools already confine every path to
// the workspace. See files.go.
const filesEl = document.getElementById('files');
const fileViewEl = document.getElementById('fileview');
const cardTabs = document.getElementById('cardtabs');
const filesToggle = document.getElementById('filesToggle');

// What is open, and which of them is showing. Paths, not contents: the file is fetched when its
// tab is chosen, so a tab left open for an hour shows what the file is now rather than what it was
// when somebody clicked it.
let openFiles = [];
// The companion the panes are drawn for, so opening one later can fill it without waiting for the
// next poll. Set where the page draws a companion, cleared when it leaves.
let lastDrawnFor = null;
let cardShows = 'facts';
// Which directories the reader has opened, so a redraw does not close the tree under them.
const openDirs = new Set();

// The attribute is written both ways by paneHandle, so this is the whole of the question.
const filesOpen = () => document.body.getAttribute('files') !== 'shut';

// What is being searched for in the workspace, and which of the two searches it is.
//
// Two, because they are two questions with two costs: a name search walks directory entries and a
// content search reads every file in the tree. Which one is a control the reader sets, not a guess
// this page makes from the shape of what they typed — a guess would make the expensive one happen
// by accident.
let findQ = '';
let findIn = 'names';
let findAt = 0;             // the query this page is waiting on, so a slow answer cannot overwrite a fast one

// findRow builds the box and the two chips over the tree.
function findRow(a) {
  const box = cell('filefind');
  const f = withGlass(document.createElement('md-outlined-text-field'));
  f.setAttribute('label', tr('files.find'));
  f.value = findQ;
  // Typed rather than submitted, and debounced: a name search is cheap and reading the tree
  // narrow as you type is the whole point. A content search waits for the pause too — it is the
  // expensive one and the pause is what keeps it from running on every keystroke.
  f.addEventListener('input', () => {
    findQ = f.value;
    const mine = ++findAt;
    setTimeout(() => { if (mine === findAt) runFind(a); }, 250);
  });
  box.append(f);
  const chips = document.createElement('md-chip-set');
  for (const [kind, key] of [['names', 'files.by_name'], ['text', 'files.by_text']]) {
    const c = document.createElement('md-filter-chip');
    c.setAttribute('label', tr(key));
    c.selected = findIn === kind;
    c.onclick = () => { findIn = kind; runFind(a); };
    chips.append(c);
  }
  box.append(chips);
  return box;
}

// runFind replaces the tree with what was found, or puts the tree back when the box is empty.
async function runFind(a) {
  if (!a) return;
  if (!findQ.trim()) { loadTree(a); return; }
  const mine = ++findAt;
  const got = await fetchOne('/find' + qFor(a) + '&in=' + findIn + '&q=' + encodeURIComponent(findQ));
  if (mine !== findAt) return;               // a later query is already on its way
  const kids = [findRow(a)];
  const hits = (got && got.hits) || [];
  if (!got) kids.push(cell('filesnote', tr('files.unreadable')));
  else if (!hits.length) kids.push(cell('filesnote', tr('files.no_match')));
  for (const hit of hits) kids.push(hitRow(a, hit));
  if (got && got.more) kids.push(cell('filesnote', tr('files.more', {n: got.more})));
  filesEl.replaceChildren(paneCard('files', tr('nav.files'), kids));
}

// One result. A name search answers with paths; a content search answers "path:line:text", which
// is the shape the agent's own grep produces — so the line and the text are shown as they came
// rather than being reassembled into a sentence.
function hitRow(a, hit) {
  const row = document.createElement('button');
  row.type = 'button';
  row.className = 'treerow hit state';
  let path = hit, line = '', text = '';
  if (findIn === 'text') {
    const first = hit.indexOf(':');
    const second = hit.indexOf(':', first + 1);
    if (first > 0 && second > first) {
      path = hit.slice(0, first);
      line = hit.slice(first + 1, second);
      text = hit.slice(second + 1);
    }
  }
  const mark = iconOr('#i-sl-file-lines', '·', 'treemark');
  if (mark) row.append(mark);
  const what = cell('hitwhat');
  what.append(cell('treename', line ? path + ':' + line : path));
  if (text) what.append(cell('hitline', text.trim()));
  row.append(what);
  row.onclick = () => openFile(a, path);
  return row;
}

// loadTree draws the workspace root, and nothing when the pane is shut: a fetch whose answer
// nobody can see is a request somebody's daemon served for nothing, four times a minute.
async function loadTree(a) {
  if (!a || !filesOpen()) return;
  // A companion known only by gossip has no socket this console can open — the path in its row is a
  // path on ITS filesystem, and the fleet door carries work rather than file contents. Say so, and
  // say the way round it: a magi-web running there is a peer, and a peer's companions come through
  // its own console with their files intact. A row with a peer on it is NOT this case.
  if (a.elsewhere) {
    filesEl.replaceChildren(paneCard('files', tr('nav.files'), [cell('filesnote', tr('files.elsewhere'))]));
    return;
  }
  treeAt.seen = [];
  const rows = await treeAt(a, '.');
  if (rows === null) {
    filesEl.replaceChildren(paneCard('files', tr('nav.files'), [cell('filesnote', tr('files.unreadable'))]));
    return;
  }
  // Two cards, like the pane on the other side — and each one scrolls itself.
  //
  // One scroller for both was wrong in a way you feel rather than see: a repository with three
  // hundred files pushed git's section off the bottom, and scrolling down to reach the branch
  // scrolled the tree away from wherever the reader had got to. They are two different things
  // being looked at for two different reasons, so they get two boxes with two scrollbars, and
  // neither moves when the other does.
  const tree = paneCard('files', shortPath(a.workdir || ''),
                        [findRow(a), ...(await branches(a, '.', rows, 0))]);
  const git = await gitSection(a);
  // Only when something is different. This runs on the three-second poll, so every message
  // arriving in the conversation rebuilt the whole pane — the tree, the git card, the branch
  // select, the per-file menus — for a workspace where nothing had changed. Visible as a flicker,
  // and worse than a flicker: a rebuilt select is a select whose open menu shuts, and a row being
  // pointed at moves out from under the pointer.
  //
  // Compared on the INPUTS, not on the markup: everything drawn here comes from the listings, the
  // git state, which directories are open and which file is showing, and a string of those is
  // cheap next to serialising a tree of several hundred rows.
  const now = JSON.stringify([a.workdir, treeAt.seen, gitSection.raw, [...openDirs].sort(), cardShows, findQ]);
  if (now === loadTree.drawn && filesEl.children.length) return;
  loadTree.drawn = now;
  filesEl.replaceChildren(tree, ...git);
}

// One section of the pane: a heading you can press, and what is under it.
//
// Collapsible rather than an accordion — an accordion closes one thing to open another, and here
// the branch and the tree are wanted at the same time about as often as not. What a person needs
// is to put away whichever half they are not using, which is a disclosure per section: both open
// by default, each remembered, neither closing the other.
//
// The heading is a button and says its state, so a keyboard reaches it and a screen reader is told
// what pressing it does — which is the part that gets left out when this is built from a div.
function paneCard(key, title, kids) {
  const shut = localStorage.getItem('pane.' + key) === 'shut';
  const card = cell('filescard pane-' + key + (shut ? ' shut' : ''));
  const head = document.createElement('button');
  head.type = 'button';
  head.className = 'panehead state';
  head.setAttribute('aria-expanded', String(!shut));
  const caret = iconOr('#i-sl-chevron-down', '▾', 'panecaret');
  if (caret) head.append(caret);
  head.append(cell('panetitle', title));
  head.onclick = () => {
    const now = !card.classList.contains('shut');
    card.classList.toggle('shut', now);
    head.setAttribute('aria-expanded', String(!now));
    localStorage.setItem('pane.' + key, now ? 'shut' : 'open');
  };
  const body = cell('panebody');
  body.append(...kids);
  card.append(head, body);
  return card;
}

// What git makes of this workspace: the branch, how far it is from its upstream, and what has not
// been committed.
//
// Above the tree, because it is the state the tree is IN: a file list with no branch on it is the
// same list on main and on a branch three commits deep, and which of those you are looking at
// changes what every row means. Nothing at all when the workspace is not a checkout — a companion
// working in a directory nobody put under version control has no branch to be on, and a section
// saying so would be a heading over an emptiness.
async function gitSection(a) {
  const g = await fetchOne('/git' + qFor(a));
  gitSection.raw = JSON.stringify(g || null);
  if (!g || !g.repo) return [];
  const box = cell('gitinner');
  const top = cell('gittop');
  const mark = iconOr('#i-sl-layer-group', '⎇', 'gitmark');
  if (mark) top.append(mark);
  // The branch is a MENU where there is more than one, which is what every editor's git panel puts
  // in this corner: the thing you look at to see where you are is the thing you press to go
  // somewhere else. A detached head is not a branch — git says "(detached)" where the name goes,
  // and printing a short sha under the word "branch" teaches the wrong thing in the state where it
  // costs work — so that case is a label and not a menu.
  const here = g.branch || (g.head ? '@' + g.head : tr('git.detached'));
  const branches = g.branches || [];
  if (may('shell') && g.branch && branches.length > 1) {
    const pick = document.createElement('md-outlined-select');
    pick.className = 'gitpick';
    pick.setAttribute('label', tr('git.branch'));
    for (const name of branches) {
      const o = document.createElement('md-select-option');
      o.value = name;
      if (name === g.branch) o.selected = true;
      const t = document.createElement('div');
      t.slot = 'headline';
      t.textContent = name;
      o.append(t);
      pick.append(o);
    }
    // Switching moves every file under the reader, so it is confirmed rather than done on a
    // change event somebody triggered with an arrow key while the menu was open.
    pick.addEventListener('change', () => {
      const to = String(pick.value || '');
      if (!to || to === g.branch) return;
      confirmThis({
        head: tr('git.switch_head', {branch: to}),
        body: tr('git.switch_body'),
        keep: tr('action.cancel'), keepMark: '#i-sl-xmark',
        doIt: tr('git.switch'), doMark: '#i-sl-arrows-rotate',
        go: () => gitRun(a, 'switch', {message: to}),
        // Put back if they say no: the menu has already moved to the branch they did not choose.
        onKeep: () => { pick.value = g.branch; },
      });
    });
    top.append(pick);
  } else {
    top.append(cell('gitbranch', here));
  }
  if (g.ahead) top.append(cell('gitab ahead', '↑' + g.ahead));
  if (g.behind) top.append(cell('gitab behind', '↓' + g.behind));
  box.append(top);
  if (may('shell')) box.append(gitBranchActs(a, g));
  const changes = g.changes || [];
  if (!changes.length) {
    box.append(cell('gitclean', tr('git.clean')));
    return [paneCard('git', tr('git.section'), [box])];
  }
  // Grouped the way every editor's git panel groups them: what a commit would take, and what it
  // would leave. A flat list makes somebody read the word on each row to work out which half of
  // the commit they are looking at.
  const staged = changes.filter(c => c.kind === 'staged' || c.kind === 'both');
  const rest = changes.filter(c => !(c.kind === 'staged' || c.kind === 'both'));
  if (staged.length) {
    box.append(cell('gitgroup', tr('git.group_staged')));
    for (const c of staged) box.append(gitLine(a, c));
  }
  if (rest.length) {
    box.append(cell('gitgroup', tr('git.group_changed')));
    for (const c of rest) box.append(gitLine(a, c));
  }
  if (may('shell') && staged.length) box.append(commitRow(a));
  return [paneCard('git', tr('git.section'), [box])];
}

// gitLine is one changed file: the row that opens it, and what can be done to it.
function gitLine(a, c) {
  const line = cell('gitline');
  const row = document.createElement('button');
  row.type = 'button';
  row.className = 'treerow gitrow state ' + (c.kind || '');
  row.append(cell('gitkind', tr(GIT_KIND[c.kind] || 'git.changed')));
  const name = cell('treename', c.path);
  // Clipped in an 18rem column, so the whole path has to be somewhere: the page's own tooltip,
  // which appears on focus as well as on hover — a native title does neither for a keyboard.
  tip(name, c.path);
  row.append(name);
  row.onclick = () => openFile(a, c.path);
  line.append(row, gitActs(a, c));
  return line;
}

// What the branch as a whole can be told to do. The four an editor's panel puts over the file
// list, and no more: everything here either moves the branch or moves the tree, and the ones that
// move the tree say so before they do it.
function gitBranchActs(a, g) {
  const box = cell('gitbranchacts');
  // Icon buttons with the word in a tooltip, not five labelled buttons: this column is 18rem and
  // "Restore stash" alone is most of it, so labels wrapped the row into three. A tooltip rather
  // than a menu — the actions are five, they are always the same five, and a menu would put two
  // presses between somebody and pulling. The page's own tooltip appears on focus as well as on
  // hover, which a native title does not.
  const act = (key, mark, run, on) => {
    if (on === false) return;
    const b = document.createElement('md-icon-button');
    const m = icon(mark);
    if (m) b.append(m);
    b.setAttribute('aria-label', tr(key));
    tip(b, tr(key));
    b.onclick = run;
    box.append(b);
  };
  act('git.pull', '#i-sl-reply', () => gitRun(a, 'pull'), !!g.upstream);
  act('git.push', '#i-sl-share-from-square', () => gitRun(a, 'push'), !!g.upstream || !!g.ahead);
  // Stash or put it back — never both, because which one is meaningful is a fact about the tree.
  if ((g.changes || []).length) {
    act('git.stash', '#i-sl-floppy-disk', () => confirmThis({
      head: tr('git.stash_head'), body: tr('git.stash_body'),
      keep: tr('action.cancel'), keepMark: '#i-sl-xmark',
      doIt: tr('git.stash'), doMark: '#i-sl-floppy-disk',
      go: () => gitRun(a, 'stash'),
    }));
  } else {
    act('git.unstash', '#i-sl-arrows-rotate', () => gitRun(a, 'unstash'));
  }
  act('git.new_branch', '#i-sl-plus', () => {
    const name = prompt(tr('git.new_branch_who'));
    if (name && name.trim()) gitRun(a, 'new-branch', {message: name.trim()});
  });
  return box;
}

// gitRun sends one of them and redraws from what git says afterwards rather than from what the
// button meant to do.
function gitRun(a, what, extra) {
  return post('/git-do',
    new URLSearchParams(Object.assign({do: what}, extra || {})),
    a.socket || '', a.peer || '').then(why => { if (!why) loadTree(a); });
}

// What somebody does to a changed file, behind one control.
//
// Three or four icon buttons per row is most of an 18rem column spent on actions that are wanted
// once each — and the path, which is the thing being read, was what gave up the room. One button
// that opens the list is the same trade every editor's git panel makes at this width.
//
// The menu is the same component the tree's rows use, and the first item is the one that answers
// the question the others act on: what changed.
function gitActs(a, c) {
  const box = cell('gitacts');
  if (!may('shell')) return box;
  const open = document.createElement('md-icon-button');
  const dots = icon('#i-sl-sliders');
  if (dots) open.append(dots);
  open.setAttribute('aria-label', tr('files.more'));
  tip(open, tr('files.more'));
  const menu = document.createElement('md-menu');
  const id = 'ga' + (gitActs.n = (gitActs.n || 0) + 1);
  open.id = id;
  menu.setAttribute('anchor', id);
  const send = (what, extra) => gitRun(a, what, Object.assign({path: c.path}, extra || {}));
  const item = (key, mark, run) => {
    const it = document.createElement('md-menu-item');
    const head = document.createElement('div');
    head.slot = 'headline';
    head.textContent = tr(key);
    it.append(head);
    const g = icon(mark);
    if (g) { g.setAttribute('slot', 'start'); it.append(g); }
    it.addEventListener('click', run);
    menu.append(it);
  };
  item('diff.show', '#i-sl-file-lines',
       () => openDiff(a, c.path, c.kind === 'untracked' ? 'untracked' : (c.kind === 'staged' ? 'staged' : '')));
  if (c.kind !== 'staged') item('git.stage', '#i-sl-plus', () => send('stage'));
  if (c.kind === 'staged' || c.kind === 'both') item('git.unstage', '#i-sl-reply', () => send('unstage'));
  // Throwing away what is in a file is the one thing here that cannot be undone by pressing the
  // other button, so it asks first — and it is not offered for a file git does not track yet,
  // where `git restore` has nothing to restore FROM.
  if (c.kind !== 'untracked') {
    item('git.discard', '#i-sl-eraser', () => confirmThis({
      head: tr('git.discard_head', {path: c.path}),
      body: tr('git.discard_body'),
      keep: tr('action.cancel'), keepMark: '#i-sl-xmark',
      doIt: tr('git.discard'), doMark: '#i-sl-eraser',
      go: () => send('discard'),
    }));
  }
  open.onclick = ev => { ev.stopPropagation(); menu.open = !menu.open; };
  box.append(open, menu);
  return box;
}

// A message and a button. Only when something is staged: a commit with nothing in it is a refusal
// git would have to explain, and the screen already knows.
function commitRow(a) {
  const box = cell('gitcommit');
  const msg = withGlass(document.createElement('md-outlined-text-field'));
  msg.setAttribute('label', tr('git.message'));
  const go = label(withMark(document.createElement('md-filled-button'), '#i-sl-check'),
                   tr('git.commit'));
  const send = () => {
    const text = String(msg.value || '').trim();
    if (!text) return;
    post('/git-do', new URLSearchParams({do: 'commit', message: text}),
         a.socket || '', a.peer || '').then(why => { if (!why) { msg.value = ''; loadTree(a); } });
  };
  msg.addEventListener('keydown', ev => { if (ev.key === 'Enter') send(); });
  go.onclick = send;
  box.append(msg, go);
  return box;
}

// Literal keys in a lookup, for the reason every other one on this page is: a key built by
// concatenation is invisible to the check that finds phrases nobody asks for.
const GIT_KIND = {staged: 'git.staged', unstaged: 'git.unstaged', both: 'git.both',
                  untracked: 'git.untracked', conflict: 'git.conflict'};

async function treeAt(a, path) {
  const got = await fetchList('/files' + qFor(a) + '&path=' + encodeURIComponent(path));
  // Written down as it arrives, for the comparison in loadTree. The pane is a function of several
  // requests — the root, every directory the reader has opened, the git state — and this is the
  // only place all of them pass through.
  if (Array.isArray(got)) treeAt.seen.push(path + ':' + got.map(e => e.name + (e.isDir ? '/' : '')).join(','));
  return Array.isArray(got) ? got : null;
}
treeAt.seen = [];

// branches renders one directory, and the ones the reader has opened under it.
//
// Depth is drawn as an indent rather than as a nested box: a tree of boxes at 18rem runs out of
// width four levels down, and the indent is what every file tree has used since they existed.
async function branches(a, dir, rows, depth) {
  const out = [];
  for (const e of rows) {
    const path = dir === '.' ? e.name : dir + '/' + e.name;
    out.push(treeRow(a, e, path, depth));
    if (e.isDir && openDirs.has(path)) {
      const kids = await treeAt(a, path);
      if (kids) out.push(...(await branches(a, path, kids, depth + 1)));
    }
  }
  return out;
}

// The menu a tree row opens: the things an editor's project view does to a file.
//
// A real menu and not a row of icons, because there are six of them and five are rare — an icon
// per action would put "delete" permanently beside every filename in an 18rem column. md-menu,
// which the bundle now carries: it positions itself, closes on the next click and on Escape,
// and moves focus through its items with the arrow keys, none of which is worth rebuilding here.
//
// Opened by the right button, and by a control on the row, because a context menu that can only be
// reached by right-clicking is one nobody finds on a trackpad or a touch screen.
function rowMenu(a, e, path) {
  const box = cell('rowmenu');
  const open = document.createElement('md-icon-button');
  const m = icon('#i-sl-sliders');
  if (m) open.append(m);
  open.setAttribute('aria-label', tr('files.more'));
  tip(open, tr('files.more'));
  const menu = document.createElement('md-menu');
  const id = 'rm' + (rowMenu.n = (rowMenu.n || 0) + 1);
  open.id = id;
  menu.setAttribute('anchor', id);
  const item = (key, run, mark) => {
    const it = document.createElement('md-menu-item');
    const head = document.createElement('div');
    head.slot = 'headline';
    head.textContent = tr(key);
    it.append(head);
    const g = icon(mark);
    if (g) { g.setAttribute('slot', 'start'); it.append(g); }
    it.addEventListener('click', run);
    menu.append(it);
    return it;
  };
  const send = (what, extra) => post('/file-do',
    new URLSearchParams(Object.assign({do: what, path: path}, extra || {})),
    a.socket || '', a.peer || '').then(why => { if (!why) loadTree(a); });
  const under = e.isDir ? path + '/' : (path.includes('/') ? path.slice(0, path.lastIndexOf('/') + 1) : '');
  item('files.new_file', () => {
    const name = prompt(tr('files.new_file_who'), under);
    if (name && name.trim()) send('new-file', {path: name.trim()}).then(() => openFile(a, name.trim()));
  }, '#i-sl-plus');
  item('files.new_dir', () => {
    const name = prompt(tr('files.new_dir_who'), under);
    if (name && name.trim()) send('new-dir', {path: name.trim()});
  }, '#i-sl-plus');
  item('files.rename', () => {
    const to = prompt(tr('files.rename_who'), path);
    if (to && to.trim() && to.trim() !== path) send('rename', {to: to.trim()});
  }, '#i-sl-pen-to-square');
  item('files.copy_path', () => {
    if (navigator.clipboard) navigator.clipboard.writeText(path).then(() => says(tr('files.copied')));
  }, '#i-sl-copy');
  // Both of the ones that lose work ask first, and they are different questions: a delete takes
  // the file, a rollback takes what was typed into it since the last commit.
  item('git.discard', () => confirmThis({
    head: tr('git.discard_head', {path: path}),
    body: tr('git.discard_body'),
    keep: tr('action.cancel'), keepMark: '#i-sl-xmark',
    doIt: tr('git.discard'), doMark: '#i-sl-eraser',
    go: () => gitRun(a, 'discard', {path: path}),
  }), '#i-sl-eraser');
  item('files.delete', () => confirmThis({
    head: tr('files.delete_head', {path: path}),
    body: tr('files.delete_body'),
    keep: tr('action.cancel'), keepMark: '#i-sl-xmark',
    doIt: tr('files.delete'), doMark: '#i-sl-trash-can',
    go: () => send('delete').then(() => {
      // A tab showing a file that is no longer there is a tab showing nothing.
      openFiles = openFiles.filter(p => p !== path && diffPath(p) !== path);
      if (cardShows === path || diffPath(cardShows) === path) cardShows = 'facts';
      drawCardTabs(a);
    }),
  }), '#i-sl-trash-can');
  open.onclick = ev => { ev.stopPropagation(); menu.open = !menu.open; };
  box.append(open, menu);
  return box;
}

function treeRow(a, e, path, depth) {
  const row = document.createElement('button');
  row.type = 'button';
  // No hit48 here, and that is deliberate. It and the state layer are both ::after — one element —
  // so a row wearing both got a hover wash 48px tall over a 26px row, spilling onto its
  // neighbours. A tree is a dense list read with a mouse, which is the case the guide allows 40dp
  // for; the row carries its own height instead, and grows on a touch screen where the finger is.
  row.className = 'treerow state' + (e.isDir ? ' dir' : '') + (cardShows === path ? ' now' : '');
  // The depth as a number the stylesheet can use, so the indent AND the guide line that marks it
  // come from one value rather than from two that have to agree.
  row.style.setProperty('--d', String(depth));
  const mark = iconOr(e.isDir ? '#i-sl-chevron-right' : '#i-sl-file-lines', e.isDir ? '▸' : '·',
                      'treemark' + (e.isDir && openDirs.has(path) ? ' open' : ''));
  if (mark) row.append(mark);
  row.append(cell('treename', e.name));
  row.onclick = () => {
    if (e.isDir) {
      if (openDirs.has(path)) openDirs.delete(path);
      else openDirs.add(path);
      loadTree(a);
      return;
    }
    openFile(a, path);
  };
  if (!may('shell')) return row;
  // The row and its menu travel together: the menu anchors to the control in it, and a row that
  // returned only the button would leave the menu behind when the tree is redrawn.
  const line = cell('treeline');
  const menu = rowMenu(a, e, path);
  line.append(row, menu);
  // The right button opens the same menu, which is where a person looks for it first.
  line.addEventListener('contextmenu', ev => {
    ev.preventDefault();
    const opener = menu.children[0];
    if (opener && opener.onclick) opener.onclick(ev);
  });
  return line;
}

// A diff is opened the way a file is, and lives in the same tab strip.
//
// One list rather than two: they are both "something about this workspace, up in the slot", they
// close the same way, and a reader switching between a file and its diff should not be switching
// between two mechanisms. The tab is named by a prefix — a path can be anything, and a prefix with
// a colon in it is a path git cannot produce, since a path is relative to the workspace root.
const DIFF = 'diff:';
const isDiff = p => String(p).startsWith(DIFF);
const diffPath = p => String(p).slice(DIFF.length).split('#')[0];
const diffWhich = p => (String(p).split('#')[1] || '');

async function openDiff(a, path, which) {
  const key = DIFF + path + '#' + (which || '');
  if (!openFiles.includes(key)) openFiles.push(key);
  cardShows = key;
  drawCardTabs(a);
  const got = await fetchOne('/diff' + qFor(a) + '&path=' + encodeURIComponent(path) +
                             '&which=' + encodeURIComponent(which || ''));
  if (cardShows !== key) return;
  drawDiff(path, which, got && typeof got.text === 'string' ? got.text : '');
  loadTree(a);
}

// The diff, coloured by what each line does and nothing else.
//
// No parsing beyond the first character of a line, which is the whole of what a unified diff
// promises: + is added, - is removed, @@ is where, and anything else is context. A renderer that
// tried to understand more would be a second implementation of the thing git just did.
function drawDiff(path, which, text) {
  const bar = cell('filebar');
  bar.append(cell('filedir', path + (which ? '  ·  ' + tr(DIFF_WHICH[which] || 'diff.unstaged')
                                            : '  ·  ' + tr('diff.unstaged'))));
  const body = document.createElement('pre');
  body.className = 'filecode diffbody';
  const lines = String(text).split('\n');
  if (!String(text).trim()) {
    fileViewEl.replaceChildren(bar, cell('filesnote', tr('diff.same')));
    showCard();
    return;
  }
  for (const line of lines) {
    const row = document.createElement('span');
    const c = line[0];
    row.className = 'dl' + (c === '+' ? ' add' : c === '-' ? ' cut' : c === '@' ? ' at' : '');
    row.textContent = line + '\n';
    body.append(row);
  }
  fileViewEl.replaceChildren(bar, body);
  showCard();
}

const DIFF_WHICH = {staged: 'diff.staged', untracked: 'diff.untracked', '': 'diff.unstaged'};

// openFile puts a file in the slot the facts card is in, behind a tab of its own.
async function openFile(a, path) {
  if (!openFiles.includes(path)) openFiles.push(path);
  cardShows = path;
  drawCardTabs(a);
  const got = await fetchOne('/file' + qFor(a) + '&path=' + encodeURIComponent(path));
  if (cardShows !== path) return;            // somebody moved on while it was fetching
  drawFile(path, got && got.text ? got.text : tr('files.unreadable'));
  loadTree(a);
}

// The file, as the agent's own read tool rendered it — line numbers and all. Not re-numbered here
// and not stripped: a person and their companion pointing at different line 40s is the whole cost
// of tidying this up.
// Whether the file showing is being edited, and what it said when it was opened for editing — so
// "cancel" can put back exactly what was there rather than re-fetching a file the agent may have
// changed in the meantime.
let editing = null;

function drawFile(path, text) {
  // The WHOLE path, not just the directory. The tab carries the name so the reader can find the
  // file among the open ones; this line is the one they copy into a message or a command, and half
  // a path is not something anybody can paste. It is a bar of its own — a surface a step up, ruled
  // off from the code under it — because a path over a file is a label and not the file's first
  // line, which is what an unruled one read as.
  // One place for this file's controls, at the top and in the same corner whichever mode it is in:
  // "edit" becomes "save · cancel" where the edit button was. They were at the foot of the editor,
  // which on an ordinary wide screen is below the fold — somebody had to scroll a 28rem box past
  // the thing they had just typed to reach the button for it. A toolbar over the document is what
  // every editor does with the same problem.
  const bar = cell('filebar');
  bar.append(cell('filedir', path));
  const acts = cell('fileacts');
  bar.append(acts);
  // Editing is offered only to somebody who may — the server refuses regardless, and a button that
  // answers 403 is one people learn not to press. `shell` is the gate: anybody who can run a
  // command in that workspace can already write any file in it.
  if (may('shell') && editing !== path) {
    const go = withMark(document.createElement('md-text-button'), '#i-sl-pen-to-square');
    label(go, tr('action.edit'));
    go.onclick = () => { editing = path; drawFile(path, text); };
    acts.append(go);
  }
  if (editing === path) {
    fileViewEl.replaceChildren(bar, editor(path, text, acts));
    showCard();
    return;
  }
  const box = cell('filebody');
  box.append(...codeBlocks(text, path));
  fileViewEl.replaceChildren(bar, box);
  showCard();
}

// The editor: the file as it is, and two buttons.
//
// Plain text, no line numbers, no highlighting — what is being changed has to be exactly what gets
// sent, and a gutter woven into the text is the thing this pane already had to take apart to make
// a drag copy the code. The numbers come back the moment it is saved and read again.
//
// The text sent is the WHOLE file. An edit expressed as "replace this string" needs the console and
// the file to agree about what is in it right now, and between opening this and pressing save the
// agent may have written it twice — which is also why saving re-reads afterwards rather than
// assuming what is on disk is what was typed.
// Whether the model is asked to look at what is being typed, and what it last said.
//
// Off unless somebody turns it on, and remembered: it spends the backend on every pause in typing,
// which is a cost the person doing the typing should choose rather than discover.
let lookOn = localStorage.getItem('lookover') === 'on';
let lookAt = 0;

function editor(path, text, acts) {
  const box = cell('fileedit');
  // The library's field with type=textarea, not a bare one: a bare textarea inherits the body's
  // 14px, and iOS Safari zooms the whole page in on a field under 16 and does not zoom back. This
  // page has a guard that fails the build over exactly that.
  const area = document.createElement('md-outlined-text-field');
  area.setAttribute('type', 'textarea');
  area.setAttribute('rows', '20');
  area.setAttribute('spellcheck', 'false');
  area.className = 'fileeditarea';
  area.value = plainText(text);
  // The same marking as the reading view, behind the text being typed.
  //
  // A textarea cannot hold coloured runs — it holds a string — so the colour goes on a copy of the
  // text UNDER it, in the same face at the same size, with the field itself made transparent. That
  // is how every browser editor that is not a rewritten text engine does it, and it costs nothing
  // when it drifts: the worst case is colour a pixel out of place behind a perfectly readable
  // caret. It is redrawn as you type, and scrolls with the field.
  const behind = document.createElement('pre');
  behind.className = 'filecode editghost';
  behind.setAttribute('aria-hidden', 'true');
  const repaint = () => {
    behind.replaceChildren();
    for (const part of codeParts(String(area.value || ''), commentMark(path))) {
      if (!part.cls) { behind.append(document.createTextNode(part.text)); continue; }
      const m = document.createElement('span');
      m.className = part.cls;
      m.textContent = part.text;
      behind.append(m);
    }
    // A trailing newline so the last line has somewhere to be, the way a textarea keeps one.
    behind.append(document.createTextNode('\n'));
  };
  repaint();
  // What the model made of it, above the buffer: it is about what is on the screen, and putting it
  // under a 28rem editor is putting it off the bottom of the pane.
  const said = cell('looksaid');
  said.hidden = true;
  // The switch, and the only thing that turns this on. A model reading over somebody's shoulder is
  // a good idea and a bill; which of the two it is depends on whether they asked for it.
  const ask = async () => {
    if (!lookOn || !may('prompt')) { said.hidden = true; return; }
    const mine = ++lookAt;
    const out = await postText('/look' + qFor(lastDrawnFor || {socket: ''}),
                               new URLSearchParams({path: path, text: area.value}));
    if (mine !== lookAt) return;             // they kept typing; this answer is about older text
    // Silence is the answer when there is nothing worth saying. No panel, no "looks good" — a
    // reviewer that always finds three things is one people stop reading.
    said.textContent = (out || '').trim();
    said.hidden = !said.textContent;
  };
  // On a pause, not on a keystroke. Two seconds is long enough that a sentence being typed is not
  // sent five times and short enough that stopping to think gets an answer while it is still about
  // what you were thinking.
  area.addEventListener('input', () => {
    repaint();
    const mine = ++lookAt;
    setTimeout(() => { if (mine === lookAt) { lookAt = mine - 1; ask(); } }, 2000);
  });
  // The colour scrolls with the text it is under. Read from the field's own scroller, which is
  // inside its shadow root — the host does not scroll, so listening to the host alone would leave
  // the colour standing still under a moving caret.
  const inner = area.shadowRoot && area.shadowRoot.querySelector('textarea');
  if (inner) {
    inner.addEventListener('scroll', () => {
      behind.scrollTop = inner.scrollTop;
      behind.scrollLeft = inner.scrollLeft;
    }, {passive: true});
  }
  const save = label(withMark(document.createElement('md-filled-button'), '#i-sl-floppy-disk'),
                     tr('action.save'));
  const opened = plainText(text);
  save.onclick = async () => {
    save.disabled = true;
    // A patch when one can be made, the file when it cannot. The patch is smaller and it is the
    // only version that can refuse — see unifiedDiff.
    const body = new URLSearchParams({path: path});
    const patch = unifiedDiff(opened, area.value, path);
    if (patch) body.set('patch', patch);
    else body.set('text', area.value);
    // The companion is named ONCE. post() addresses it from the socket and peer it is given —
    // passing a path that already carries ?d= as well produced /save?d=…?d=…, which resolves to a
    // socket path with a query string welded to the end of it. The console then said "no daemon at
    // …?d=…" and every workspace-changing action from this page had been failing that way: the
    // demo mocks every POST and the Go tests call the handler directly, so neither could see it.
    const why = await post('/save', body,
                           (lastDrawnFor || {}).socket || '', (lastDrawnFor || {}).peer || '');
    save.disabled = false;
    if (why) {
      // A refusal here is usually the file having moved: the companion edited it while this was
      // open. Said where the model's own remarks go, because it is about this buffer.
      said.textContent = why;
      said.hidden = false;
      return;
    }
    editing = null;
    // Read back rather than drawn from what was typed: the file on disk is the fact, the tool may
    // have written it differently (a missing final newline), and the companion has just been told
    // in its own log that this happened.
    openFile(lastDrawnFor, path);
  };
  const stop = withMark(document.createElement('md-text-button'), '#i-sl-xmark');
  label(stop, tr('action.cancel'));
  stop.onclick = () => { editing = null; drawFile(path, text); };
  // Into the bar at the top, where the edit button was: the control that starts this and the two
  // that end it are the same control in three states, and a control that moves is one you look for.
  acts.append(save, stop);
  // The switch only where the capability is: asking costs the backend, and a control that answers
  // 403 is one people learn not to press.
  // One box holding both, the colour under and the field over it.
  const stack = cell('editstack');
  stack.append(behind, area);
  box.append(said, stack);
  return box;
}

// A unified diff between what was opened and what is in the box now.
//
// # Why the page makes one at all
//
// Saving used to send the whole file. For a page of prose that is nothing; for a source file of
// four thousand lines it is the whole thing over a tunnel on every save — and, worse, the last
// writer wins. A patch is smaller and it carries CONTEXT, which is what lets the far side refuse:
// if the agent has edited that file since this was opened, the context no longer matches, git says
// so, and a save that would have thrown somebody's work away becomes a sentence instead.
//
// # The algorithm, and its bound
//
// Common head and tail are trimmed off — which for an ordinary edit leaves a handful of lines —
// and what is left is diffed with a plain LCS table. That table is O(n×m), so it is only entered
// when the changed middle is small; past the bound this answers with nothing and the caller sends
// the file, which is the honest fallback rather than a worse diff.
function unifiedDiff(before, after, path) {
  if (before === after) return '';
  // A file that ends in a newline has as many LINES as it has newlines — the empty string after
  // the last one is not a line, and splitting on "\n" invents it. The patch then carried a
  // trailing context line nothing on disk matched, so `git apply` refused every save of every
  // ordinary text file: "main.go changed since you opened it". Both sides lose it, so a patch that
  // adds or removes the final newline still shows up as a change to the last real line.
  const rows = t => { const l = String(t).split('\n'); if (l.length && l[l.length - 1] === '') l.pop(); return l; };
  const a = rows(before), b = rows(after);
  let head = 0;
  while (head < a.length && head < b.length && a[head] === b[head]) head++;
  let tail = 0;
  while (tail < a.length - head && tail < b.length - head &&
         a[a.length - 1 - tail] === b[b.length - 1 - tail]) tail++;
  const mid = {a: a.slice(head, a.length - tail), b: b.slice(head, b.length - tail)};
  // 4,000 cells per side is a change of a few hundred lines, which is a very large hand edit and
  // still a table of sixteen million booleans at the limit — beyond it, the file is the cheaper
  // thing to send.
  if (mid.a.length > 4000 || mid.b.length > 4000) return '';
  const ops = lcsOps(mid.a, mid.b);
  // Three lines of context either side, which is what git writes and what makes a refusal mean
  // "this moved" rather than "this file is not identical".
  const ctx = 3;
  const from = Math.max(0, head - ctx);
  const toEnd = Math.min(a.length, a.length - tail + ctx);
  const lines = [];
  for (let i = from; i < head; i++) lines.push(' ' + a[i]);
  for (const op of ops) lines.push(op);
  for (let i = a.length - tail; i < toEnd; i++) lines.push(' ' + a[i]);
  const oldCount = toEnd - from;
  const newCount = oldCount - mid.a.length + mid.b.length;
  const p = String(path);
  return 'diff --git a/' + p + ' b/' + p + '\n' +
         '--- a/' + p + '\n+++ b/' + p + '\n' +
         '@@ -' + (from + 1) + ',' + oldCount + ' +' + (from + 1) + ',' + newCount + ' @@\n' +
         lines.join('\n') + '\n';
}

// lcsOps turns two runs of lines into the +/- lines between them, longest common subsequence
// first: the point is to keep the lines that did not change OUT of the patch, or a one-word edit
// in the middle of a function arrives as the whole function removed and added again.
function lcsOps(a, b) {
  const n = a.length, m = b.length;
  const table = [];
  for (let i = 0; i <= n; i++) table.push(new Uint32Array(m + 1));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      table[i][j] = a[i] === b[j] ? table[i + 1][j + 1] + 1
                                  : Math.max(table[i + 1][j], table[i][j + 1]);
    }
  }
  const out = [];
  let i = 0, j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) { out.push(' ' + a[i]); i++; j++; }
    else if (table[i + 1][j] >= table[i][j + 1]) { out.push('-' + a[i]); i++; }
    else { out.push('+' + b[j]); j++; }
  }
  while (i < n) { out.push('-' + a[i]); i++; }
  while (j < m) { out.push('+' + b[j]); j++; }
  return out;
}

// plainText strips the read tool's gutter, because what is edited has to be what is sent.
//
// The same split codeBlocks makes, in the other direction: a numbered line is digits, a tab, the
// line. A line that is not numbered is passed through as it is — the tool cuts a long file off
// with a sentence of its own, and mangling that would hide it.
function plainText(text) {
  return String(text).split('\n').map(line => {
    const tab = line.indexOf('\t');
    return tab > 0 && /^\s*\d+$/.test(line.slice(0, tab)) ? line.slice(tab + 1) : line;
  }).join('\n');
}

// codeBlocks splits the tool's output into two columns: the line numbers, and the file.
//
// Two blocks and not one, because of what happens when somebody drags across it. The read tool
// writes "   12\tthe line", and as one run of text a selection takes the numbers with it — paste
// that into a terminal and every line has a number welded to its front. The gutter is its own
// column, unselectable, so a drag across the code copies the code.
//
// It also has to stay put sideways: a long line scrolls the block, and a gutter that scrolled with
// it would leave the numbers off the left edge exactly when a reader is furthest into a line.
function codeBlocks(text, path) {
  const comment = commentMark(path);
  const nums = document.createElement('pre');
  nums.className = 'filegutter';
  // Hidden from a screen reader: it reads the file, and a column of bare numbers between it and
  // every line is noise that cannot be skipped.
  nums.setAttribute('aria-hidden', 'true');
  const code = document.createElement('pre');
  code.className = 'filecode';
  let gutter = '';
  for (const line of String(text).split('\n')) {
    const tab = line.indexOf('\t');
    let body = line;
    if (tab > 0 && /^\s*\d+$/.test(line.slice(0, tab))) {
      gutter += line.slice(0, tab).trim() + '\n';
      body = line.slice(tab + 1);
    } else {
      // A line the tool did not number keeps its place in the column, or every number below it
      // would point at the wrong line.
      gutter += '\n';
    }
    for (const part of codeParts(body, comment)) {
      if (!part.cls) { code.append(document.createTextNode(part.text)); continue; }
      const m = document.createElement('span');
      m.className = part.cls;
      m.textContent = part.text;
      code.append(m);
    }
    code.append(document.createTextNode('\n'));
  }
  nums.textContent = gutter;
  return [nums, code];
}

// paintCode marks the parts of a file that are not code: comments, strings, numbers.
//
// # Why not a highlighter
//
// Highlighting properly means a grammar per language and a parser to run it — that is a library
// the size of everything else this page vendors put together, for a pane that exists so somebody
// can glance at the file their companion just mentioned. And a half-parser is worse than none: it
// colours a keyword inside a string and the reader stops trusting every colour on the screen.
//
// So this marks only what can be found by scanning a line left to right and cannot be got wrong in
// a way that misleads: a comment that runs to the end of the line, a quoted string, and a bare
// number. Nothing here claims to know the language beyond which character starts a comment in it.

// commentMark is what starts a comment to the end of the line in this kind of file, or "" when
// this page does not know — in which case nothing is marked as one, which is the honest answer.
function commentMark(path) {
  const ext = String(path).split('.').pop().toLowerCase();
  if (['go', 'js', 'mjs', 'ts', 'tsx', 'jsx', 'css', 'java', 'c', 'h', 'cc', 'cpp', 'rs', 'swift',
       'kt', 'scala', 'php', 'sql'].includes(ext)) return '//';
  if (['py', 'sh', 'bash', 'zsh', 'rb', 'yml', 'yaml', 'toml', 'conf', 'ini', 'mk'].includes(ext) ||
      ['makefile', 'dockerfile'].includes(String(path).split('/').pop().toLowerCase())) return '#';
  if (['lua', 'sql'].includes(ext)) return '--';
  return '';
}

// codeParts splits one line into the pieces worth marking. A scan, not a parse: a quote opens a
// string and the next matching quote closes it, and a comment mark outside a string runs to the
// end. Anything it cannot place stays plain, which is what "not a parser" has to mean.
function codeParts(code, comment) {
  const out = [];
  let plain = '';
  const flush = () => { if (plain) { out.push({text: plain}); plain = ''; } };
  for (let i = 0; i < code.length; i++) {
    const c = code[i];
    if (comment && code.startsWith(comment, i)) {
      flush();
      out.push({text: code.slice(i), cls: 'tok-note'});
      return out;
    }
    if (c === '"' || c === "'" || c === '`') {
      let j = i + 1;
      while (j < code.length && code[j] !== c) j += code[j] === '\\' ? 2 : 1;
      flush();
      out.push({text: code.slice(i, Math.min(j + 1, code.length)), cls: 'tok-text'});
      i = j;
      continue;
    }
    if (/[0-9]/.test(c) && !/[\w.]/.test(code[i - 1] || ' ')) {
      let j = i;
      while (j < code.length && /[\w.]/.test(code[j])) j++;
      flush();
      out.push({text: code.slice(i, j), cls: 'tok-num'});
      i = j - 1;
      continue;
    }
    plain += c;
  }
  flush();
  return out;
}

// drawCardTabs says which of the things sharing the slot is showing.
//
// Hidden while the facts are the only thing in there: a strip with one tab is a heading that looks
// like a control. md-secondary-tab, because this switches CONTENT inside a pane rather than moving
// between destinations — which is the distinction the guide draws between the two kinds of tab.
function drawCardTabs(a) {
  if (!openFiles.length) {
    cardTabs.hidden = true;
    cardTabs.replaceChildren();
    showCard();
    return;
  }
  const tabs = [];
  const facts = document.createElement('md-secondary-tab');
  facts.textContent = tr('field.facts');
  facts.onclick = () => { cardShows = 'facts'; showCard(); drawCardTabs(a); };
  tabs.push(facts);
  for (const path of openFiles) {
    const t = document.createElement('md-secondary-tab');
    t.append(cell('tablbl', isDiff(path) ? baseName(diffPath(path)) + ' ±' : baseName(path)));
    // A way to shut it, on the tab, which is where an editor puts it. An icon button inside a tab
    // would be a target inside a target; this is a plain mark with its own click, and the tab
    // keeps its own.
    const x = iconOr('#i-sl-xmark', '×', 'tabclose');
    if (x) {
      x.onclick = ev => {
        ev.stopPropagation();
        openFiles = openFiles.filter(p => p !== path);
        if (cardShows === path) cardShows = openFiles[openFiles.length - 1] || 'facts';
        drawCardTabs(a);
        if (cardShows !== 'facts') {
          if (isDiff(cardShows)) openDiff(a, diffPath(cardShows), diffWhich(cardShows));
          else openFile(a, cardShows);
        }
        loadTree(a);
      };
      t.append(x);
    }
    t.onclick = () => {
      if (isDiff(path)) openDiff(a, diffPath(path), diffWhich(path));
      else openFile(a, path);
    };
    tabs.push(t);
  }
  // Which one is showing, said on the TAB and not on the strip.
  //
  // activeTabIndex is the strip's view of a list it has not been given yet: the assignment ran in
  // the same breath as replaceChildren, before the component had seen its new children, so it
  // found nothing to mark — the strip read back -1 and the labels were not painted at all. A tab
  // bar with the right boxes, the right text in the DOM, the right colours computed, and nothing
  // on the screen. Measured: appending any node afterwards made the component notice and the
  // words appeared.
  //
  // The children carry the state instead, before they are handed over, which is the same rule the
  // pane handles and the chips follow — the component owns what it draws, and it reads `active`
  // off the tabs when it adopts them.
  const at = cardShows === 'facts' ? 0 : openFiles.indexOf(cardShows) + 1;
  const which = at < 0 ? 0 : at;
  tabs.forEach((t, i) => { t.active = i === which; });
  cardTabs.replaceChildren(...tabs);
  cardTabs.hidden = false;
  showCard();
}

// showCard draws whichever of the two the tab strip says.
function showCard() {
  const file = cardShows !== 'facts' && openFiles.includes(cardShows);
  fileViewEl.hidden = !file;
  // The facts card folds its own body away; here it goes altogether, because something else is
  // standing in its place. Empty means there is no companion drawn yet, and an empty card is not
  // shown either.
  const facts = document.getElementById('detail');
  facts.hidden = file || !facts.children.length;
}

function baseName(p) { return String(p).split('/').pop() || p; }

// shortPath is a workspace path with the home directory folded away — the head of a tree is a
// label, and /Users/somebody/work/thing takes the width of the pane to say "thing".
function shortPath(p) {
  const parts = String(p).split('/').filter(Boolean);
  return parts.slice(-2).join('/') || p;
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

// openFormat is the editor: one row per section, which is the pair a contract is made of.
//
// It fetches what is in force when the caller has not already got it, which is what the button in
// the facts does: the card is redrawn on every fleet poll, and asking the daemon for a contract
// that changes about as often as the model does — three seconds apart, forever — to fill in a line
// nobody was reading is a request paid for by every console watching.
async function openFormat(a, f) {
  fmtFor = a;
  if (!f) f = await fetchOne('/report-format' + qFor(a)) || {sections: []};
  // A headline that says what the dialog does, not what area of the app it belongs to. "Report
  // format" is a heading on a card; on a dialog it leaves the person to work out what saving will
  // change, which is the thing the guide asks the headline to answer.
  //
  // And it stopped calling this a report. A run that ends delivers one — that is the word's job
  // here — while this is the packet an agent puts in front of somebody to get a decision out of
  // them. One word for both left the screen ambiguous about which of the two it was editing; the
  // code still says report, because the contract, the route and the file are named that.
  fmtK.textContent = tr('fmt.headline');
  fmtForm.replaceChildren();
  // Supporting text, which is the part of a dialog the guide asks for and this one did without: a
  // headline states the subject and the sentence under it says what pressing save will mean. Here
  // that is worth saying outright — these are not preferences, they are what the agent will be
  // refused for leaving out.
  fmtForm.append(cell('dlgsup', tr('fmt.about')));
  // And where the one in force came from, because that is what "save" will change and it is not
  // the same act in the three cases: this companion's own workspace, everything under this console,
  // or nothing written down yet. Said here rather than on the card behind it — this is the moment
  // it matters, and it was costing a line on a card the rest of the time.
  // Literal keys in a lookup, not a key built by concatenation: a key the pack check cannot see is
  // the one that ships missing and renders as its own dotted name.
  const FROM = {workspace: 'fmt.from_workspace', console: 'fmt.from_console', default: 'fmt.from_default'};
  fmtForm.append(cell('dlgsup from', tr(FROM[f.from] || FROM.default)));
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
    // Array.from: children is an HTMLCollection in a browser and has no .filter, so this threw
    // where it matters most — on the way to saving what somebody just typed.
    const fields = Array.from(row.children || []).filter(c => c.name === 'key' || c.name === 'prompt');
    const key = (fields.find(c => c.name === 'key') || {}).value || '';
    const prompt = (fields.find(c => c.name === 'prompt') || {}).value || '';
    if (!String(key).trim()) continue;
    body.append('key', key);
    body.append('prompt', prompt);
  }
  const why = await post('/report-format', body, fmtFor.socket, fmtFor.peer);
  if (!why) drawDetail(fmtFor);
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
  if (!items || !items.length) { showSide(box, false); box.replaceChildren(); return; }
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
  box.replaceChildren(markedKey('#i-sl-layer-group', tr('field.queued', {n: items.length})), ...rows);
  showSide(box, true);
}

// justFailed is a child that ended badly within the last few minutes.
//
// Long enough that somebody who was looking elsewhere still finds out, short enough that the strip
// empties. Without the second half it is not a strip of what is happening, it is a scar.
const FAILED_FOR = 5 * 60 * 1000;
function justFailed(c) {
  if (!c.err) return false;
  if (!c.ended) return true;                     // ended without saying when: show it
  const at = Date.parse(c.ended);
  return !at || Date.now() - at < FAILED_FOR;
}

// taskLine is the first line of a child's task that says anything about the task.
//
// A spawned child is given a prompt, and the prompt is what gets recorded as its task — so a chip
// meant to say "what is this one doing" led with the scaffolding: "THE QUESTION", "HOW TO ANSWER".
// The heading is for the model; the line under it is the answer to the reader's question.
function taskLine(task) {
  const lines = String(task).split('\n').map(l => l.trim()).filter(Boolean);
  for (const l of lines) {
    if (l === l.toUpperCase() && /[A-Z]/.test(l)) continue;   // a shouted heading, not the task
    return l;
  }
  return lines[0] || '';
}

async function loadJobs(a) {
  if (!a) { stripEl.hidden = true; stripEl.replaceChildren(); drawQueued(null); return; }
  const j = await fetchList('/jobs' + qFor(a)) || {};
  drawQueued(j.queued);
  const kids = j.children || [], bg = j.background || [];
  const chips = [];
  for (const c of kids) {
    // What is going on, and what has just gone wrong. Not everything that ever ran: the strip was
    // drawing every child the session had spawned, so after an hour of meetings a companion's page
    // carried fifteen chips, none of them running, each holding a slab of the prompt its turn was
    // given — which also read as the meeting's words having leaked onto the companion's own page.
    // A failure stays for a few minutes, because a subagent that died is the one thing here
    // somebody needs to be told about; everything finished is on the subagents screen, where
    // finished things live.
    if (!c.running && !justFailed(c)) continue;
    chips.push(jobChip(tr('detail.subagent'), c.tool || tr('detail.subagent'),
      oneLine(taskLine(c.task || ''), 48),
      {running: c.running, bad: !!c.err, go: () => goDeep('sub', c.id)}));
  }
  for (const b of bg) {
    chips.push(jobChip(tr('job.command'), oneLine(b.command || '', 40), lastLine(b.tail),
      {running: b.running, bad: !b.running && b.exit !== 0}));
  }
  // Hidden when it has nothing to say, decided after the chips are built rather than from the
  // length of what came back: with fifteen finished children and none of them drawn, the strip
  // stayed on screen as a band of nothing.
  stripEl.replaceChildren(...chips);
  stripEl.hidden = chips.length === 0;
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
  const box = scroller();
  const wasAt = box.scrollTop, wasTall = box.scrollHeight;
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
  if (stick) toBottom();
  else if (i === 0 && box.scrollHeight !== wasTall) {
    box.scrollTop = wasAt + (box.scrollHeight - wasTall);
  }
}

// The reader arriving at the top of the window is the ask for more of it.
//
// On scroll rather than on a control: what is above is not a page of results, it is the same
// conversation, and a button saying "earlier" in the middle of it would be furniture explaining a
// mechanism nobody asked about. The margin is a screen and a half, so the rows are there before the
// empty box is.
// Both boxes, because only one of them is scrolling at a time and a scroll inside an element does
// not reach the window. Same handler: what it asks is "is the top of what we are showing near the
// top of the box", which is the same question wherever the box is.
const reachedUp = () => {
  if (!above || !spacer.parentNode) return;
  if (spacer.getBoundingClientRect().bottom > -scroller().clientHeight * 1.5) {
    if (reachUp()) draw(lastRows);
  }
};
addEventListener('scroll', reachedUp, {passive: true});
log.addEventListener('scroll', reachedUp, {passive: true});

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
  if (!mayEl(stopBtn)) stopBtn.hidden = true;
  withMark(stopBtn, '#i-ss-circle-stop');
  railMenu.setAttribute('aria-label', tr('nav.menu'));
  // A secondary tab's indicator spans the tab; a primary tab's hugs its label. The bundle keeps
  // that as a reactive @state with no attribute behind it, so it is set as a property — assigning
  // it re-renders the tab with the indicator on the button instead of on the content.
  for (const id of ['ptabTalk', 'ptabState']) document.getElementById(id).fullWidthIndicator = true;
  // The waiting badge changes parent with the rail, per the spec: on the icon while collapsed,
  // beside the label once there is one.
  refreshSideToggle();
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
  paintTheme();
  prefsEl.setAttribute('aria-label', tr('nav.preferences'));
  document.getElementById('lookK').textContent = tr('files.look');
  document.getElementById('lookWhy').textContent = tr('files.look_why');
  document.getElementById('accessK').textContent = tr('nav.access');
  document.getElementById('accessWhy').textContent = tr('access.why');
  label(document.getElementById('accessGo'), tr('access.open'));
  prefsClose.textContent = tr('action.close');
  withMark(prefsClose, '#i-sl-xmark');
  prefsK.textContent = tr('nav.preferences');
  consoleK.textContent = tr('nav.this_console');
  // Both keys written out rather than built as key + '_sub': the phrase pack's own audit finds
  // unused phrases by grepping for the literal, and a key assembled at runtime is invisible to it
  // — which would leave four translated lines nobody could tell were still reachable.
  for (const [el, key, sub] of [[railFleet, 'nav.companions', 'nav.companions_sub'],
                                [railSkills, 'nav.shared', 'nav.shared_sub'],
                                [railMeet, 'nav.meet', 'nav.meet_sub'],
                                [railAccess, 'nav.access', 'nav.access_sub']]) {
    // The word is on the item whether or not it is drawn: collapsed, the icon is all there is to
    // see, and a rail nobody can read aloud is not a navigation. The icon itself is markup and is
    // not touched here — a shape does not need translating, and rebuilding it on every language
    // change would throw away four elements to replace them with the same four.
    el.setAttribute('aria-label', tr(key));
    el.querySelector('.lbl').textContent = tr(key);
    // And what is behind it, one line, drawn only when the rail is open. Open and closed carried
    // the same four words at two sizes, so widening the rail bought room and spent it on nothing —
    // while "shared" and "meeting" are exactly the two a newcomer cannot guess. The stylesheet
    // hides it collapsed; the text is written either way so a language change reaches it.
    el.querySelector('.sub').textContent = tr(sub);
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
  document.getElementById('langK').textContent = tr('pref.lang');
  paintChoice(langEl, 'lang', true);
  if (consoleEl.children.length) loadConsole();
loadMe();   // its two labels are words too
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
  else if (view() === 'access' && mayEl(accessEl)) loadAccess();

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
function paintChoice(el, kind, named) {
  const c = CHOICES[kind];
  // `named` says the row already carries the setting's name, so the field must not repeat it: the
  // theme sits in a row whose label is "Theme" and a floating label saying it again is the word
  // twice in one line. The name still reaches anything that reads the field on its own.
  if (named) {
    el.removeAttribute('label');
    el.setAttribute('aria-label', tr(c.label));
  } else {
    el.setAttribute('label', tr(c.label));
  }
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
  // The workspace panes belong to the companion that was open. Left standing, the next one's page
  // would draw somebody else's tree for the length of a poll — and a file tab from a workspace
  // that is no longer on screen is a tab that reads from a companion nobody is looking at.
  lastDrawnFor = null;
  openFiles = [];
  cardShows = 'facts';
  editing = null;
  openDirs.clear();
  findQ = '';
  filesEl.replaceChildren();
  fileViewEl.replaceChildren();
  fileViewEl.hidden = true;
  cardTabs.hidden = true;
  cardTabs.replaceChildren();
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
  // The map is the companions destination seen another way, so that is the one that stays lit
  // while you stand in it — the same reason a companion's own page keeps it lit.
  for (const [el, key] of RAILS) {
    el.toggleAttribute('selected', s || v === 'map' ? key === 'fleet' : v === key);
  }
  fleetEl.hidden = !!s || v !== 'fleet';
  summaryEl.hidden = !!s || v !== 'fleet';
  skillsEl.hidden = !!s || v !== 'skills';
  boardEl.hidden = !!s || v !== 'board';
  mapEl.hidden = !!s || v !== 'map';
  // Hidden by the view AND by the capability, like the access screen: a meeting spends model turns
  // on several companions at once, so somebody who may not prompt should not arrive at the form by
  // editing the address either. The server refuses regardless.
  meetEl.hidden = !!s || v !== 'meet' || !mayEl(meetEl);
  mcpEl.hidden = !!s || v !== 'skills';
  // Hidden by the view AND by the capability: a screen somebody may not use is one they should not
  // be able to arrive at by editing the address either.
  accessEl.hidden = !!s || v !== 'access' || !mayEl(accessEl);
  // Only on a companion's own page. Addressing one by typing its name into a box, from a list where
  // it is already on screen and one click away, is a second way to do the thing the list does — and
  // the harder one: it asks somebody to spell a name they can see.
  // The conversation and everything that acts on it belong to the companion's page, not to a
  // screen about one piece of what happened there. Standing in a verdict, "send" would put a
  // message into a conversation that is not on screen.
  const deepNow = deepIn();
  // One deep screen keeps the composer: a session's own transcript. The rule the others fail is
  // that the conversation is not on screen — standing in a verdict, "send" would put a message
  // into a conversation you cannot see. Standing in a session, you are looking at the conversation
  // the message would go to; it is simply not the one the companion is in yet, and that is a
  // question the composer asks before it sends rather than a reason to take the box away.
  const onSession = pastOn() && !!pastOf();
  document.getElementById('agentdetail').hidden = !deepNow;
  streamEl.hidden = !!deepNow;
  f.hidden = !s || (deepNow && !onSession);
  // Navigation changes which conversation the box would reach, and arriving at a session screen
  // does not draw a prompt — so the composer is told here as well as when a question appears.
  composerReach();
  // Nothing to interrupt from the fleet view — and nothing to offer somebody who may not answer,
  // which is the same permission: stopping a turn is deciding for it.
  const stopBtn = document.getElementById('stop');
  stopBtn.hidden = !s || deepNow || !mayEl(stopBtn);
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
  for (const el of [fleetEl, skillsEl, boardEl, mcpEl, accessEl, streamEl]) reveal(el);
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
  if (v === 'map') {
    // Live, like the table it is another view of: a picture of who is talking to whom is worth
    // nothing if it is a picture of five minutes ago. Same interval as the fleet poll, and the
    // same clean-up path — render() clears fleetTimer on the way out of every view.
    loadMap();
    fleetTimer = setInterval(loadMap, 3000);
    return;
  }
  if (v === 'meet') {
    // Polled, and only while the meeting is still somewhere. A turn takes a minute, so two seconds
    // is well inside the grain of what changes — and a stream would arrive no sooner.
    //
    // The redraw does not take the box out from under a person mid-sentence: the topic and what is
    // being typed live outside the render, and the room is only rebuilt when the poll actually
    // finds something different. A meeting that has gone stops the poll rather than asking a
    // console that has already answered "no such meeting" once every two seconds for the evening.
    loadMeet();
    fleetTimer = setInterval(() => {
      // A meeting that has gone stops the poll, rather than asking a console that has already
      // answered "no such meeting" once every two seconds for the rest of the evening.
      if (meetGone) { clearInterval(fleetTimer); fleetTimer = null; return; }
      loadMeet();
    }, 2000);
    return;
  }
  if (v === 'access') {
    // Read once and not polled: this list changes when somebody joins or leaves, which is not on a
    // three-second clock — and a table that reorders itself while an admin is picking a role is
    // worse than one a minute old.
    //
    // Not asked for at all when it may not be had. The server refuses either way; a fetch that
    // exists only to be refused is a 403 in somebody's audit record with nothing behind it.
    if (mayEl(accessEl)) loadAccess();
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
// The rail's destinations, addressed the same way the tabs are. They are anchors with an href, so
// the click is intercepted like every other in-page link, and a middle click or a copied address
// still lands. Access is one of them for the same reason it is in the rail at all: it is a screen
// with an address, and the only thing different about it is that it sits at the foot.
const RAILS = [[railFleet, 'fleet'], [railSkills, 'skills'], [railMeet, 'meet'],
               [railAccess, 'access']];
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

// One handle, wired once, for both panes.
//
// # The button owns "selected", and the page reads it
//
// A toggle icon button flips its own `selected` — and it does so AFTER the click has finished
// propagating, from the value it captured before. So a click listener that assigns `selected` is
// assigning to something the component overwrites a microtask later, from a value the listener has
// already changed. That is what the left pane was doing, and the handle ended up saying the
// opposite of what the pane was: shut and lit, open and dark, and the click that should have
// closed it opened it again. It is not a race to be won — the component is the owner. So the page
// listens for `change`, reads what the button now is, and makes the pane match.
//
// # Both panes, one function
//
// They were two copies, and the copies differed in exactly one character: one read "is it open"
// as `!== 'shut'` and the other as `=== 'open'`. With the attribute left unset for a remembered-
// open pane — which is what the init below used to do — those two are not the same question, and
// the second one answers "shut" about a pane that is plainly open. One implementation cannot drift
// from itself.
//
// SHUT unless somebody opened it. It used to be the other way round, and each pane took its width
// from the best place on the page the moment there was room — at a 900px window that left the
// conversation 44 characters a line. What is in them is reference; the conversation is what the
// page is for. Remembered, because a pane you shut should stay shut when you open the next
// companion: reopening it every time would make the button feel like it did nothing.
//
// Stored as the word "open" rather than as an empty string, so the default can be read off the
// absence of a value — an empty string and "never chosen" were the same thing before, which is why
// the default could not be changed without also forgetting everybody's choice. The ATTRIBUTE is
// always written, both ways: "the attribute is missing" as a third state is what let the two
// predicates above disagree.
function paneHandle(el, key, opened) {
  const say = open => {
    document.body.setAttribute(key, open ? 'open' : 'shut');
    localStorage.setItem(key, open ? 'open' : 'shut');
    // What a screen reader is told, from the same fact the drawing comes from, so the two cannot
    // come apart. The markup says "false" until this runs, which is the safe thing to have said.
    el.setAttribute('aria-expanded', String(open));
  };
  const open = localStorage.getItem(key) === 'open';
  say(open);
  el.selected = open;
  el.addEventListener('change', () => {
    const now = !!el.selected;
    say(now);
    if (now) opened?.();
    paint();
  });
}
// Asked for the first time when it opens: a pane nobody has opened has never cost a request.
paneHandle(filesToggle, 'files', () => loadTree(lastDrawnFor));
paneHandle(sideToggle, 'side');

// The look-over preference, wired where the other two preferences are. Remembered rather than
// asked again: it is true of the reader and not of the file, which is why it is here rather than
// on the editor.
const lookSwitch = document.getElementById('lookSwitch');
lookSwitch.selected = lookOn;
lookSwitch.addEventListener('change', () => {
  lookOn = !!lookSwitch.selected;
  localStorage.setItem('lookover', lookOn ? 'on' : 'off');
});

// The two grips: the edge of each pane, dragged.
//
// What they write is the custom property the grid track is made of, so the column and the pane
// pinned to it move together and nothing has to be told twice. Clamped, because a pane dragged to
// nothing is a pane somebody cannot get back without knowing where the invisible edge is — and one
// dragged past half the window has taken the conversation the page is for.
//
// A separator in the ARIA sense: it takes focus and arrow keys move it, which is the half of a
// splitter that is always left out. Remembered, like whether the pane is open at all.
function grip(el, prop, key, lead) {
  const root = document.documentElement;
  const rem = parseFloat(getComputedStyle(root).fontSize) || 16;
  const saved = parseFloat(localStorage.getItem(key) || '');
  if (saved) root.style.setProperty(prop, saved + 'rem');
  const clamp = w => Math.max(12, Math.min(40, w));
  const setW = w => {
    const at = clamp(w);
    root.style.setProperty(prop, at + 'rem');
    localStorage.setItem(key, String(at));
    el.setAttribute('aria-valuenow', String(Math.round(at)));
  };
  const widthNow = () => parseFloat(getComputedStyle(root).getPropertyValue(prop)) || 18;
  el.addEventListener('pointerdown', ev => {
    ev.preventDefault();
    el.setPointerCapture(ev.pointerId);
    el.classList.add('gripping');
    const from = ev.clientX, was = widthNow();
    const move = m => setW(was + (lead ? (from - m.clientX) : (m.clientX - from)) / rem);
    const done = () => {
      el.classList.remove('gripping');
      el.removeEventListener('pointermove', move);
      el.removeEventListener('pointerup', done);
      el.removeEventListener('pointercancel', done);
    };
    el.addEventListener('pointermove', move);
    el.addEventListener('pointerup', done);
    el.addEventListener('pointercancel', done);
  });
  el.addEventListener('keydown', ev => {
    const step = ev.shiftKey ? 4 : 1;
    if (ev.key === 'ArrowLeft') { ev.preventDefault(); setW(widthNow() + (lead ? step : -step)); }
    if (ev.key === 'ArrowRight') { ev.preventDefault(); setW(widthNow() + (lead ? -step : step)); }
  });
}
grip(document.getElementById('filesGrip'), '--magi-comp-files-w', 'files.w', false);
grip(document.getElementById('sideGrip'), '--magi-comp-side-w', 'side.w', true);

// The masthead's real height, into the property the shell is sized from.
//
// Measured rather than assumed: the bar wraps at some widths and in some languages, and a shell
// sized against a guess either hides its last line under the fold or leaves a strip of nothing at
// the bottom. Same shape as the dock's measurement, which has been doing this for the composer.
function measureMasthead() {
  const bar = document.getElementById('masthead');
  if (!bar || typeof bar.getBoundingClientRect !== 'function') return;
  const h = Math.ceil(bar.getBoundingClientRect().height);
  if (h > 0) document.documentElement.style.setProperty('--magi-comp-masthead', h + 'px');
  // And where the shell actually BEGINS, which is not the same number.
  //
  // The columns were sized as 100dvh minus the masthead, and everything else between the top of
  // the window and the top of the content was unaccounted for: the page's own padding above main,
  // and in the demo a banner across the top. The column then ran past the bottom of the window by
  // exactly that much, so the last card in it was cut off by the edge of the screen — which is
  // what "the git card is stuck to the bottom with no room under it" was.
  const main = document.querySelector('main');
  if (main && typeof main.getBoundingClientRect === 'function') {
    const top = Math.ceil(main.getBoundingClientRect().top);
    if (top > 0) document.documentElement.style.setProperty('--magi-comp-shelltop', top + 'px');
  }
}
measureMasthead();
addEventListener('resize', measureMasthead, {passive: true});

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
// Preferences is where the way to the people screen lives; see the markup for why it is not in the
// navigation. Closing the dialog first, because leaving a modal open over the screen it just took
// somebody to is the one thing a link out of a dialog must not do.
document.getElementById('accessGo').onclick = () => {
  prefsDialog.close('go');
  history.pushState({}, '', at(HREF.access));
  render();
};
// Painted when it OPENS, not before. A dialog does not render what is slotted into it until then,
// so a select told its value while the dialog was closed had no options to resolve it against and
// showed an empty field over a value it was holding.
prefsDialog.addEventListener('opened', () => { if (painted) paint(); paintNotify(); });
// The toggle writes the SAME preference the select does, so the two are one setting with two
// controls rather than two settings. Pressing it leaves 'system' behind on purpose: asking for the
// other theme is a choice, and pretending it was still deferring to the machine would mean the
// next OS change silently undid it.
themeToggle.onclick = () => {
  // A ring, and every stop moves the sky — so even the step that lands on the colour already
  // showing (following a machine that is already light, say) is a press that visibly did
  // something, which is the whole reason a cycling control is allowed here at all.
  const at = THEMES.findIndex(([v]) => v === prefOf('theme'));
  localStorage.setItem('theme', THEMES[(at + 1) % THEMES.length][0]);
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

// postText is post for a route whose ANSWER is the point rather than its silence.
//
// post() reports failures and swallows the body on success, which is right for an action: what a
// caller wants to know is whether it was refused. Asking the model what it makes of a file is the
// other shape — the body IS the answer — and a refusal here is drawn where the answer would have
// gone rather than announced across the page.
async function postText(path, body) {
  try {
    const r = await fetch(path + (path.includes('?') ? '' : q()), {method: 'POST', body});
    const said = (await r.text()).trim();
    return r.ok ? said : '';
  } catch {
    return '';   // the console is talking to something it cannot reach; the pane simply says nothing
  }
}

const t = document.getElementById('t');
// The field grows itself: it is a component with its own textarea in a shadow root, so measuring
// scrollHeight from out here reads the host and not the text. All that is left to do is re-measure
// the dock, because the transcript reserves whatever the dock is actually occupying.
const grow = () => measureDock();

// The transcript reserves whatever the dock is actually occupying. Its height changes with the
// composer as you type and with the prompt bar appearing, and a guessed constant either wastes a
// screen or hides the last thing the agent said — on a phone, the second.
//
// And the reader who was at the bottom stays there. The dock is fixed, so its own growth moves
// nothing; what moves is the padding the page reserves for it, which makes the document taller
// under somebody who was reading its last line. A question arriving with three options to press is
// enough to push the answer they were reading off the bottom of the screen — the transcript's own
// redraw anchors itself for exactly this reason, and this is the other thing that changes height.
//
// Measured BEFORE the property is set: the dock is fixed, so at this point the document is still
// the height it was and "at the bottom" is still answerable.
// The same failure once more, from the other direction: the transcript is set in a web font, and
// `font-display: swap` means every line is measured in a fallback face first and re-measured when
// the real one arrives. On a cold load that lands after the first draw, so the page changes height
// under somebody who was already at the foot of it. Once, when the fonts are in.
if (document.fonts && document.fonts.ready && document.fonts.ready.then) {
  document.fonts.ready.then(() => { if (atBottom()) toBottom(); });
}

const dock = document.getElementById('dock');
function measureDock() {
  const stick = atBottom();
  document.documentElement.style.setProperty('--dock', (dock.offsetHeight || 0) + 'px');
  if (stick) toBottom();
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
  // A conversation the companion is not in. Ask, then move, then send — in that order, and only
  // that order: sending first would put the words in the session it is in now, which is the one on
  // nobody's screen, and moving without asking would take somebody who opened last Tuesday's work
  // to read it and quietly make it the live conversation.
  const to = movingTo();
  if (to) {
    const a = shownAgent();
    confirmThis({
      head: tr('move.headline', {to: to}),
      body: tr('move.body', {from: (a && a.session) || tr('move.somewhere'), to: to}),
      keep: tr('action.cancel'), keepMark: '#i-sl-xmark',
      doIt: tr('action.move_and_send'), doMark: '#i-ss-paper-plane',
      go: () => {
        t.value = ''; grow();
        // Sequential on purpose: the send is only correct once the move has happened, and the move
        // can be refused — mid-turn, or a conversation this companion does not own. post() reports
        // the refusal itself, and returning here leaves what was typed unsent rather than sending
        // it somewhere nobody was looking.
        // post() answers with the REASON it failed and an empty string when it did not, so the
        // test is truthiness. Compared against false it was never true, and a move the companion
        // had refused — mid-turn, or a conversation belonging to another workspace — was followed
        // by the send anyway: the words went into whatever conversation it was actually in, which
        // is the one nobody was looking at, while the refusal flashed past as a toast.
        post('/resume', new URLSearchParams({session: to})).then(why => {
          if (why) { t.value = v; grow(); return; }
          post('/submit', new URLSearchParams({text: v}));
          // Back to the companion's own page: the conversation just became the current one, and
          // standing in the session view would leave the reply arriving on a screen behind this.
          goDeep('past', null);
        });
      },
    });
    return;
  }
  // The composer is only on a companion's page, so there is one place the work can go.
  t.value = ''; grow(); post('/submit', new URLSearchParams({text: v}));
};
// Enter sends on a keyboard and inserts a newline on a phone: a soft keyboard's return key is the
// only way to break a line there, and hijacking it leaves no way to write a second paragraph.
const touch = matchMedia('(hover: none)').matches;
t.onkeydown = e => { if (e.key === 'Enter' && !e.shiftKey && !touch) { e.preventDefault(); f.requestSubmit(); } };
// …and the fleet is re-read straight after, rather than at the next poll. Stopping a turn is the
// one action somebody takes because they want to do something ELSE immediately, and up to three
// seconds of a prompt bar that is no longer true is three seconds of typing into the wrong role.
document.getElementById('stop').onclick = () =>
  confirmStop(nameOf(sock()), () => post('/interrupt', null).then(loadFleet));

// The markup's own drawings give way to the baked ones, once, before the first paint. Not on every
// render: these eight elements are in the document from the start and outlive every redraw.
dressIcons();
paint();
render();
repaintable = true;
loadConsole();