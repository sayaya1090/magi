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
// Bumped whenever the label pack changes. A row's cache signature includes it, so a language that
// lands after a row was first drawn invalidates the cached (wrong-language) node — measured: a
// Korean console kept English status badges on rows drawn before the pack arrived, because the
// signature was the companion's data only.
let labelVer = 0;
labels$.subscribe(v => { L = v; labelVer++; });
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
// One writer for both halves.
//
// `panel` is the page's answer to "which half of a companion am I reading"; ptabs.activeTabIndex is
// the strip's. They were written in four places and reset in one — leaving a companion put `panel`
// back to talk and left the strip pointing at Workspace, so coming back drew the CONVERSATION under
// a tab that said Workspace. Reported from a phone, and it is the general shape the reporter
// suspected: the control keeps its state while the content behind it moves on.
function setPanel(name) {
  panel = name;
  const at = ['talk', 'facts', 'files', 'plan'].indexOf(name);
  if (at >= 0 && ptabs.activeTabIndex !== at) ptabs.activeTabIndex = at;
}
// A panel move is a step the Back button can take back — on the phone, where the four are whole
// screens. Reported: reading the workspace, Back went past the companion entirely, because the
// four screens were one history entry. The entry carries the panel in STATE and not in the URL,
// deliberately: a link somebody shares still lands on the conversation, and a reload still
// arrives at the conversation — what changes is only where Back goes.
function notePanelMove() {
  if (ptabs.hidden) return;
  history.pushState({panel: panel}, '', location.href);
}
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
    // Both halves, as they were. Nothing may stay hidden from a previous narrow visit — and no
    // panel is showing, because they are all showing.
    //
    // The panel the reader was standing on becomes the pane that holds it. Widening a window from
    // the workspace or from "going on" used to drop the panel and leave both panes shut, so the
    // screen became the facts card and nothing else — measured, no conversation and no panel, from
    // a phone-width screen that had both. A pane is where that content lives up here; opening it
    // is the same answer to "where did my screen go".
    const was = document.body.getAttribute('panel');
    if (was === 'files' && paneSays.files) paneSays.files(true);
    if (was === 'plan' && paneSays.side) paneSays.side(true);
    document.body.removeAttribute('panel');
    if (sideEl) sideEl.hidden = false;
    filesEl.hidden = false;
    log.hidden = !s;
    // The facts card is not simply "shown on a companion's page" any more: it shares its slot with
    // an open file, and which of the two is showing is the tab strip's answer. Said through
    // showCard so there is one place that decides, rather than three that have to agree.
    if (!s && detailEl) detailEl.hidden = true;
    else showCard();
    return;
  }
  // One of the four, and nothing else. The names are the tabs': talk, facts, files, plan.
  //
  // Said on the body FIRST, because the panes read it to decide whether they are on screen — and
  // the workspace tab loads its tree from inside this function. Written last, that load asked
  // "is the workspace showing?" while the body still said "talk", answered no, and returned:
  // an empty pane behind the tab until something else happened to redraw it. Nothing else does
  // any more — the roster arrives as a frame when it CHANGES rather than as a poll every three
  // seconds — so the empty pane was the end state, measured at 390x844 as 0 children after 10s.
  document.body.setAttribute('panel', panel);
  const talk = panel === 'talk';
  log.hidden = !talk;
  sideEl.hidden = panel !== 'plan';
  filesEl.hidden = panel !== 'files';
  // The file slot belongs to the workspace half, not above the conversation.
  //
  // It was drawn by showCard alone, which knows which card is showing and not which PANEL is — so
  // on a phone an open file sat between the header and the transcript and pushed the conversation
  // off the screen: measured at 390x844, 0px of conversation visible and 1636px of page under the
  // thumb, with the file's own Save five hundred pixels above the composer that answers the
  // companion. Two places to type on one screen, and neither near its own words.
  if (panel === 'files') {
    // The workspace, one thing at a time: the tree, or the git card, or the file that was opened
    // out of one of them — never all three stacked.
    //
    // Asked for here, because the pane handle used to be what asked. Below the breakpoint the
    // handle is gone and the tab is what opens this — and a tab that opens an empty box is worse
    // than the handle it replaced: measured, 0px of workspace behind it.
    // Only a redraw, so a listing read a moment ago will do. What arrives with a CHANGE — the
    // roster frame — reads for itself.
    if (lastDrawnFor) loadTree(lastDrawnFor, true);
    const open = wsShows !== 'files' && wsShows !== 'git' && openFiles.includes(wsShows);
    detailEl.hidden = true;
    filesEl.hidden = open;
    cardTabs.hidden = !open;
    fileViewEl.hidden = !open;
    // The list shows one card. Both were stacked, so the git card began below a tree of forty
    // names and nobody scrolled to it.
    filesEl.setAttribute('data-shows', wsShows);
  } else if (panel === 'facts') {
    cardTabs.hidden = true;
    fileViewEl.hidden = true;
    detailEl.hidden = !detailEl.children.length;
  } else {
    detailEl.hidden = true;
    fileViewEl.hidden = true;
    cardTabs.hidden = true;
  }
  if (panel === 'plan') {
    // One scrollable stack, phone and desk alike — the sections read as titled blocks, the way the
    // desk side column already shows them. It was a list you drilled into (tab → list → detail, a
    // three-level walk to see the plan) grouping four unlike things under one vague word; flattened,
    // every section is there at once with its own heading, and refreshSideToggle's own empty state
    // covers "nothing is going on". The cards manage their own visibility (each hides when empty),
    // so nothing here has to choose which one shows.
    for (const old of sideEl.querySelectorAll('.panelist, .panelback')) old.remove();
    sideEl.removeAttribute('data-shows');
  }
}
// What the companion said while somebody was on another half of its page.
//
// The composer is on every panel — telling the agent something while reading its work is half of
// why the file is on the screen — but the transcript is not, and on a phone an answer arriving
// while you edit lands on a screen nobody is looking at. The count is the only thing that says so.
let unread = 0;
function paintUnread() {
  const tab = document.getElementById('ptabTalk');
  if (!tab) return;
  const had = tab.querySelector('.tabbadge');
  if (had) had.remove();
  if (!unread || panel === 'talk') return;
  const b = cell('tabbadge', unread > 9 ? '9+' : String(unread));
  b.setAttribute('aria-label', tr('panel.unread', {n: unread}));
  tab.append(b);
}

// onePane is true where the window holds one thing at a time — the same breakpoint the panes use,
// asked of the window rather than assumed, because a phone turned sideways is not a phone.
//
// The guide draws the line here: "Compact and medium breakpoints: A single pane works best."
// Which of the plan screen's four a phone is showing. Above the breakpoint it is not read: the
// pane is a column and they are stacked in it, which is what a column is for.
// Empty means the LIST — which is where a phone starts, because the panel is a list of what is
// going on and the sections are what it drills into. It was 'plan', so the first thing a reader saw
// was one section with a way back to a list they had never been shown.
// The way back out of a section, at the top of it, where the file view already puts one.
function panelBack(word, go) {
  const box = cell('panelback');
  const b = label(withMark(document.createElement('md-text-button'), '#i-sl-chevron-left'), word);
  // The word alone is where it GOES, and read aloud that is the same name as the tab you are
  // standing on — two controls with one name on one screen, one of which leaves it. The label says
  // what pressing it does; the word stays as what is written on it.
  b.setAttribute('aria-label', tr('action.back_to', {name: word}));
  b.onclick = go;
  box.append(b);
  return box;
}

// Which half of the shared destination a phone is showing.
let sharedShows = 'skills';
const sharedTabs = document.getElementById('sharedTabs');

function drawSharedTabs() {
  // A tab role belongs to a tab SET. These are md-secondary-tabs in a plain div, so the
  // accessibility tree held bare `tab` nodes with no `tablist` around them — a role whose whole
  // contract (one of a set, arrow keys, a current one) has no owner to keep it. The strip is the
  // owner; it says so, and it says which destination it switches. (Two when this was written;
  // the wiki made it three, which is why the count is not spelled out here.)
  sharedTabs.setAttribute('role', 'tablist');
  sharedTabs.setAttribute('aria-label', tr('nav.shared'));
  // A tablist answers the arrow keys. Saying `role="tablist"` and not answering them is a promise
  // less kept than the orphan roles were: md-tabs installs this handler and there is no md-tabs
  // here, so it is installed once, from the same place that claims the role. It does not wrap —
  // the guide asks a tab set not to.
  if (!drawSharedTabs.keys) {
    drawSharedTabs.keys = true;
    sharedTabs.addEventListener('keydown', e => {
      if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return;
      const tabs = [...sharedTabs.children];
      const at = tabs.indexOf(document.activeElement);
      if (at < 0) return;
      const to = e.key === 'ArrowRight' ? Math.min(at + 1, tabs.length - 1) : Math.max(at - 1, 0);
      e.preventDefault();
      if (to !== at && tabs[to].focus) tabs[to].focus();
    });
  }
  const want = [['skills', 'nav.experience'], ['wiki', 'nav.wiki'], ['mcp', 'nav.mcp']];
  const same = [...sharedTabs.children].map(t => t.textContent).join('|') ===
               want.map(([, k]) => tr(k)).join('|');
  if (!same) {
    sharedTabs.replaceChildren(...want.map(([key, word]) => {
      const t = document.createElement('md-secondary-tab');
      t.textContent = tr(word);
      t.onclick = () => {
        if (sharedShows === key) return;
        sharedShows = key;
        // A step Back can take back, same as the companion's panels: on a phone these two are
        // whole screens, and switching them was invisible to the history — Back from the servers
        // half left the destination entirely. State only; the URL people share stays clean.
        history.pushState({shared: key}, '', location.href);
        render();
      };
      return t;
    }));
  }
  [...sharedTabs.children].forEach((t, i) => { t.active = want[i][0] === sharedShows; });
}

function onePane() {
  return typeof matchMedia === 'function' && matchMedia('(max-width:52.4375em)').matches;
}

// toWorkspacePanel puts the reader where the thing they just opened is.
//
// Only where the two halves are two screens. Opening a file from the tree while the conversation
// is showing used to load it into a panel nobody was looking at — the press appeared to do
// nothing, which is worse than the wedged card it replaced.
function toWorkspacePanel() {
  // What is open is recorded whatever the width, and only the MOVE is compact-only.
  //
  // wsShows is what the phone's workspace screen draws, and it was written here, below a return
  // that fires above the breakpoint. So a file opened on a wide screen was in cardShows and not in
  // wsShows — and a window narrowed afterwards drew the tree while the file sat in the DOM,
  // undrawable, with the Workspace tab unable to bring it back. Measured crossing 1200 → 839.
  wsShows = cardShows;
  if (ptabs.hidden) return;
  // Going back is the control on the bar above it.
  if (panel === 'files') {
    drawPanels();
    return;
  }
  setPanel('files');
  notePanelMove();
  drawPanels();
}

// toWorkspaceList is the way back out of an open file: the list it was opened from.
function toWorkspaceList(which) {
  wsShows = which || 'files';
  drawPanels();
}

// Only when the reader switched, not on the poll that redraws the facts four times a minute.
// Sideways, in the direction the reader moved. Talk sits left of state, so arriving at state comes
// in from the right and going back to talk comes in from the left — which is what tells somebody
// these two are peers rather than one being under the other.
function revealPanel(fromIndex) {
  const at = ['talk', 'facts', 'files', 'plan'].indexOf(panel);
  const how = fromIndex === undefined ? 'enter' : (at > fromIndex ? 'slideL' : 'slideR');
  reveal(panel === 'talk' ? log : panel === 'facts' ? detailEl
         : panel === 'files' ? filesEl : sideEl, how);
}
// The tab set does not wrap.
//
// The component's roving tabindex loops both ends: ArrowRight from the last tab lands on the
// first, ArrowLeft from the first on the last. The guide says not to — "it's not recommended to
// loop a tab set … this can trap users who are navigating linearly with a screen reader" — and a
// four-tab strip on a phone is exactly where somebody meets it. Stopped here rather than in the
// bundle: the component is vendored and patching it would be a fork to carry forever.
// Written once and given to both strips: the card slot's grows to a dozen file tabs on the desk,
// which is longer than the four this was first written for and wraps the same way.
function unloop(strip, kind) {
  strip.addEventListener('keydown', e => {
    if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return;
    const tabs = [...strip.querySelectorAll(kind)].filter(t => !t.hidden);
    const at = tabs.indexOf(document.activeElement);
    if (at < 0) return;
    const end = e.key === 'ArrowRight' ? tabs.length - 1 : 0;
    if (at === end) { e.preventDefault(); e.stopPropagation(); }
  }, true);
}
unloop(ptabs, 'md-primary-tab');
ptabs.addEventListener('change', () => {
  const was = ['talk', 'facts', 'files', 'plan'].indexOf(panel);
  const now = ['talk', 'facts', 'files', 'plan'][ptabs.activeTabIndex] || 'talk';
  // The component dispatches change for a programmatic activation too, and everything below answers
  // a PRESS: it slides the panel in from a direction, clears the unread count, re-reads the pane.
  // Nothing moved means nobody pressed anything.
  if (now === panel) return;
  panel = now;
  notePanelMove();
  if (panel === 'talk') unread = 0;
  paintUnread();
  drawPanels();
  revealPanel(was);
  // What happened while this panel was the one nobody was looking at. The panes are drawn from
  // frames, and a frame arriving while the workspace sits behind the conversation draws into a box
  // off screen — so arriving at a panel reads once, and the stream carries on from there.
  freshen();
  measureDock();
});
wide.addEventListener('change', drawPanels);
// Dragging a window past this one changes which shape the page is in, not just how it is spaced,
// so it is a re-render rather than a re-layout.

const skillsEl = document.getElementById('skills');
const wikiEl = document.getElementById('wiki');
const boardEl = document.getElementById('board');
const mapEl = document.getElementById('map');
const meetEl = document.getElementById('meet');
const mcpEl = document.getElementById('mcp');
const accessEl = document.getElementById('access');
const screenHead = document.getElementById('screenHead');
// The last fleet answer, so the "which companion" picker names them without a second fetch.
let fleetSeen = [];
// The newest build any listed companion reports, so a row can say it is behind without every row
// re-deriving the answer. Recomputed on each fleet answer; '' until one arrives with versions.
let newestVer = '';
// verCmp orders two "vA.B.C[-extra]" strings by their numeric core. A git-describe suffix
// (-14-gabc) marks a build PAST that tag, but between two builds of one fleet the tags are what
// people ship and compare; the suffix only breaks ties in favour of the described one.
function verCmp(a, b) {
  const parse = v => (v || '').replace(/^v/, '').split('-');
  const [ca, ea] = parse(a), [cb, eb] = parse(b);
  const na = ca.split('.').map(Number), nb = cb.split('.').map(Number);
  for (let i = 0; i < Math.max(na.length, nb.length); i++) {
    const d = (na[i] || 0) - (nb[i] || 0);
    if (d) return d;
  }
  return (ea ? 1 : 0) - (eb ? 1 : 0);
}
const railEl = document.getElementById('rail');
const langEl = document.getElementById('lang');
const prefsK = document.getElementById('prefsK'), consoleK = document.getElementById('consoleK');
const prefsEl = document.getElementById('prefs');
const settingsEl = document.getElementById('settings');
const mcpDialog = document.getElementById('mcpDialog');
const stopDialog = document.getElementById('stopDialog');
const askDialog = document.getElementById('askDialog');
const askK = document.getElementById('askK'), askBody = document.getElementById('askBody');
const askField = document.getElementById('askField');
const askCancel = document.getElementById('askCancel'), askGo = document.getElementById('askGo');
const askPick = document.getElementById('askPick');
const askExtra = document.getElementById('askExtra');
const stopK = document.getElementById('stopK'), stopBody = document.getElementById('stopBody');
const stopCancel = document.getElementById('stopCancel'), stopGo = document.getElementById('stopGo');
const mcpFormEl = document.getElementById('mcpForm');
const mcpDialogK = document.getElementById('mcpDialogK');
const mcpCancel = document.getElementById('mcpCancel'), mcpGo = document.getElementById('mcpGo');
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
  // And it says which state it is in. On four of the seven screens this readout is the dot and
  // nothing else — no text, no title — so for anybody who cannot see it the connection was told in
  // colour alone, which is the one thing this file keeps saying not to do.
  state.setAttribute('aria-label', lost ? tr('state.lost') : tr('state.live'));
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
  // …and on the companion's own page, where a stopped daemon leaves a blank screen, the words go
  // where the conversation was. Measured: 378px of nothing between the tab strip and a composer
  // still offering Send, with the one sentence explaining it clipped in the status line.
  //
  // Through the same door as every other emptying of this box. draw() reuses transcript rows BY
  // POSITION against a cache of what it drew last time, so replaceChildren on its own detaches
  // every node and leaves the cache full of them: the conversation could never come back when the
  // daemon did — the next frame prefix-matched the whole window and appended nothing — and a
  // shorter frame would have called removeChild on a node with no parent and thrown out of draw().
  if (!ok && sock() && !log.querySelector('.empty')) {
    forgetShownRows();
    log.replaceChildren(emptyState('state.companion_gone', 'state.gone_how'));
  }
};
const railBadge = document.getElementById('railBadge');
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
              map: '?v=map', meet: '?v=meet', settings: '?v=settings'};
// In the order they are written in the markup, because md-tabs addresses its tabs by index.
// The board is not among them. It keeps its address and its crumb; what it lost is a permanent
// seat in a navigation that has to fit on a phone, for a screen somebody opens when they have a
// question about the past rather than one they live on.


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

// The third crumb says which level you are standing on, and it is made of words.
//
// Its own function because two callers need it: render(), which knows the level changed, and
// paint(), which knows the language did. Written only by render(), it kept whatever the inlined
// English seed said for as long as the screen stayed open — measured on a Korean browser standing
// in past work: "companions / design / What it has done", with every other label around it in
// Korean. paint() already repaints the first crumb for exactly this reason; the deeper one was
// added later and missed.
// deepWord is what the screen one level in is called.
//
// Its own function because the trail uses it in two places now: as the third crumb when a deep
// screen is all there is, and as the FOURTH when that screen was opened inside a past session —
// where the third belongs to the session it is inside.
function deepWord() {
  return inspOf() === 'tools' ? '🛠 ' + tr('insp.tools')
    : inspOf() === 'loop' ? '↻ ' + tr('insp.loop')
    : askOf() ? '⏸ ' + tr('ask.deciding')
    : crOf() ? '⚖ ' + crOf().split(':').slice(1).join(':')
    : pastOn() ? tr('field.history')
    : '◆ ' + tr('detail.subagent');
}

function paintDeepCrumb() {
  if (!deepIn()) { crumbDeep.textContent = ''; return; }
  // Inside a session, the third crumb is the session: it is the thing the fourth is part of, and
  // the only way back to the transcript somebody was reading.
  const inSession = pastOn() && pastOf() && (crOf() || subOf() || askOf() || inspOf());
  crumbDeep.textContent = inSession ? pastOf() : deepWord();
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

// mid-composition reports whether this Enter belongs to an input method rather than to the page.
//
// Typing Korean (or Japanese, or Chinese) means composing: the letters under the caret are a
// PROVISIONAL syllable and Enter is one of the keys that commits it. A handler that submits on
// Enter without asking took that commit as a send — measured on macOS, the composing syllable was
// sent as its own request, so the last character of every Korean message arrived twice.
//
// Two tests because neither alone covers every browser: `isComposing` is the standard flag, and
// keyCode 229 is what a browser reports for a key the IME swallowed when the flag is absent.
// Checked in the KEYDOWN handler, which is where the race is — compositionend arrives after.
const composing = (e) => !!(e.isComposing || e.keyCode === 229);

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
// A way out at the top of a dialog that takes the whole screen.
//
// On a phone the five full-screen dialogs ARE the screen, and the guide answers "how do I leave"
// with one control in the bar: "앱바의 유일한 내비게이션은 close X 하나여야 한다." They took the
// shape and kept a basic dialog's bottom row, which on a 664px window sits at 584–664 — under the
// on-screen keyboard, in the one variant that exists because a keyboard is about to appear. The
// bottom row stays for a mouse; this is the door a thumb can reach, hidden above the breakpoint.
//
// Called wherever a headline is written, because writing textContent takes the button with it.
// Nothing behind a modal moves.
//
// A wheel over the scrim scrolled the page underneath it — measured 801px on a phone with the
// palette open — and closing the dialog then left the reader somewhere they had never scrolled to.
// The bundle was checked before the page was blamed: `vendor/material.js` carries no scroll lock
// and no `inert`, so Material Web does not do this for anybody. The guide's own table puts a
// dialog at the top of the interruption scale and says it blocks the page until it is answered.
//
// Counted, not toggled: a confirm can open over a form, and the first of the two to close must not
// unlock the page under the second.
let modalsOpen = 0;
function pageMoves(may) {
  const root = document.documentElement;
  if (!may) {
    // What the scrollbar was taking, given back as padding, so the page does not jump sideways by
    // its width on platforms that draw one. Zero where scrollbars are overlaid, which is most of
    // them, and the property simply resolves to 0px there.
    root.style.setProperty('--magi-comp-scrollbar-w', Math.max(0, innerWidth - root.clientWidth) + 'px');
  }
  root.classList.toggle('nomove', !may);
}
// The dialogs' own motion is WAAPI, which no stylesheet reaches.
//
// The reduced-motion block kills every CSS animation on the page, and the test that guards it says
// so in its comment — but md-dialog drives a 500ms translateY(-50px) through element.animate(),
// measured unchanged under `reduce`. The component carries the off switch itself: `quick` skips
// the open and close animations entirely. Kept in step with the setting, both ways.
{
  const still = matchMedia('(prefers-reduced-motion: reduce)');
  // Named one by one — the six are static markup — because the fake DOM's querySelectorAll
  // deliberately refuses tag selectors rather than flattering the page with an empty answer.
  const calm = () => {
    for (const id of ['mcpDialog', 'stopDialog', 'palDialog', 'askDialog', 'fmtDialog']) {
      document.getElementById(id).quick = still.matches;
    }
  };
  calm();
  still.addEventListener('change', calm);
}
addEventListener('opened', e => {
  if (!e.target || e.target.tagName !== 'MD-DIALOG') return;
  if (modalsOpen++ === 0) pageMoves(false);
}, true);
addEventListener('closed', e => {
  if (!e.target || e.target.tagName !== 'MD-DIALOG') return;
  modalsOpen = Math.max(0, modalsOpen - 1);
  if (!modalsOpen) pageMoves(true);
}, true);

function closeX(dlg, head) {
  if (!dlg || !head || !head.querySelector) return;
  // In the CONTENT slot, not the headline. md-dialog names itself from its headline slot —
  // aria-labelledby pointing at the shadow h2 that wraps it — so a control put in that slot folds
  // into the name: measured, five dialogs announced themselves as "Close Preferences", "Close Go
  // to, or do". Writing aria-label on the host does not help; labelledby wins. Slotted into the
  // content and pinned to the corner by the stylesheet (the compact dialogs are the whole screen,
  // so fixed IS the corner), the name is the title again — and the first focusable thing in the
  // dialog is a field rather than the way out.
  let x = dlg.querySelector('.dlgclose');
  if (!x) {
    x = withMark(document.createElement('md-icon-button'), '#i-sl-xmark');
    x.className = 'dlgclose';
    x.setAttribute('slot', 'content');
    x.onclick = () => dlg.close('cancel');
    dlg.append(x);
  }
  x.setAttribute('aria-label', tr('action.close'));
}

// A card's heading in the side column: a mark, the word, and — where there is one — how many.
//
// The five cards had five shapes: two with a mark and three without, one with its count folded
// into the word ("Waiting to run (3)"), one with two counts written into the heading as a
// sentence. They sit in one column, one above the other, so the eye reads them as a list and the
// differences read as meaning. The count is a separate element in the quiet role, which also lets
// the list on a phone count the rows rather than the furniture.
function countedKey(ref, text, n) {
  const k = markedKey(ref, text);
  if (n) k.append(cell('knum', String(n)));
  return k;
}

// head3 marks a Going-on section title as a level-3 heading. They are the only headings on a
// companion's page below its name, and drawn as .k divs the page had none — a screen reader could
// not jump between plan / running / waiting / scheduled / handed-out. Applied at the call sites
// rather than folded into markedKey, because that helper also draws the meet "doing now" status
// line and the team group titles, which are not section headings.
function head3(el) { el.setAttribute('role', 'heading'); el.setAttribute('aria-level', '3'); return el; }

function markedKey(ref, text, cls, markCls) {
  // The word goes in as the element's own text and the mark is PREPENDED, rather than both being
  // appended as nodes. A heading is read by anything that asks for its textContent — the tests do,
  // and so does a screen reader taking the accessible name — and split across two child nodes it
  // reads as empty in the fake DOM and as the icon plus the word everywhere else. One of those is
  // a test that lies.
  const k = cell(cls || 'k', text);
  const m = icon(ref, {cls: 'mk hk' + (markCls ? ' ' + markCls : '')});
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
    // Decorative, like the sprite icon() it stands in for — without this, a no-sprite build read the
    // fallback glyph (a chevron "›", a caret) into the accessible name of the control it sits in.
    s.setAttribute('aria-hidden', 'true');
    return s;
  }});
}

// What each companion's state was the last time the table was drawn, so the next draw can tell
// which rows are news. Keyed by socket rather than by index: the fleet gains and loses rows, and an
// index would report the whole table as changed the moment one of them left.
const wasState = new Map();

// carrying is what a companion has been handed by its neighbours: one piece in hand, and however
// many are queued behind it.
//
// Both halves or neither, the way the terminal says it (fleet.Carrying): one in hand with nothing
// queued means the next request waits a turn, three queued means something else entirely, and one
// number without the other cannot tell those apart. Silent when there is nothing — "0 waiting" on
// every idle companion is a column of noise.
function carrying(a) {
  const parts = [];
  if (a.handling) parts.push(tr('load.in_hand'));
  if (a.waiting > 0) parts.push(tr('load.waiting', {n: a.waiting}));
  return parts.join(', ');
}

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
  // …and what somebody ELSE asked it for. Work handed over runs in a conversation of its own, so
  // the state — which is read from the session a person attaches to — says idle while the
  // companion is busy answering a neighbour. The terminal has said this since handing work over
  // existed (fleet.Carrying); this screen never did, and a row reading "idle" while its companion
  // works is the one reading somebody acts on.
  const load = carrying(a);
  if (load) badge.append(cell('load', load));
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
  // The build, when the daemon says one — and a word when it trails the newest build in this
  // list. Which companion is behind is a fact about the FLEET, and it was only findable by
  // opening every facts card in turn; the list is the screen whose whole job is that comparison.
  if (a.version) {
    const ver = cell('ver', a.version);
    if (newestVer && verCmp(a.version, newestVer) < 0) {
      ver.classList.add('behind');
      ver.append(cell('vhint', tr('ver.behind', {v: newestVer})));
    }
    host.append(document.createElement('br'), ver);
  }
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
// askLine is confirmThis with a field in it: one line of text, or nothing if they change their
// mind. The five places that wanted a name used the browser's own prompt() — a modal this page
// cannot style, cannot translate and cannot put a hint under, and one that some browsers simply
// refuse. It answers through a callback rather than returning, because a dialog is not a function
// call that blocks; that is the whole difference from prompt().
function askLine(q) {
  askK.textContent = q.head;
  closeX(askDialog, askK);
  askBody.textContent = q.body || '';
  askBody.hidden = !q.body;
  askField.setAttribute('label', q.label || q.head);
  // A line, or a paragraph. A commit message written into a one-line box is a commit message
  // nobody writes a body for — the box is the whole invitation.
  if (q.lines > 1) {
    askField.setAttribute('type', 'textarea');
    askField.setAttribute('rows', String(q.lines));
  } else {
    askField.removeAttribute('type');
    askField.removeAttribute('rows');
  }
  askField.value = q.value || '';
  askCancel.textContent = tr('action.cancel');
  withMark(askCancel, '#i-sl-xmark');
  askGo.textContent = q.doIt;
  withMark(askGo, q.doMark || '#i-sl-check');
  // The second half of a question, when it has one: a list to choose from beside the line to type.
  // Rebuilt each time rather than kept, because the choices belong to the question and a stale
  // option list is a control that offers something the caller never meant.
  askPick.replaceChildren();
  askPick.hidden = !q.pick;
  if (q.pick) {
    askPick.setAttribute('label', q.pick.label || '');
    for (const name of (q.pick.options || [])) {
      const o = document.createElement('md-select-option');
      o.value = name;
      if (name === q.pick.value) o.selected = true;
      const t = document.createElement('div');
      t.slot = 'headline';
      t.textContent = name;
      o.append(t);
      askPick.append(o);
    }
    askPick.value = q.pick.value || '';
  }
  // The third action: something the dialog can do TO its own field rather than with it.
  askExtra.hidden = !q.extra;
  if (q.extra) {
    askExtra.textContent = q.extra.label;
    withMark(askExtra, q.extra.mark || '#i-sl-wand-magic-sparkles');
    askExtra.disabled = false;
    askExtra.onclick = async () => {
      askExtra.disabled = true;
      const said = await q.extra.run();
      askExtra.disabled = false;
      // Only if it found something, and never over words somebody has already typed: a draft is
      // an offer, and one that eats a sentence you were in the middle of is not.
      if (said && !String(askField.value || '').trim()) askField.value = said;
      else if (said) askField.value = said;
      if (askField.focus) askField.focus();
    };
  }
  const go = () => {
    const said = (askField.value || '').trim();
    // Read before the dialog closes: a component asked for its value after it has gone is asked
    // about something that is no longer on the screen.
    const chose = q.pick ? (askPick.value || q.pick.value || '') : '';
    askDialog.close('go');
    if (said) q.go(said, chose);
  };
  askCancel.onclick = () => askDialog.close('cancel');
  askGo.onclick = go;
  // Enter is what a person presses in a one-field dialog; without it the field is a box that eats
  // the key that would have finished the job.
  askField.onkeydown = e => { if (e.key === 'Enter' && !composing(e)) { e.preventDefault(); go(); } };
  askDialog.show();
  // Focus after the dialog has opened, or it lands on a box that is not on screen yet.
  requestAnimationFrame(() => { if (askField.focus) askField.focus(); });
}

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
  // Named by its target: five running rows put five "Interrupt" buttons on one screen, and a
  // reader hears the same word five times with nothing to say which agent each halts. Same
  // pattern the permission buttons use.
  const stopName = tr('action.for_companion', {action: tr('action.interrupt'), name: nameOf(a.socket) || a.name || ''});
  stop.setAttribute('aria-label', stopName);
  tip(stop, stopName);
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
    // Soft-disabled, not disabled, and never the one that is on. A hard-disabled chip is
    // pointer-events:none and out of the tab order — so a filter chip left selected after its
    // count fell to zero (leave the destination and come back, the roster has moved on) could not
    // be un-pressed, and the table under it was empty with no way out. soft-disabled dims a zero
    // chip while keeping it announced, and the ACTIVE filter is never dimmed, so it can always be
    // cleared. Both are the bundle's own properties (checked: soft-disabled, always-focusable).
    b.softDisabled = n === 0 && filter !== k;
    b.alwaysFocusable = true;
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
  // Kept, because the language can change without the fleet doing anything: paint() rewrites the
  // four destination names from the pack, and the count has to go back on afterwards.
  markWaiting.n = n;
  for (const b of [railBadge]) {
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
  // Onto the LABEL, because the label is what wins.
  //
  // This used to be written as content — a screen-reader-only span inside the link — with a
  // comment saying an aria-label on the host does not reach the name. That was true when the host
  // was a list item rendering its own anchor in a shadow root; the rail is a plain anchor now, and
  // for a plain anchor aria-label overrides everything inside it. Measured in the tree: the
  // destination read "Companions" with the badge showing, and read "3 Companions, 3 waiting on
  // you" the moment the attribute was removed. So the span was being drowned by the label that was
  // meant to be redundant, and the count — the whole reason for a badge — was never spoken.
  //
  // The name cannot simply come from content instead: collapsed, the rail hides both of its words
  // and a link named by an icon has no name at all.
  for (const host of [railFleet]) {
    host.setAttribute('aria-label', tr('nav.companions') + said);
    host.querySelector('.srcount')?.remove();
  }
}

// srOnly is a phrase for the reader who is listening and not looking. Used where a number is
// drawn as a badge or a bare count: the digit carries its meaning in where it sits, and where it
// sits is exactly what does not survive into a screen reader.
// Whether the reader asked for less movement. The stylesheet answers this for CSS animations and
// transitions, and cannot answer it for a scroll asked for in JavaScript: `behavior:'smooth'` in an
// options bag overrides `scroll-behavior:auto` in a rule. Measured under reduce, both the jump to
// the next waiting companion and the scroll-to-top on re-tapping a destination still glided.
function stillness() {
  return typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches
    ? 'auto' : 'smooth';
}

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
  //
  // And the NAME changes with the word. Callers that name the row give this button an aria-label
  // ("Forget mem-3nol…"), which wins over the label — so armed, the button showed "Confirm?" and
  // still answered to "Forget", and the confirmation step was the one state assistive tech was not
  // told about. The name keeps whatever the caller made of it and gains the state.
  const named = () => btn.getAttribute('aria-label');
  const was = named();
  const say2 = w => { if (was) btn.setAttribute('aria-label', w === word ? was : w + ' — ' + was); };
  label(btn, word);
  const reset = () => {
    armed = false; btn.className = btn.className.replace(' armed', '');
    label(btn, word); say2(word);
  };
  btn.onclick = () => {
    if (armed) { clearTimeout(timer); reset(); act(); return; }
    armed = true;
    btn.className += ' armed';
    label(btn, tr('action.confirm'));
    say2(tr('action.confirm'));
    timer = setTimeout(reset, 5000);
  };
}

// emptyState is the two lines a screen shows when it has nothing: what is absent, and how it stops
// being absent. Both from the pack — these were the last four sentences on the page written in
// English, and they are the ones a first-time reader meets before anything else.
//
// The second line may carry markup (a command in <code>), which is why it is set as HTML. It comes
// from a pack this binary serves and embeds; nothing a companion or the network says reaches here.
// A pane that could not load says so, where the content would have been.
//
// Every one of these loaders answered a refusal by returning early and leaving the pane exactly as
// it was — which on first load is EMPTY. Measured with the server answering 500: the board, the
// people screen and the map each drew a completely blank window between the app bar and the
// navigation bar, with the only trace of the failure four characters wide in the status line. A
// blank pane and a pane still loading are the same picture, and one of them is a lie.
// reading paints a wait cue where a list will go, while its first load is in flight — the same
// indeterminate bar the workspace panes use (waitingFor), generalized to the six destinations that
// had nothing: measured, a slow or hung /fleet /board /skills /mcp /access /map left a blank window
// for the whole wait, indistinguishable from an empty one. Only when the pane is EMPTY (first load,
// or a slow link): a poll that follows a good load has content and this does nothing, and the draw's
// replaceChildren clears the cue. paneFailed treats a lone cue as "nothing to keep" so a refusal
// still replaces it. Named for the content it is loading, per the guide's progress-label rule.
function reading(box, key) {
  if (box.children.length) return;
  box.replaceChildren(waitingFor(key));
}
// paneLoadingOnly reports whether the pane holds nothing but a loading cue — a placeholder to be
// replaced, not content to keep.
function paneLoadingOnly(box) {
  return box.children.length === 1 && /\bpaneloading\b/.test(box.children[0].className || '');
}

function paneFailed(box, headKey) {
  // Only where there is nothing to keep. A refusal on the FIRST load leaves a blank window and has
  // to say so; a refusal on the poll that follows a good one must leave what is on screen alone —
  // stale and said-so beats blank, which is the rule the fleet's own test has held since before
  // this function existed. The status line carries the refusal either way. A lone loading cue is a
  // placeholder, not content, so it does not count as "something to keep".
  if (box.children.length && !paneLoadingOnly(box)) return;
  // With its heading, if it has one. Replacing the pane's children takes the section head with
  // them, so a reader navigating by headings arrives at a pane with no name while the strip above
  // still says which one it is.
  box.replaceChildren(...(headKey ? [sectionHead(headKey)] : []),
                      emptyState('error.pane', 'error.pane_how'));
}

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
      // …and only one this console can answer. A companion waiting on somebody at ANOTHER console
      // is drawn "waiting" and carries no question and no buttons, and the queue was centring it —
      // sending the reader to a card with nothing on it while skipping the answerable one.
      if (!row.querySelector('.answer')) continue;
      if (row.scrollIntoView) row.scrollIntoView({block: 'center', behavior: stillness()});
      return;
    }
  });
}

function jumpToFirstRow() {
  requestAnimationFrame(() => {
    const first = fleetEl.querySelector('.card');
    if (first && first.scrollIntoView) first.scrollIntoView({block: 'start', behavior: stillness()});
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
  // Both, and then the focus by hand. stopPropagation alone was not enough and the reason it was
  // written that way is a mistake about which event does what: a link's navigation is its DEFAULT
  // ACTION, which runs off the event path and does not care that no ancestor listener fired — so
  // one tap on this box left the page. preventDefault is what cancels a navigation, and it is
  // mousedown, not click, whose default action gives a field its focus; cancelling the click
  // therefore costs nothing. Measured on a phone: the tap used to land on /?d=…, activeElement
  // <body>, no keyboard. focus() is kept anyway so the caret is certain rather than inferred.
  i.onclick = e => { e.preventDefault(); e.stopPropagation(); i.focus(); };
  i.onkeydown = e => { if (e.key === 'Enter' && !composing(e)) go(e); };
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
      // Outlined, not tonal. The tonal container this page gives them is a surface role one step
      // off the page — measured 1.24:1 dark and 1.18:1 in the light theme, where the guide asks a
      // filled or tonal button for 3:1 from its CONTAINER because four of them in a row have to be
      // told from the page and from each other. No surface role in this palette reaches 3:1
      // against the background; the outline role does (3.41 dark, 3.75 light), and an outlined
      // button is measured from its outline. The four stay one weight as a group, which is the
      // point — a console that emphasised one of them would be answering for the person.
      const b = label(withMark(document.createElement('md-outlined-button'), mark), tr(key));
      // Named for the companion it answers for. Two companions blocked at once put ten controls on
      // the fleet screen with five names between them — "Allow", "Deny", "Allow", "Deny" — and the
      // card that says whose they are is a link above them, which a reader tabbing control to
      // control never hears. These are the highest-stakes presses on the page.
      if (a.name) b.setAttribute('aria-label', tr('action.for_companion', {action: tr(key), name: a.name}));
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
  // The mode-dependent chrome — the field's floating label, the send button's word AND its mark (a
  // paper plane for putting something into the conversation, a reply arrow for answering the question
  // above it) — changes ONLY when the mode flips. drawPrompt calls this every fleet frame, and
  // label()/withMark() tear down and rebuild the button's text node and slotted icon each time, so
  // an idle companion churned ~80 send-button mutations a minute for a word that never changed. Gated
  // on the same flip the parked-draft swap uses.
  const flip = !!a !== wasAnswering;
  // Re-render the mode chrome on a mode flip OR when the language pack version moved (a pack that
  // arrives after the first paint must reach these labels). labelVer starts !== the unset sentinel,
  // so this also covers the very first render, where there is no flip.
  if (flip || answerMode.lver !== labelVer) {
    t.setAttribute('label', tr(a ? 'label.answer' : 'label.ask'));
    const send = document.getElementById('send');
    label(send, tr(a ? 'action.answer' : 'action.send'));
    withMark(send, a ? '#i-sl-reply' : '#i-ss-paper-plane');
    answerMode.lver = labelVer;
  }
  if (flip) {
    // The old text is PARKED, not deleted. It used to be cleared on the flip — but the flip arrives
    // on a background frame, and a frame that lands mid-sentence was erasing 39 typed characters
    // with no undo, which is the thing the guide will not even let the Escape key do. The words come
    // back when the mode does: an answer being typed when the question is withdrawn is kept for the
    // next question, a request being typed when a question arrives is back the moment it is dealt
    // with. Each mode owns its own draft, so neither is ever offered as the other.
    const parked = answerMode.parked || '';
    answerMode.parked = t.value;
    t.value = parked;
    // The box content just changed under any standing suggestion, without an input event to clear it.
    // Left up, the hint is about the parked draft and Tab would append it to the swapped-in text.
    if (typeof sugClear === 'function') sugClear();
  }
  // The note's text can change WITHOUT a flip (a question with options replacing one without), so it
  // is not gated on the flip — but it is only written when it actually changed, so an unchanged one
  // costs nothing.
  const note = document.getElementById('cnote');
  const noteText = a ? tr((a.askOptions || []).length ? 'answer.or_pick' : 'answer.instead') : '';
  if (note.textContent !== noteText) note.textContent = noteText;
  note.hidden = !a;
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
  reading(boardEl, 'loading.board');
  const list = await fetchList('/fleet');
  // No heading key: the board is the one destination whose name is already in the app bar's own
  // h2 (it draws team names, not a section head), so a failure that added its own "Board" put the
  // same word in the heading tree twice.
  if (!list) return void paneFailed(boardEl);
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
  // A wheel with no horizontal axis still reaches the lanes past the edge. A plain mouse — the
  // commonest pointer on a wide screen — could not move this strip at all: the page behind it has
  // nothing to scroll, so the wheel did nothing and four of nine teams stayed invisible with no
  // scrollbar to say they were there.
  lanes.addEventListener('wheel', e => {
    if (e.deltaX || !e.deltaY) return;                 // a real horizontal wheel: leave it alone
    if (lanes.scrollWidth <= lanes.clientWidth) return; // nothing to reach
    lanes.scrollLeft += e.deltaY;
    e.preventDefault();
  }, {passive: false});
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
    // h3: a lane is a section of the board's own h2, not a peer of it — the same rule teamHead uses.
    const title = document.createElement('h3');
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
    // What was refused and with what code — in this console's own words.
    //
    // The server's response BODY used to be the message, cut to 80 characters. Behind a proxy that
    // is an HTML error page, so the status line read "<!doctype html><html><head><title>502 Bad
    // Gateway" clipped at a quarter of its width, which says nothing and pushes the crumb and the
    // instance name to nothing beside it. The body is still worth having, so it goes to the
    // console where a whole line fits.
    const why = (await r.text().catch(() => '')).trim();
    if (why) console.warn('magi-web', r.status, path, why);
    says(tr('error.refused', {code: r.status, what: path.split('?')[0]}));
    return null;
  }
  // A 200 with a body that will not parse is NOT "cannot reach magi-web" — the console reached it
  // and it answered; the answer was garbled (a truncated stream, a proxy that rewrote the body). The
  // connection is real, so the dot does not go red for it, and the message says what actually
  // happened. The body goes to the console, where the whole of it fits, like the refusal path above.
  try { return await r.json(); }
  catch (e) {
    console.warn('magi-web', 'garbled', path, e);
    says(tr('error.garbled'));
    return null;
  }
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

// loadFleet fetches the roster and draws everything read off it. The stream hands the list in
// instead, so a push costs no request — see watchFleet.
async function loadFleet(given) {
  // A roster, or nothing. Six callers reach this as `post(…).then(loadFleet)`, and post resolves
  // the REFUSAL TEXT when the daemon says no — so a refused answer or interrupt arrived here as a
  // truthy string and `list.filter` threw. The screen then kept the question, kept all four
  // buttons live, and said only the first 80 characters of why in the status line. Anything that
  // is not a list means the same thing an absent argument means: go and ask.
  if (!Array.isArray(given)) reading(fleetEl, 'loading.roster');
  const list = Array.isArray(given) ? given : await fetchList('/fleet');
  // The companions screen is a pane like the others. Every loader beside it was given this and it
  // was not: measured with the roster refused, 105px of nothing between the app bar and the
  // navigation bar, and the only trace a status line clipped to "magi-web answered …".
  if (!list) return void paneFailed(fleetEl);
  reach(true);
  const waiting = list.filter(a => a.state === 'waiting').length;
  retitle(waiting);

  // On an agent's page the fleet is polled for this one entry: the prompt it is blocked on and the
  // facts in its header reach the browser no other way.
  fleetSeen = list;
  newestVer = list.reduce((m, a) => (a.version && verCmp(a.version, m) > 0 ? a.version : m), '');
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
    loadJobs(mine);
    // The workspace beside the conversation. Redrawn on the poll like everything else on this
    // page: a file appearing in a directory while somebody watches is the thing a tree is for.
    lastDrawnFor = mine;
    // The tree, unless somebody is looking at search results — a redraw under a reader would take
    // their results away mid-scroll.
    //
    // Read for itself when this came from a frame, which is the companion saying something
    // happened; drawn from what is kept when it came from a read this page asked for, which is
    // the same question again (arriving at the panel, coming back to the tab).
    if (!findQ.trim()) loadTree(mine, !given);
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
  // wasState is keyed by socket alone (it drives the arrival chime, which is per companion, not per
  // peer view), so it is pruned against the live sockets rather than the peer+socket keys above —
  // otherwise it grew one entry per distinct socket ever seen, for the life of the tab.
  const aliveSockets = new Set(list.map(a => a.socket));
  for (const s of [...wasState.keys()]) if (!aliveSockets.has(s)) wasState.delete(s);
  // A filter that now matches nothing says so and offers the way out. A roster of six narrowed to
  // zero used to draw a bare table head under a lit chip, with no word about why and — since the
  // chip had gone hard-disabled — no way to clear it. This is the shape the access screen already
  // uses for its capability filter: name what is being shown, and a control to show everything.
  if (filter && !rows.length && !here) {
    const note = cell('capnote');
    note.append(cell('', tr('filter.only', {state: stateWord(filter)})));
    const all = document.createElement('md-text-button');
    label(all, tr('action.show_all'));
    all.onclick = () => { filter = null; render(); };
    note.append(all);
    fleetEl.replaceChildren(tableHead(), note);
    return;
  }
  // The ways to the other views of this destination sit in the summary row with the board's, where
  // they are one row of matching controls rather than three shapes in three places. See summarise.
  fleetEl.replaceChildren(...(here ? [] : [tableHead()]), ...grouped(rows));
}

// The masthead's readout for the list's own screen: how many there are, and a way to reach whoever
// is blocked.
function drawFleetCount(list, waiting) {
  // The count says somebody is blocked; pressing it goes there. It said so and did nothing before,
  // which is the readout every console has and the reason nobody presses it.
  const said = tr(list.length === 1 ? 'count.agent' : 'count.agents', {n: list.length}) +
    (waiting ? ' · ' : '');
  // Rebuilt only when it says something different. This is a polite live region and the fleet is
  // redrawn on every poll and on every destination change, so an unconditional replaceChildren
  // announced "3 agents" again for a page that had not changed — measured twice per tap.
  // Compared including the part that is a button, because that part is what changes while somebody
  // is waiting. Written as `textContent === said && !waiting` the guard switched itself off in
  // exactly the state it was needed: measured with an observer, twelve rebuilds of a polite live
  // region in ten seconds, all carrying the same sentence.
  const whole = said + (waiting ? tr('state.waiting_on_you', {n: waiting}) : '');
  // The memo is about the LIVE REGION, and only about that. Written as an early return it also
  // skipped summarise() at the foot of this function — the one thing that rebuilds the four state
  // tiles and the ways to the other views — so while somebody was blocked (the case the old guard
  // deliberately excluded) a companion moving between working and idle stopped changing the counts,
  // and a pressed filter chip left the previous one lit.
  const same = drawFleetCount.said === whole;
  drawFleetCount.said = whole;
  // The sentence in an element of its own, because it is the part that may be given up.
  //
  // As a bare text node it was an anonymous flex item, which text-overflow does not reach: the
  // readout clipped mid-glyph ("3 agent" and a sliced s) instead of ellipsing, and the shrinking
  // had to be done by the row's only other lever — squeezing the button beside it until its label
  // was one letter and, at 2x text, until it laid out on top of the icon buttons and could not be
  // pressed at all. Named, it can be the one that gives way.
  if (!same) state.replaceChildren(cell('scount', said));
  if (waiting && !same) {
    const go = document.createElement('md-text-button');
    go.className = 'jump';
    // A full-sentence label ("3 waiting on you") overflowed the icon buttons on a narrow bar —
    // measured 25px onto the palette icon at 390. Two spans: the sentence where there is room, a
    // compact "3 ⏸" where there is not (the stylesheet swaps them at compact), and the full
    // sentence stays the accessible name either way.
    go.append(cell('jfull', tr('state.waiting_on_you', {n: waiting})),
              cell('jshort', tr('state.waiting_short', {n: waiting})));
    go.setAttribute('aria-label', tr('state.waiting_on_you', {n: waiting}));
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
  return [labelVer, a.state, a.name, a.role, a.team, a.hub, a.workdir, a.session, a.steps, a.idle,
          a.task, a.doing, a.asking, a.askId, a.askKind, a.planDone, a.planTotal,
          a.host, a.addr, a.pid, a.peer, a.live, a.permission, a.user, a.version, newestVer,
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
  // h3: the screen's own h2 ("Companions") is above these, and a team is a section of that list,
  // not a peer of it.
  const h = document.createElement('h3');
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
function setFolded(want, chosen) {
  const box = document.getElementById('detail');
  box.toggleAttribute('folded', want);
  const bar = box.querySelector('.foldbar');
  if (bar) bar.setAttribute('aria-expanded', want ? 'false' : 'true');
  // The other card in the same slot folds with it. They take turns in one box — the facts and
  // whatever file is open — so "how much of this screen do I want the conversation to have" is one
  // question, not two, and answering it on one card and not the other leaves the slot jumping
  // between two heights as you switch tabs.
  fileViewEl.toggleAttribute('folded', want);
  const fbar = fileViewEl.querySelector('.foldcaret');
  if (fbar) fbar.setAttribute('aria-expanded', want ? 'false' : 'true');
  // Only a press is a preference. The default below is decided by the window, and writing that
  // down would turn "this window is narrow today" into "this reader wants it folded".
  if (chosen) localStorage.setItem('facts', want ? 'folded' : 'open');
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

// updateControl is the companion's BUILD row on the facts card: the version its daemon reports,
// and — only when that build trails the newest one this console has seen in the fleet — an update
// button under it. Pressing it tells a same-machine daemon to update itself to the latest release
// and restart onto it (/update → the daemon's own update, which downloads, commits with rollback,
// and re-execs); the daemon's one-line account then takes the button's own place. Disabled without
// the configure capability, like the permission control above it. Same-machine only is enforced by
// the caller (a.trust==='own') and by /update itself.
//
// Build and update are one cell rather than two: the version is the reason to update and the button
// is what you do about it, and a card that put the number at the top and the action at the bottom
// asked the reader to hold one against the other across the whole grid. Behind-only, because a
// button offered on a companion already current is a button whose only outcome is "nothing to do".
function updateControl(a) {
  const f = cell('f');
  f.append(cell('k', tr('field.version')));
  const v = cell('v');
  v.append(cell('vnum', a.version));
  const btn = withMark(document.createElement('md-text-button'), '#i-sl-cloud-arrow-down');
  label(btn, tr('action.update'));
  const behind = newestVer && verCmp(a.version, newestVer) < 0;
  // The outcome lives at MODULE scope, per socket, and the row is drawn FROM it — not from this
  // build's local variables. The facts grid keeps a row whose words did not change (rowWords) and
  // replaces one whose words did, so state that only lived in a closure was destroyed by the poll:
  // a rebuild mid-download swapped in a disabled button the finished fetch could never re-enable
  // (its nodes were detached), and the daemon's account of a SUCCESS was written to a dead node or
  // wiped by the next frame. Drawn from shared state, every transition changes the row's words and
  // the reconciler replaces it correctly; the same pattern commitRules uses across card rebuilds.
  const states = updateControl.state || (updateControl.state = new Map());
  const st = states.get(a.socket);
  const say = cell('updsay');
  // The button and the account take turns in the same slot under the version: a line the daemon
  // sent (working, done, or refused) replaces the button, which is what "the message shows where
  // the button was" means. With no line, the button shows only when there is a newer build to move
  // to. Disabled-not-hidden without configure so a viewer who cannot press it still sees one is out.
  if (st && st.text) {
    // A line the daemon SENT answers "is there anything to do here", so it takes the button's
    // place. A transport failure sent nothing — its line is magi's own "try again", and the thing
    // to try was the button that sentence had just removed. So that one keeps the button beside
    // it: an instruction whose control is gone is a control that does nothing, drawn as words.
    btn.hidden = !(st.retry && behind);
    say.hidden = false;
    say.textContent = st.text;
  } else {
    btn.hidden = !behind;
    say.hidden = true;
  }
  btn.disabled = !may('configure') || !!(st && st.working);
  btn.onclick = async () => {
    if (states.get(a.socket) && states.get(a.socket).working) return;
    states.set(a.socket, {working: true, text: tr('update.working')});
    btn.hidden = true;
    say.hidden = false;
    say.textContent = tr('update.working');
    // The body is shown WHATEVER the status: a refusal here carries its own instruction ("run it on
    // the machine that companion is on", "do it from a terminal") and collapsing it to a generic
    // "try again" pointed people at a healthy daemon. Only a transport failure — no reply at all,
    // e.g. the socket already gone mid-restart — falls back to the generic line. The abort signal
    // guarantees the promise settles even against a wedged daemon, so the working flag cannot leak
    // and pin the button disabled forever.
    let out = '';
    try {
      const r = await fetch('/update' + qFor(a), {method: 'POST', signal: AbortSignal.timeout(15 * 60 * 1000)});
      out = (await r.text()).trim();
    } catch { /* transport failure; the generic line below */ }
    states.set(a.socket, {working: false, text: out || tr('update.failed'), retry: !out});
    // These nodes may be detached by now (the poll rebuilds the card); writing them is free and
    // right when they are still live, and the state above repaints the rebuilt row either way. The
    // account stays in the button's place — the button does not come back under the same version,
    // because the outcome IS the answer to "is there anything to do here" until the next visit
    // clears it (a success republishes as a newer build, a refusal said why). The one exception is
    // the transport failure, whose line says "try again": the control it means comes back HERE, at
    // the moment the outcome is known, and not a poll later — a sentence pointing at a button that
    // is not there yet is the same dead instruction whether it lasts a second or a minute.
    say.textContent = out || tr('update.failed');
    btn.hidden = !(!out && behind);
    // The console panel's own build line ("this console / companions") answers "why hasn't the thing
    // I shipped shown up", and it was loaded once at startup. Refreshed AFTER the restart settles,
    // not at the reply: the reply lands before the daemon drains, so an instant refresh read the OLD
    // record — or, mid-drain, no live daemon at all — and the panel contradicted the reply just
    // shown. One early refresh for the fast case, one later for a slow republish.
    setTimeout(loadConsole, 4000);
    setTimeout(loadConsole, 12000);
  };
  v.append(btn, say);
  f.append(v);
  return f;
}

function permField(a) {
  // Changing how a companion runs is `configure`; reading which mode it is on is not. So the field
  // is drawn and disabled rather than removed: a viewer who cannot see the approval mode cannot
  // tell a companion that stops for everything from one that stops for nothing.

  const f = cell('f');
  f.append(cell('k', tr('field.permission')));
  const v = cell('v');
  // The companion this field speaks for, kept live. The element is cached across redraws so an
  // open menu is not thrown away by a poll — but the change listener is installed once and closed
  // over whichever `a` was first, so a mode changed on companion B was POSTed to companion A
  // (measured), and B's next poll reverted the readout so it looked like the click did nothing.
  // The listener reads permField.a, which every draw updates to the companion on screen.
  permField.a = a;
  let sel = permField.el;
  if (sel) sel.toggleAttribute('disabled', !may('configure'));
  if (!sel) {
    sel = permField.el = document.createElement('md-outlined-select');
    // Named for a reader who cannot see the word beside it. The field's own heading is a div next
    // to the control, not a label bound to it, so without this the control announces as an
    // unnamed combobox — measured on the facts card, three of them.
    sel.setAttribute('aria-label', tr('field.permission'));
    sel.className = 'permsel';
    paintPerm(sel);
    sel.addEventListener('change', async () => {
      const want = sel.value;
      const to = permField.a || a;
      // Said by the daemon, not assumed by the page: the next poll paints whatever it reports, so
      // a refused change reverts visibly instead of leaving the console claiming a mode nobody is
      // on. Kept as the pending value until then so the poll in between does not fight the click.
      permField.want = want;
      const why = await post('/permission', new URLSearchParams({mode: want}), to.socket, to.peer);
      permField.want = '';
      if (!why) loadFleet();
    });
  }
  // The name is re-set on every draw, not only at creation: written once, it froze in whatever
  // language was loaded when the element was first made — measured as "Approvals" among Korean
  // fields after a pack landed later.
  sel.setAttribute('aria-label', tr('field.permission'));
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
  f.append(cell('k', tr('field.provider_model')));
  const v = cell('v');
  const key = (a.peer || '') + '\0' + a.socket;
  let sel = modelField.el;
  if (!sel || modelField.key !== key) {
    sel = modelField.el = document.createElement('md-outlined-select');
    sel.setAttribute('aria-label', tr('field.model'));
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
      // The list arriving is the moment the count becomes real; disable from the same rule the
      // synchronous path uses, or a menu of one stays pressable until the next poll.
      const at = modelField.now || now;
      const n = modelField.list.filter(m => m).length + (at && !modelField.list.includes(at) ? 1 : 0);
      sel.disabled = !may('configure') || n < 2;
    });
  }
  modelField.now = modelField.want || now;
  const models = modelField.list || [];
  paintModels(sel, models, modelField.now);
  // One writer for two reasons. paintModels shut the select when there was nothing to change to
  // (a real /model answered `null`, so a menu of one); this line then re-opened it for anybody
  // who may configure — toggleAttribute(false) REMOVES the attribute. Disabled if EITHER holds:
  // you cannot change it, or there is nothing to change it to.
  const optionCount = models.filter(n => n).length + (modelField.now && !models.includes(modelField.now) ? 1 : 0);
  sel.disabled = !may('configure') || optionCount < 2;
  // The provider, beside the model and before it — the pair reads left to right the way the
  // question does ("which backend, then which of its models"), and they are one row because
  // changing the first changes what the second may offer.
  //
  // Only drawn when something IS serving: with no CLI backend up, the model select alone is the
  // whole truth and an empty provider menu would be a control with nothing behind it.
  const pick = providerField.el || (providerField.el = document.createElement('md-outlined-select'));
  if (providerField.key !== key) {
    providerField.key = key;
    providerField.list = null;
    pick.className = 'permsel';
    pick.setAttribute('aria-label', tr('field.provider'));
    pick.addEventListener('change', async () => {
      const p = (providerField.list || []).find(x => x.name === pick.value);
      if (!p) return;
      // The address goes on the wire, not the name: the console knows where each shim answers and
      // the daemon should not have to look it up a second time (or disagree about the answer).
      const why = await post('/providers', new URLSearchParams({base: p.base}), a.socket, a.peer);
      if (!why) {
        // The model list belongs to the backend, so it is stale the moment the backend changes.
        modelField.list = null;
        loadFleet();
      }
    });
  }
  if (providerField.list === null) {
    providerField.list = [];
    fetchList('/providers').then(list => {
      providerField.list = list || [];
      paintProviders(pick, providerField.list);
      pick.hidden = providerField.list.length < 1;
      pick.disabled = !may('configure') || providerField.list.length < 2;
    });
  }
  paintProviders(pick, providerField.list || []);
  pick.hidden = (providerField.list || []).length < 1;
  pick.disabled = !may('configure') || (providerField.list || []).length < 2;
  const pair = cell('modelpair');
  pair.append(pick, sel);
  v.append(pair);
  f.append(v);
  return f;
}

// providerField holds the provider select across redraws, like modelField beside it: the card is
// rebuilt on every poll and a select rebuilt under an open menu closes it.
const providerField = {el: null, key: '', list: null};

// paintProviders fills the provider select with the backends that are serving. No "current" entry:
// which one a companion is ON is not something the console can read back — the daemon holds the
// base URL and does not report it — so the menu offers what exists and says nothing it cannot know.
function paintProviders(sel, list) {
  const names = list.map(p => p.name);
  if ((sel._painted || []).join('\0') === names.join('\0')) return;
  sel._painted = names;
  sel.replaceChildren();
  for (const n of names) {
    const o = document.createElement('md-select-option');
    o.value = n;
    const h = document.createElement('div');
    h.slot = 'headline';
    h.textContent = n;
    o.append(h);
    sel.append(o);
  }
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
  // Whether to disable is the CALLER's call: it also knows whether this reader may configure at
  // all, and two functions writing sel.disabled with different reasons left the state flapping.
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
    sel.setAttribute('aria-label', tr('field.session'));
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

// rowWords is everything a row says, gathered the same way in a browser and in the fake DOM the
// page's tests run against.
//
// textContent alone would have done in a browser, where it already includes the descendants — and
// it silently made the comparison above always say "nothing changed" under the tests, where a
// node's textContent is its OWN text and the words live in its children.
function rowWords(n) {
  if (!n) return '';
  return (n.textContent || '') + [...(n.children || [])].map(rowWords).join('');
}

function drawDetail(a) {
  const box = document.getElementById('detail');
  if (!a) { box.hidden = true; box.replaceChildren(); return; }
  // Takes a KEY, not a word. Every label in this panel was written in English here while the pack
  // carried a translation for it, and the panel is the one screen that answers "what am I looking
  // at" — the last place that should be answering it in a language the reader did not pick.
  const field = (key, v, cls) => {
    const f = cell('f');
    // Named, so a redraw can find the same field again and write the new value into it rather than
    // replacing the grid it lives in. See the reconciliation below.
    f.dataset.k = key;
    f.append(cell('k', tr(key)), cell('v ' + (cls || ''), v));
    return f;
  };
  // The grid outlives the poll. It is assembled by several hands — this function, the context
  // fetch that lands later, the children fetch after that — so it cannot be built fresh and
  // swapped in: whichever hand finished first would be drawing into a box nobody is looking at.
  // Instead the rows are put INTO it by key, and a row whose words have not changed is left alone.
  // Which is what stops the two selects being re-parented every three seconds — and a re-parented
  // select is one whose open menu shuts under the pointer.
  const wrapNow = drawDetail.wrap;
  const reuse = drawDetail.grid && drawDetail.for === a.socket &&
                wrapNow && wrapNow.contains(drawDetail.grid);
  const grid = reuse ? drawDetail.grid : cell('grid');
  drawDetail.grid = grid;
  drawDetail.for = a.socket;
  const seen = new Set();
  // put is the whole of the reconciliation: same key and same words, leave it; different words,
  // replace that row; unknown key, append. The key is the row's own label when nothing named it.
  const put = row => {
    if (!row) return;
    const label = row.querySelector && row.querySelector('.k');
    const k = row.dataset && (row.dataset.k || (label ? 'k:' + label.textContent : ''));
    if (!k) { grid.append(row); return; }
    row.dataset.k = k;
    seen.add(k);
    if (drawDetail.late) row.dataset.late = '1';
    const had = [...grid.children].find(c => c.dataset && c.dataset.k === k);
    if (!had) { grid.append(row); return; }
    if (rowWords(had) === rowWords(row)) return;
    had.replaceWith(row);
  };
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
  // A same-machine companion that reports a build gets its version folded into the update cell
  // (updateControl, appended below) — build and the action on it in one place. Every other companion
  // that reports a build shows it read-only here: a remote or peer-sighted one cannot be updated from
  // this console (updating restarts a daemon over its own local socket; /update refuses across a
  // dial), so its version is a fact to read, not a control.
  // !a.peer as well as trust: a federated console's /fleet rows arrive with the PEER's own trust
  // stamped on them ("own", from its point of view), so trust alone would draw the update button on
  // another machine's companion. a.peer is the fact about THIS console: set exactly when the row
  // came from a peer rather than a local dial.
  const ownBuild = a.trust === 'own' && !a.elsewhere && !a.peer && a.version;
  [
    field('field.status', stateWord(a.state) + (carrying(a) ? ' · ' + carrying(a) : ''),
          'state ' + a.state),
    field('field.steps', a.steps ? a.steps + '' : '—'),
    field('field.last_activity', ago(a.idle)),
    ...(a.role ? [wide(field('field.role', a.role))] : []),
    ...(a.team ? [field('field.team', a.team + (a.hub ? ' · ' + tr('team.speaks') : ''))] : []),
    field('field.host', (a.instance || a.host || tr('map.here')) + (a.addr ? ' · ' + a.addr : '') +
                  (a.pid ? ' · pid ' + a.pid : '')),
    ...(a.version && !ownBuild ? [field('field.version', a.version)] : []),
    wide(field('field.workspace', a.workdir)),
    sessionField(a),
  ].forEach(put);
  put(permField(a));
  if (ownBuild) {
    put(updateControl(a));
  }
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
  bar.onclick = () => setFolded(!box.hasAttribute('folded'), true);
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
  // Only when a field says something different. This runs on every fleet poll, and the grid holds
  // the two selects — so a rebuild re-parents them, and re-parenting a select closes the menu
  // somebody has open. Measured with a marker on each part: after a message arrived, everything on
  // the page survived except this grid, three seconds at a time.
  //
  // On the words rather than on the row payload: the payload carries an idle counter that ticks
  // every second, and comparing it would mean rebuilding once a second for a number the grid does
  // not show to that precision.
  box.replaceChildren(bar, wrap);
  // What it can do and how the run is shaped — the two things the terminal answers with /tools and
  // /loop, which had no way in here at all. Buttons in the facts card rather than rows in the
  // transcript: they are answers to a question somebody asked, not a record of what happened, and
  // the transcript is already the one place where those two kinds of thing get mixed.
  {
    const row = cell('f');
    row.dataset.k = 'field.what_it_has';
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
    put(row);
  }
  // Anything that was here last time and is not here now has stopped being true — a role that was
  // removed, a team it left. The rows that arrive later are exempt, or they would be swept away
  // between the poll that builds the grid and the fetch that fills them in.
  for (const row of [...grid.children]) {
    const k = row.dataset && row.dataset.k;
    if (k && !seen.has(k) && row.dataset.late !== '1') row.remove();
  }
  if (!reuse) wrap.replaceChildren(grid);
  // The children a turn spawned are NOT listed here any more.
  //
  // This existed because the transcript could not answer for them: a child starts inside a tool
  // call and finishes inside the same one, so the row that produced it said "spawn" and nothing
  // about what came back. That hole is closed — the child's own account now lands on the tool
  // call's result, verbatim, with its session id (see spawn.go's childAccount) — so the answer is
  // in the conversation, in the order the work happened, which is where somebody reading the work
  // is already looking.
  //
  // A second list beside it would be the same children in a place with no surrounding context, and
  // after an hour of meetings it was mostly other companions' turns. The way into a child's own
  // transcript is the id in the result; drawChild still answers ?sub=<id>.
  drawDetail.late = true;
  drawDetail.late = false;
  // Folded to start with, unless the window is wide enough to hold both.
  //
  // The card sits above the transcript in the middle column, and with every field open it is
  // ~810px: measured at 840×768 and at 900×600 the conversation began at y=905 — a companion's
  // page with no conversation on it, on first arrival, with nothing saying to scroll. Above 1200
  // there is room for both, which is where it stays open by default. A reader who folds or opens
  // it is remembered either way; this is only what happens before anybody has said.
  const said = localStorage.getItem('facts');
  setFolded(said === null ? (globalThis.innerWidth || 0) < 1200 : said === 'folded');
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
  drawDetail.late = true;
  return drawContext(a, box, grid, field, put).finally(() => { drawDetail.late = false; });
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
    // The empty-state placeholder this function manages does not count as content — counting it
    // would make it hide itself and then reappear, a flip every call.
    if (!c.hidden && !(c.classList && c.classList.contains('sideempty'))) any = true;
  }
  sideToggle.disabled = !any;
  // A desk pane that is open and empty says so, instead of being a blank column. On a phone the
  // "Going on" tab draws going_on.none for exactly this; the desk side column had nothing. Kept as
  // one managed child so it comes and goes with the cards rather than stacking.
  let blank = refreshSideToggle.blank;
  if (!any) {
    if (!blank) {
      blank = refreshSideToggle.blank = emptyState('going_on.none', 'going_on.none_how');
      blank.classList.add('sideempty');
      sideEl.append(blank);
    }
    blank.hidden = false;
  } else if (blank) {
    blank.remove();
    refreshSideToggle.blank = null;
  }
  // What it would do, or why it will not. Said on the tooltip and to a screen reader, because a
  // greyed-out control with no explanation is the least useful state a control can be in.
  const word = !any ? 'side.nothing' : (document.body.getAttribute('side') === 'shut' ? 'side.show' : 'side.hide');
  sideToggle.setAttribute('aria-label', tr(word));
  tip(sideToggle, tr(word));
}

// A side card is redrawn only when what it draws changed.
//
// drawPlan/drawHandoffs/drawCron/loadJobs each fetch and then replaceChildren every fleet frame —
// and a frame is pushed to every open companion page whenever ANY companion in the fleet takes a
// step, so a card that changes rarely was rebuilding its whole subtree several times a minute off a
// busy neighbour. The signature is the fetched data plus the companion and the label version, so a
// real change (and a late language pack) still redraws. Cleared per card in clearCompanionView so a
// return to the same companion, whose box was emptied, draws again.
function sideCardSame(box, a, list) {
  const sig = (a.peer || '') + '\0' + (a.socket || '') + '\0' + labelVer + '\0' + JSON.stringify(list || []);
  if (box._sideSig === sig) return true;
  box._sideSig = sig;
  return false;
}

// A side card's data is refetched only when the companion it belongs to has ADVANCED — not on every
// fleet frame. sideCardSame gates the DOM rebuild, but the fetch and its JSON.parse ran before it,
// unconditionally: a frame is pushed to an open page whenever ANY companion steps, so the open one's
// /plan /handoffs /cron /jobs were re-fetched several times a minute off a busy neighbour even though
// this companion's data could not have changed. Keyed like the context panel (contextKey: identity +
// steps + state), so a real advance re-fetches and a neighbour's frame is a memory hit. labelVer is
// in the key so a late language pack still redraws. Keyed by CARD id, so at most four entries live,
// each holding the last-drawn companion's payload — switching companions changes the key and
// re-fetches, so there is no stale cross-companion data and no growth.
const sideHeld = {};
async function sideFetch(id, a, path) {
  const key = contextKey(a) + '\0' + labelVer;
  const held = sideHeld[id];
  if (held && held.key === key) return held.data;
  const data = await fetchList(path);
  // A failed fetch (null) is not cached: keep the last good payload so a blip does not blank the card
  // and the next frame tries again. null with nothing held reads as "no data", the empty state.
  if (data === null) return held ? held.data : null;
  sideHeld[id] = {key, data};
  return data;
}

async function drawPlan(a) {
  const box = document.getElementById('plan');
  const todos = await sideFetch('plan', a, '/plan' + qFor(a));
  if (sideCardSame(box, a, todos)) return;
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
  box.replaceChildren(head3(markedKey('#i-sl-chart-kanban', tr('field.plan'))), bar,
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
  const list = await sideFetch('handoffs', a, '/handoffs' + qFor(a));
  if (sideCardSame(box, a, list)) return;
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
    // The request, clipped to three lines — and a press opens the rest of it.
    //
    // The clip is the list rule ("보조 텍스트는 1~3줄로 제한"); the other half of that rule is that
    // the whole is one press away, and it was nowhere: no fold, no title, not clickable, and the
    // card's own fold had been taken off at compact. A 312-character instruction showed its first
    // three lines and the remaining 144px belonged to nobody.
    const req = cell('req', h.request);
    req.setAttribute('role', 'button');
    req.setAttribute('tabindex', '0');
    req.setAttribute('aria-expanded', 'false');
    const flip = () => {
      const open = req.classList.toggle('all');
      req.setAttribute('aria-expanded', String(open));
    };
    req.onclick = flip;
    req.onkeydown = ev => {
      if (ev.key !== 'Enter' && ev.key !== ' ') return;
      ev.preventDefault();
      flip();
    };
    row.append(to, req);
    // The answer only when the work is over. Anything else would be reporting a sentence
    // mid-thought as a conclusion.
    row.append(cell('ans', h.answer ? h.answer : 'still ' + h.state));
    return row;
  });
  box.replaceChildren(head3(countedKey('#i-sl-share-from-square', tr('field.handed_out'), rows.length)), ...rows);
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
  const list = await sideFetch('cron', a, '/cron' + qFor(a));
  if (sideCardSame(box, a, list)) return;
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
  box.replaceChildren(head3(countedKey('#i-sl-calendar-clock', tr('field.scheduled'), rows.length)), ...rows);
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

// put comes from drawDetail: these rows land after the grid is on screen, so they are placed by
// key like the rest and marked as arriving late, or the next poll's sweep would remove them
// between being asked for and answering.
async function drawContext(a, box, grid, field, put) {
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
  if (c.model) put(modelField(a, c.model));
  // Said once, where somebody would otherwise wonder why there is no cache figure at all.
  if (!c.cacheReported && !c.estimated) {
    put(field('field.cache', tr('context.no_cache_report')));
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
  put(f);

  // A compaction is the one moment a companion silently stops knowing something. Four of them in
  // one session is the reason its earlier reasoning cannot be assumed still there.
  if (c.compactions) {
    const v = cell('v', c.compactions === 1 ? tr('context.fold')
                                       : tr('context.folds', {n: c.compactions}));
    const s2 = document.createElement('small');
    // Local time through the same helper the rest of the page uses, not slice(11,16)+'Z'. A real
    // daemon marshals its time WITH the host offset, so pinning "Z" on it printed a time that was
    // wrong by the machine's UTC offset (9h here). And the connective words are from the pack, not
    // raw ' · last '/' at ' English inside a Korean sentence.
    s2.textContent = ' · ' + tr('context.shed', {n: (c.shed || 0).toLocaleString()}) +
                     (c.lastBefore ? ' · ' + tr('context.last_run',
                        {before: c.lastBefore.toLocaleString(), after: (c.lastAfter || 0).toLocaleString()}) : '') +
                     (c.lastAt && hhmm(c.lastAt) ? ' · ' + tr('context.at', {time: hhmm(c.lastAt)}) : '');
    v.append(s2);
    const cf = cell('f');
    cf.append(cell('k', tr('field.summarised_away')), v);
    if (c.topics && c.topics.length) {
      // Naming them is the difference between "the detail is not lost" as a claim and as a fact:
      // these are the subjects the companion can pull back in full.
      cf.append(cell('v', c.topics.slice(0, 6).join(' · ') +
                          (c.topics.length > 6 ? ' +' + (c.topics.length - 6) : '')));
    }
    put(cf);
  }
}

// qFor is the query that names one companion: its socket, and the console it lives on.
function qFor(a) {
  const parts = ['d=' + encodeURIComponent(a.socket)];
  if (a.peer) parts.push('p=' + encodeURIComponent(a.peer));
  return '?' + parts.join('&');
}

// ── what I had to step in and say ─────────────────────────────────────────────
// The card that recorded what a person had to say mid-turn is gone.
//
// It grouped corrections by wording and offered to promote the repeated ones into the experience
// store, and the premise did not hold: what somebody says mid-turn is nearly always about THAT
// task — "no, the other file" is not a rule — the few that generalise are rare, and the grouping
// only ever matched identical wording, which people do not produce. The count that survived it
// ("three steers, one refusal") is a number nobody acted on, in a card that took a request every
// three seconds and a section in the phone's list. The corrections themselves are in the
// transcript, where they happened, next to what they were about.


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
const shared = {rules: 0, facts: 0, crossing: 0, pages: null, servers: null, reachedFrom: 0};
// Only from what actually answered. Written unconditionally, a 500 from /skills left the status
// line resting on "0 rules · 0 remembered · 0 crossing every companion" — a positive claim about an
// organisation's shared knowledge, made from the initialiser, over the top of the sentence that
// said the request had been refused.
function sayShared() {
  reach(true);
  // Only once the rules have actually answered. A refused /skills followed by a healthy /mcp
  // announced "0 rules · 0 remembered · 0 crossing every companion" — the initialiser's numbers,
  // spoken over the sentence that said the request had been refused. The server count is already
  // held back by its own null until /mcp answers.
  if (!sayShared.rules) return;
  const bits = [tr(shared.rules === 1 ? 'count.rule' : 'count.rules', {n: shared.rules}),
                tr(shared.facts === 1 ? 'count.remembered_one' : 'count.remembered', {n: shared.facts}),
                tr('count.crossing', {n: shared.crossing})];
  // Null until the servers have answered, which is not the same as none — a line that said "0
  // servers" while the request was in flight would be wrong for as long as it took.
  // The wiki is a third of this screen and the summary did not count it: a reader who cannot see
  // the screen heard about skills, memories and servers and never learned the canonical pages
  // existed. Null until /wiki answers, for the same reason the servers are.
  if (shared.pages !== null) {
    bits.push(tr(shared.pages === 1 ? 'count.page' : 'count.pages', {n: shared.pages}));
  }
  if (shared.servers !== null) {
    bits.push(tr(shared.servers === 1 ? 'count.server' : 'count.servers', {n: shared.servers}));
  }
  // Announced, not parked in the status line.
  //
  // This is a standing summary of a screen, and the status line is where something that just
  // happened goes: written there it never cleared, so on a phone the bar permanently read
  // "9 rules · 11 remembered · 0 cro…" — 22% of a sentence — while squeezing the connection dot to
  // a 1.9px sliver and a companion's name to two letters, and while a real message (a refusal, a
  // send that failed) had to fight it for the room. Two writers on one readout, which is the shape
  // this page keeps finding. The counts are one glance down the screen they describe; the reader
  // who cannot see that screen is told them here.
  say(bits.join(' · '));
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
// A touch has no hover, so it gets the other way in: a long press.
//
// "여는 법: 데스크톱 호버, 모바일 길게 누르기." Without this half, everything on a phone that
// ellipses — the status line, a workspace path, a cut summary — carried a tooltip with the rest of
// its words and no way to open it: a tap fires pointerover and then pointerdown, and the line
// above hides the tip in the same gesture. Half a second of holding still shows it; moving or
// lifting before that cancels, so it never fights a scroll or a tap.
{
  let holdAt = 0, holdFor = null;
  const drop = () => { clearTimeout(holdAt); holdAt = 0; holdFor = null; };
  addEventListener('touchstart', e => {
    const host = e.target.closest && e.target.closest('[data-tip]');
    if (!host) return;
    holdFor = host;
    holdAt = setTimeout(() => {
      if (holdFor) showTip(holdFor);
      holdAt = 0;
      // And it leaves by itself. A touch never fires pointerout, so a tip opened by one stayed on
      // the page — measured, still there after the dialog it belonged to had been cancelled, and
      // only cleared by a tap somewhere else. The guide gives a tooltip 1.5s after the pointer
      // leaves; this is the same 1.5s, counted from when there is no pointer to leave.
      setTimeout(hideTip, 1500);
    }, 500);
  }, {capture: true, passive: true});
  for (const on of ['touchmove', 'touchend', 'touchcancel']) {
    addEventListener(on, drop, {capture: true, passive: true});
  }
}
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
  // Said once. This is a polite live region: writing the same sentence into it twice queues it
  // twice, and the two callers that rebuild a summary — once from what is known, once when the
  // server's count lands — did exactly that. Measured with an observer: two callbacks, identical
  // strings, one for each tap of a destination.
  if (noteEl.textContent === text) return;
  noteEl.textContent = text;
  if (text) tip(noteEl, text); else noteEl.removeAttribute('data-tip');
}

function say(text) {
  clearTimeout(sayTimer);
  // A different sentence is a change, and a live region announces changes — so it goes in now.
  if (sayEl.textContent !== text) { sayEl.textContent = text; return; }
  // The same sentence is not a change, so the second search landing on the same count would be
  // silent. Cleared first, then written back a frame later, which the region does hear.
  sayEl.textContent = '';
  sayTimer = setTimeout(() => { sayEl.textContent = text; }, 60);
}

// Redraw without throwing away the caret.
//
// replaceChildren removes every child before putting it back, and removing a focused node blurs
// it — so a list that redraws from its own search field takes the keyboard away on the first
// keystroke. Keeping the field is half the fix (the same element keeps its value); this puts the
// focus back once the redraw has actually happened, which is after a fetch and therefore not on
// the next frame.
function keepingFocus(key, fn) {
  // The field this screen filters by, if somebody is typing in it. Reading document.activeElement
  // here is not enough: the redraw for one keystroke lands while the next is being typed, so by
  // then the focus is already on <body> and there is nothing left to put back. The field remembers
  // for itself, from focusin to focusout.
  const box = findBoxes.get(key);
  const keep = box && box.typing ? box.f : null;
  fn();
  if (!keep || !keep.isConnected || document.activeElement === keep) return;
  keep.focus();
}

// The box outlives the list it filters.
//
// Both screens that use this redraw themselves from the input handler — loadSkills and loadMCP end
// in replaceChildren — so a freshly built field was removed by the very keystroke typed into it:
// measured, focus on <body> and the second character going nowhere. One character is also less
// than the three the ranking needs, so the search could never return anything but everything.
//
// Kept per caller and reused, the way the facts card keeps its fold wrapper for the same reason.
const findBoxes = new Map();
function findBox(get, set, key) {
  // Keyed by a NAME the caller gives. Keyed on the getter, every call built a fresh arrow function
  // and therefore a fresh key — the cache never hit and the box was rebuilt exactly as before.
  const had = findBoxes.get(key);
  if (had) { if (document.activeElement !== had.f) had.f.value = get(); return had.box; }
  const box = cell('skfind');
  const f = withGlass(document.createElement('md-outlined-text-field'));
  f.setAttribute('label', tr('label.find'));
  f.value = get();
  f.addEventListener('input', () => set(f.value));
  // Whether somebody is typing in it. The redraw for one keystroke can land while the next is
  // being typed, so by then document.activeElement is already <body> and there is nothing left to
  // put back — the field has to remember for itself.
  f.addEventListener('focusin', () => { const b = findBoxes.get(key); if (b) b.typing = true; });
  f.addEventListener('focusout', () => {
    // Not when the redraw did it: a blur from replaceChildren is followed immediately by our own
    // focus() call, a blur from a press somewhere else is not.
    const b = findBoxes.get(key);
    if (!b) return;
    if (typeof requestAnimationFrame !== 'function') { b.typing = false; return; }
    requestAnimationFrame(() => { if (document.activeElement !== f) b.typing = false; });
  });
  box.append(f);
  findBoxes.set(key, {box, f});
  return box;
}

// A heading over each half of the shared destination. Two lists under one tab need to say which is
// which, and the destination's own name is now the pair rather than either.
function sectionHead(key, action, level) {
  // h3 where the section sits INSIDE another one: the shared screen put its two sub-lists at the
  // same level as the pane that holds them — three h2s for a two-level structure — while the
  // access screen next door already used h3 for exactly this shape. Content hierarchy, not
  // visual style, is what picks the number.
  const h = document.createElement(level === 3 ? 'h3' : 'h2');
  h.className = 'sectionhead';
  h.append(cell('', tr(key)));
  // Named by its words. The action below is appended INSIDE the heading — which is what puts the
  // two on one line — and a heading takes its name from everything in it: measured, "Meeting
  // Companions", "How it is laid out As a table", "Servers Add a server". The control stays where
  // it is drawn; the heading says what it is a heading for.
  h.setAttribute('aria-label', tr(key));
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
const skillFind = () => findBox(() => skillQuery, v => { skillQuery = v; loadSkills(); }, 'skills');
const mcpFind = () => findBox(() => mcpQuery, v => { mcpQuery = v; loadMCP(); }, 'mcp');
// The wiki is the third pane of this screen and the only one without a box, while the lead
// sentence over all three says "Search to find one" — an instruction with no control under it.
// Same box, same key discipline, same ranking as the skills half.
let wikiQuery = '';
const wikiFind = () => findBox(() => wikiQuery, v => { wikiQuery = v; loadWiki(); }, 'wiki');

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
  reading(skillsEl, 'loading.skills');
  const list = await fetchList('/skills');
  if (!list) return void paneFailed(skillsEl, 'nav.lessons');
  const crossing = list.filter(s => s.tier === 'global').length;
  const rules = list.filter(s => s.kind !== 'memory').length;
  reach(true);
  shared.rules = rules;
  sayShared.rules = true;   // this half has answered at least once
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
  // Said before the branch, not after it. The one search that has to be announced is the one that
  // empties the list — a sighted reader watches it shrink, and the announcement lived past an
  // early return, so zero hits was the single case that said nothing at all.
  if (skillQuery) say(tr(shown.length === 1 ? 'find.result' : 'find.results', {n: shown.length}));
  if (!shown.length) {
    keepingFocus('skills', () => skillsEl.replaceChildren(sectionHead('nav.lessons'), skillFind(),
      emptyState('empty.no_match', 'empty.no_match_how'), skillWrite(list)));
    return;
  }
  // Rules and memories are two kinds of thing and the screen said so nowhere: measured, the nine
  // rules were appended under the twenty memories, in the same card with the same two controls,
  // starting 3,845px into a 5,905px page, and the only tell was the word "rule" in the grey line at
  // the BOTTOM of each one. Seven screenfuls past content the tab had named. Each kind gets its own
  // heading; a search still draws one flat list, because there the order is the ranking.
  const draw = sk => {
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
    // The word on it is "Forget"; the name it answers to says what it forgets. Measured in the
    // accessibility tree, this screen offered twenty controls called "Read" and twenty called
    // "Forget" — the guide names a page with several "Save"s as the case that needs labels, and
    // this is that case twenty times over.
    drop.setAttribute('aria-label', tr('action.forget_named', {name: sk.name}));
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
    const bits = [sk.kind === 'memory' ? 'memory' : 'skill', sk.name];
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
    more.setAttribute('aria-label', tr('action.read_named', {name: sk.name}));
    // The button OWNS the body it shows: six other disclosures on this page say so with
    // aria-expanded, and these two — the only ones a screen reader meets in the Knowledge screen —
    // said nothing, so "Read" read as a link to somewhere rather than a toggle in place.
    more.setAttribute('aria-expanded', 'false');
    withMark(more, '#i-sl-file-lines');
    more.onclick = () => {
      open = !open;
      text.hidden = !open;
      more.setAttribute('aria-expanded', String(open));
      more.textContent = tr(open ? 'action.collapse' : 'action.read');
      // Named in BOTH states. action.collapse carries no {name}, so opening every row put twenty
      // buttons called "Close" back on the screen — the same defect in the other state.
      more.setAttribute('aria-label', open ? tr('action.collapse') + ' — ' + sk.name
                                           : tr('action.read_named', {name: sk.name}));
    };
    top.insertBefore(more, drop);
    el.append(text);
    return el;
  };
  const isRule = shown.filter(sk => sk.kind !== 'memory');
  const isFact = shown.filter(sk => sk.kind === 'memory');
  const parts = [sectionHead('nav.lessons')];
  // A lead sentence, so the section reads as itself at rest rather than as a heading over an empty
  // find box — every other destination has one and this did not. Only when nothing is typed: a
  // filtered list wants the count, not the description.
  if (!skillQuery.trim()) parts.push(cell('accsay', tr('shared.lead')));
  parts.push(skillFind());
  // And when filtering, the count in words under the box — the live region already announces it,
  // this is the same for the eye, so a search that shrinks the list says so on screen.
  if (skillQuery.trim()) parts.push(cell('filesnote', tr(shown.length === 1 ? 'find.result' : 'find.results', {n: shown.length})));
  if (skillQuery.trim() || !isRule.length || !isFact.length) {
    parts.push(...shown.map(draw));
  } else {
    parts.push(sectionHead('nav.rules', null, 3), ...isRule.map(draw),
               sectionHead('nav.memories', null, 3), ...isFact.map(draw));
  }
  keepingFocus('skills', () => skillsEl.replaceChildren(...parts, skillWrite(list)));
}

// ── the wiki: canonical pages ────────────────────────────────────────────────
// What the companions hold as CURRENT truth, per topic — including the stale tombstones the
// agent-facing index deliberately hides, because a governance screen is exactly where a person
// wants to see what was retired and why. Read-only here: a page is corrected by writing it
// (remember{page:…}) and retired by a stale revision, never by a delete button — a deletion
// would resurrect on the next fleet sync anyway.
async function loadWiki() {
  reading(wikiEl, 'loading.wiki');
  const list = await fetchList('/wiki');
  if (!list) return void paneFailed(wikiEl, 'nav.wiki');
  shared.pages = list.length;
  sayShared();
  if (!list.length) {
    wikiEl.replaceChildren(sectionHead('nav.wiki'),
      emptyState('empty.no_pages', 'empty.no_pages_how'));
    return;
  }
  const draw = p => {
    const el = cell('sk ' + p.tier + (p.stale ? ' fact' : ''));
    const top = cell('top');
    top.append(cell('tier',
      (p.tier === 'global' ? tr('reach.every_companion')
       : p.tier === 'team' ? tr('reach.team', {team: p.team})
       : tr('reach.only', {name: p.companion}))));
    top.append(cell('what', (p.stale ? '⚠ ' : '') + p.title));
    el.append(top);
    const bits = [];
    if (p.stale) bits.push(tr('wiki.stale'));
    if (p.editor) bits.push(tr('wiki.edited_by', {name: p.editor}));
    if (p.updated) bits.push(p.updated.slice(0, 10));
    if (p.summary) bits.push(p.summary);
    if (p.links && p.links.length) bits.push('→ ' + p.links.join(', '));
    if (bits.length) el.append(cell('meta', bits.join(' · ')));
    const body = (p.body || '').trim();
    if (!body) return el;
    const text = cell('body');
    text.textContent = body;
    text.hidden = true;
    const more = document.createElement('md-text-button');
    more.className = 'fold';
    let open = false;
    more.textContent = tr('action.read');
    more.setAttribute('aria-label', tr('action.read_named', {name: p.title}));
    more.setAttribute('aria-expanded', 'false'); // same contract as the skills fold above
    withMark(more, '#i-sl-file-lines');
    more.onclick = () => {
      open = !open;
      text.hidden = !open;
      more.setAttribute('aria-expanded', String(open));
      more.textContent = tr(open ? 'action.collapse' : 'action.read');
      more.setAttribute('aria-label', open ? tr('action.collapse') + ' — ' + p.title
                                           : tr('action.read_named', {name: p.title}));
    };
    top.append(more);
    el.append(text);
    return el;
  };
  let shown = list;
  if (wikiQuery.trim()) {
    const docs = list.map(p => [p.title, p.summary || '', p.body || '', p.editor || ''].join(' '));
    const order = rankByIDF(wikiQuery, docs);
    shown = order.map(i => list[i]);
  }
  const parts = [sectionHead('nav.wiki'), wikiFind()];
  if (wikiQuery.trim()) {
    parts.push(cell('filesnote', tr(shown.length === 1 ? 'find.result' : 'find.results', {n: shown.length})));
    say(tr(shown.length === 1 ? 'find.result' : 'find.results', {n: shown.length}));
  }
  keepingFocus('wiki', () => wikiEl.replaceChildren(...parts, ...shown.map(draw)));
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
  reading(accessEl, 'loading.access');
  const got = await fetchList('/access');
  if (!got) return void paneFailed(accessEl, 'nav.access');
  const roles = (got.roles || []).map(r => r.name);
  // Adding the first person is the act that turns the gate on, so a console with nothing in front
  // of it to say who anybody is cannot do it: it would refuse everybody afterwards, starting with
  // whoever pressed the button. The server says so — measured live, 409 with the reason — but the
  // reason arrived as a status note clipped at 80 characters, which cut off the half that says
  // what to do about it. So the button is not offered, and the empty state carries the sentence
  // instead, where there is room for all of it.
  const mayAdd = got.configured || got.named;
  const head = sectionHead('nav.access',
                           mayAdd ? addPersonButton(got.roles || [], !got.configured) : null);
  // Whose list this is, before the list. Drawn on both branches: "nobody is listed" is a statement
  // about one instance too, and on a page that shows companions from several machines it was the
  // branch most likely to be read as a statement about all of them.
  const whose = instanceLine(got.instance);
  if (!got.configured) {
    // Not an empty table: a console with nobody listed is the one-operator console, and which of
    // the two this is answers "was my file read".
    accessEl.replaceChildren(head, ...whose,
                             emptyState('access.nobody', mayAdd ? 'access.nobody_how' : 'access.nobody_unnamed'));
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

// A capability as a DISPLAY tag on a row — what this group or person may do, not a control. It was
// an md-filter-chip like the legend's, so a grant shown on a row was also a roster filter: press a
// group's "admin" and every "admin" tag on the screen lit and the list narrowed, which reads as
// editing that group rather than filtering. The row shows; the legend at the foot is where a
// capability is PICKED to filter. Non-interactive, so it announces as text, not a toggle.
function capTag(word) {
  const t = cell('captag', word);
  t.setAttribute('data-cap', word);
  return t;
}

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
  for (const c of (can || [])) caps.append(capTag(c));
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

// setPerson writes one person's line: their role, and which companions it is narrowed to.
//
// It was CALLED from four places — the role menu, adding somebody, and both ends of the scope
// list — and defined in none of them. Every write on this screen threw ReferenceError into the
// console and did nothing: the role snapped back on the next poll, a companion added to a scope
// never appeared, and "Add somebody" looked like a button with nothing behind it. Nothing caught
// it because a page that loads fine and a handler that throws are different events, and no test
// pressed these.
//
// Empty companions means "every companion here", which is what the route already understands and
// what the screen says under the field.
function setPerson(who, role, companions) {
  return post('/access', new URLSearchParams({who: who, role: role, companions: companions || ''}), '', '')
    .then(why => { if (!why) loadAccess(); });
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
  drop.setAttribute('aria-label', tr('action.remove_named', {name: p.who}));
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
    const drop = () => setPerson(p.who, p.role, on.filter(n => n !== name).join(','));
    // On a phone the whole chip is the control.
    //
    // An input chip's trailing × is its own action inside a 75dp chip: measured, 34×48 next to a
    // 41×48, where the guide asks for 48 and 88dp of chip — and it says what to do about it at
    // this width: "⚠ compact에서는 후행 아이콘 타겟이 너무 작다 — 칩 전체가 그 동작을 하게 만들 것."
    // So the phone gets a chip that removes, and because removing somebody's access to a companion
    // is not undoable, it asks first: the same two-press arm() every destructive control here uses.
    const c = document.createElement('md-input-chip');
    c.setAttribute('label', name);
    c.className = 'scopechip';
    // The chip's own × always removes. It is the component's trailing action and it is drawn
    // whatever the page says: `remove-only` decides whether the LABEL is a button, not whether the
    // × exists, and its handler stops the event inside the shadow root before anything on the host
    // can hear it — so a compact branch that listened for a click on the host and not for `remove`
    // left a × that took the chip off the screen and told the server nothing. Somebody's access
    // looked revoked and was not, until the next redraw brought it back.
    c.addEventListener('remove', drop);
    if (!onePane()) { chips.append(c); continue; }
    // …and on a phone the whole chip does it too, because 34×48 inside a 75dp chip is the target
    // the guide names at this width: "⚠ compact에서는 후행 아이콘 타겟이 너무 작다 — 칩 전체가
    // 그 동작을 하게 만들 것." Removing somebody's access is not undoable, so the big target asks
    // first — the two presses every destructive control on this page uses — while the small one
    // stays what it has always been.
    const named = word => {
      c.setAttribute('label', word);
      // The name follows the word. A label written on the host wins over its contents, so a chip
      // reading "Confirm?" and answering to "Forget api" is the split arm() was fixed for.
      c.setAttribute('aria-label', word === name ? tr('action.forget_named', {name}) : word);
    };
    named(name);
    let armed = false, timer = 0;
    c.addEventListener('click', ev => {
      ev.preventDefault();
      if (armed) { clearTimeout(timer); drop(); return; }
      armed = true;
      c.classList.add('armed');
      named(tr('action.confirm'));
      timer = setTimeout(() => { armed = false; c.classList.remove('armed'); named(name); }, 5000);
    });
    chips.append(c);
  }
  // And the way to add one. A name rather than a menu of what is running: a person can be scoped
  // to a companion that is not up at the moment, and a menu would refuse to say so.
  const add = withGlass(document.createElement('md-outlined-text-field'));
  add.setAttribute('label', tr('access.add_companion'));
  add.addEventListener('keydown', ev => {
    if (ev.key !== 'Enter' || composing(ev)) return;
    const name = String(add.value || '').trim();
    if (!name || on.includes(name)) return;
    setPerson(p.who, p.role, on.concat([name]).join(','));
  });
  box.append(chips, add);
  return box;
}

// The way in, and what the person coming in may do.
//
// The role used to be chosen here rather than asked for — viewer, or whatever came first — which
// was wrong twice. Silently, because somebody adding a colleague was not told what they had just
// granted; and outright on a console with nobody on it yet, where the first person MUST be able to
// admin: a console with people and no admin refuses to start, so the server refuses to create one,
// and the only offered path always ended in that refusal. Now it is asked, and the answer it
// starts on is the one that will not be turned down.
function addPersonButton(roles, first) {
  const names = roles.map(r => r.name);
  const admin = (roles.find(r => (r.can || []).includes('admin')) || {}).name;
  const usual = names.includes('viewer') ? 'viewer' : (names[0] || '');
  const b = label(withMark(document.createElement('md-text-button'), '#i-sl-plus'),
                  tr('access.add'));
  b.onclick = () => askLine({
    head: tr('access.add'),
    body: first && admin ? tr('access.add_first') : tr('access.add_who'),
    label: tr('access.who'),
    pick: {label: tr('access.role'), options: names, value: first ? (admin || usual) : usual},
    doIt: tr('access.add'), doMark: '#i-sl-plus',
    go: (who, role) => setPerson(who, role || usual, ''),
  });
  return b;
}

async function loadMCP() {
  reading(mcpEl, 'loading.servers');
  const list = await fetchList('/mcp');
  if (!list) return void paneFailed(mcpEl, 'nav.mcp');
  const reachedFrom = new Set(list.map(s => s.companion || 'every companion here'));
  reach(true);
  shared.servers = list.length;
  sayShared.servers = true;
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
    // From the pack — the two phrases already existed there for the access screen; this line was
    // the console's one hardcoded English string, sitting in Korean pages untranslated.
    top.append(cell('tier', sv.tier === 'global' ? tr('access.everywhere')
                                                 : tr('reach.only', {name: sv.companion})));
    top.append(cell('what', sv.name));
    // Editing one meant typing all of it into the add form again and trusting the name matched —
    // the write is by name, so a typo made a SECOND server rather than changing the first.
    const edit = document.createElement('md-text-button');
    edit.className = 'srvedit';
    edit.textContent = tr('action.edit');
    edit.setAttribute('aria-label', tr('action.edit_named', {name: sv.name}));
    withMark(edit, '#i-sl-pen-to-square');
    tip(edit, tr('hint.edit_server', {file: sv.file}));
    edit.onclick = () => openMCP(sv);
    top.append(edit);
    const drop = document.createElement('md-text-button');
    drop.className = 'drop';
    drop.setAttribute('aria-label', tr('action.remove_named', {name: sv.name}));
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
    // One asterisk. The component adds its own for a required field — the bundle does it in
    // renderLabel — so writing a second one into the label drew "Name **" on screen.
    i.setAttribute('label', tr(labelKey));
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
    closeX(mcpDialog, mcpDialogK);
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
    mcpEl.replaceChildren(sectionHead('nav.mcp', open), emptyState('empty.no_servers', 'empty.no_servers_how'));
    return;
  }
  if (!rows.length) {
    keepingFocus('mcp', () => mcpEl.replaceChildren(sectionHead('nav.mcp', open), mcpFind(),
      emptyState('empty.no_match', 'empty.no_match_how')));
    return;
  }
  keepingFocus('mcp', () => mcpEl.replaceChildren(sectionHead('nav.mcp', open), mcpFind(), ...rows));
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
  // Decode the JSON encoding first: stripping the outer quotes left the inner \t and \n as literal
  // backslashes in the summary line. decodeToolText turns "\"1\\t# title\\n\"" into real text.
  const t = decodeToolText(out).trim();
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
// A tool result arrives as the JSON encoding of its value: a string comes wrapped in quotes with
// its newlines and tabs backslash-escaped ("\"1\\t# title\\n\""), an array as "[…]". Shown raw,
// the reader sees the backslashes and the quotes. Decode a JSON scalar or array to the text it
// stands for; objects are left for jsonPairs, and anything that is not JSON is returned unchanged.
function decodeToolText(text) {
  const t = String(text || '');
  const trimmed = t.trim();
  if (!trimmed || (trimmed[0] !== '"' && trimmed[0] !== '[')) return t;
  let v;
  try { v = JSON.parse(trimmed); } catch { return t; }
  if (typeof v === 'string') return v;                 // the string it encodes, with real newlines
  if (Array.isArray(v)) {
    // A list of scalars reads as one per line; a list of objects keeps its JSON, which is the least
    // bad rendering of structure without a schema.
    if (v.every(x => x === null || typeof x !== 'object')) return v.map(x => String(x)).join('\n');
    return v.map(x => JSON.stringify(x)).join('\n');
  }
  return t;
}

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
    //
    // Only a PRESS writes it. Setting det.open above fires a toggle too — asynchronously, so the
    // listener attached synchronously here still catches it — and that echo was writing the
    // preference: every failed tool call, which starts open, silently set "open all tool calls"
    // for a reader who never touched one. Measured: fold.tool flipped shut→open on a render, and
    // the next visit brought unopened grep rows up open. The flag is armed on the task AFTER the
    // programmatic toggle has flushed, so the echo is ignored and a real click is not.
    let userToggle = false;
    det.addEventListener('toggle', () => {
      if (!userToggle) return;
      localStorage.setItem('fold.' + r.who, det.open ? 'open' : 'shut');
    });
    setTimeout(() => { userToggle = true; }, 0);
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
      for (const [key, rawText, asDiff] of parts) {
        // One block needs no label: with nothing to tell it apart from, a word above it is noise.
        if (key && parts.length > 1) body.append(cell('foldk', tr(key)));
        // A tool result is a JSON-encoded value; the arguments are already an object jsonPairs
        // reads, but a bare string or array result was shown with its escapes and quotes. Decoded
        // to the text it stands for — after the diff check, which the raw form also confused.
        const pairs = asDiff ? null : jsonPairs(rawText);
        const text = (asDiff || pairs) ? rawText : decodeToolText(rawText);
        if (asDiff) {
          const pre = el('pre');
          pre.className = 'diff';
          body.append(diffInto(pre, text));
        } else if (pairs) {
          body.append(pairsInto(pairs));
        } else {
          body.append(childLinks(el('pre', text)));
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
  reading(mapEl, 'loading.map');
  const [rows, hands] = await Promise.all([fetchList('/fleet'), fetchList('/handoffs')]);
  // Both halves. `hands || []` drew a complete-looking map with no wires on it and the full legend
  // underneath — answered, in flight, cannot be reached — for a request that had been refused. A
  // furnished lie is worse than a blank pane.
  if (!rows || !hands) return void paneFailed(mapEl, 'nav.map');
  const head = sectionHead('nav.map', toTable());
  const boxes = cell('places');
  // Two boundaries, two boxes. The outer one is the MACHINE, which is what a network reaches and
  // what goes down; the inner one is the account on it, which is what owns a config directory, a
  // policy, a key and a session store. Nesting them says both without a word of explanation — and
  // it says the thing a flat list of "you@studio, sam@studio, you@buildbox" makes a reader
  // assemble for themselves: that two of those share a kernel and two share an owner.
  // Nothing to lay out is its own state, not a legend over an empty canvas. The map drew its lead
  // paragraph and the wire legend — "answered · in flight · cannot be reached" — over no boxes at
  // all, a key for a picture that was not there. Every other destination names its own emptiness.
  if (!rows.length) {
    mapEl.replaceChildren(sectionHead('nav.map', toTable()),
                          emptyState('map.empty', 'map.empty_how'));
    return;
  }
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
  drawWires(canvas, wires, rows, hands);
  watchWires(canvas, () => drawWires(canvas, wires, rows, hands));
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
// that, plus the two things a person does in a room — say something, and end it.
//
// It is WATCHED, not polled. This paragraph used to say the opposite — that a turn is a minute of
// model time so a stream would deliver nothing sooner than the next tick — and that stopped being
// true when the room got its own subscription (`/events?m=<id>`, see the `meet` entry in the view
// table): a sentence appears when the driver writes it rather than up to two seconds later. What
// the listener does NOT do is draw the frame: it calls loadMeet, which does its own reading,
// because the redraw has rules of its own (it holds still while somebody is typing, and rebuilds
// only when the answer is different) and a second path in would be a second set of them.
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

let lastMeetShown = '';
async function loadMeet() {
  const id = meetOf();
  // Leaving a meeting, or switching to another, drops the last one's per-participant caches. They
  // are keyed by speaker and were never cleared, so a long-lived tab that visited many meetings
  // kept every participant's live rows, fetched conversation and open-work flags for all of them.
  if (id !== lastMeetShown) {
    roomLive.clear();
    roomRows.clear();
    workOpen.clear();
    lastMeetShown = id;
  }
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
  // A companion that has left the fleet must leave the selection with it. meetPick was only emptied
  // on a successful convene, so a pick that went offline kept Convene enabled (armConvene reads
  // meetPick.size) with no chip to see or clear, and its dead socket was still POSTed to /meet.
  // Intersect the pick with who is actually here, every draw.
  const hereSet = new Set(here.map(a => a.socket));
  for (const s of [...meetPick]) if (!hereSet.has(s)) meetPick.delete(s);
  // Grouped by whose they are, coloured by which team they belong to.
  //
  // One row of chips was fine for four companions and stops being fine at fifteen: the two facts a
  // person picks by — whose account runs it, and which team it belongs to — were in neither the
  // label nor the order. Owner is the grouping because it is the harder boundary (two accounts on
  // one machine cannot see each other's work at all); team is the colour because it is what makes
  // a set of chips scannable rather than a wall of names.
  //
  // Colour is never the only telling: the team is in the tooltip too, which is where this page
  // puts every other fact it cannot fit on a control.
  const owner = a => a.instance || a.host || '';
  const teams = [...new Set(here.map(a => a.team).filter(Boolean))].sort();
  const groups = new Map();
  for (const a of here) {
    if (!groups.has(owner(a))) groups.set(owner(a), []);
    groups.get(owner(a)).push(a);
  }
  const who = cell('meetwho');
  // A chip set of one is not a set — the guidance says so twice, once about chips never standing
  // alone and once about a filter that offers a single choice. With fewer than two companions
  // there is no room to fill, and the line below says that in words instead.
  for (const [mine, kids] of (here.length > 1 ? [...groups] : [])) {
    // The owner's name only when there is more than one of them: a heading over the single group
    // every ordinary console has says nothing and costs a line.
    if (groups.size > 1 && mine) who.append(cell('meetowner', mine));
    const set = document.createElement('md-chip-set');
    who.append(set);
  for (const a of kids) {
    const c = document.createElement('md-filter-chip');
    const slot = a.team ? teams.indexOf(a.team) % 4 : -1;
    if (slot >= 0) c.classList.add('tm' + slot);
    // The name alone. A chip label is capped at twenty characters by the guidance and these were
    // running to sixty — "design — the design system: component specs and visual review" is a
    // sentence wearing a chip. What it is for belongs in the tooltip, where the rest of this page
    // puts the same fact.
    c.setAttribute('label', a.name);
    const says = [a.team ? tr('meet.of_team', {team: a.team}) : '', a.role].filter(Boolean).join(' · ');
    if (says) tip(c, says);
    c.selected = meetPick.has(a.socket);
    c.onclick = () => {
      // The chip owns its own selected state and flips it after the click; what this reads is what
      // it is ABOUT to become, so the set and the drawing cannot disagree. Same lesson the pane
      // handles learned the hard way.
      if (meetPick.has(a.socket)) meetPick.delete(a.socket); else meetPick.add(a.socket);
      armConvene();
    };
    set.append(c);
  }
  }

  const go = label(withMark(document.createElement('md-filled-button'), '#i-sl-comments'),
                   tr('meet.start'));
  go.className = 'meetgo';
  meetGoBtn = go;
  go.onclick = () => whileItRuns(go, async () => {
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
  });

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
    box.append(sectionHead(key, null, 3));
    const l = cell('meetlist');
    for (const m of rooms) l.append(meetRow(m));
    box.append(l);
  }
  meetEl.replaceChildren(box);
  armConvene();
}

// whileItRuns locks a control for as long as the thing it started is in flight, and says so.
//
// A slow backend makes a press look like nothing happened. What a person does then is press again,
// and the console gets two of everything — a live run convened the same meeting twice that way,
// and it took a lock in the server to undo. This is the other half: the button says it heard you.
//
// Restored whatever happens, including a throw. A control left disabled by a failure is a screen
// somebody has to reload.
async function whileItRuns(btn, run) {
  if (!btn) return run();
  const was = btn.textContent;
  btn.disabled = true;
  if (was) btn.textContent = tr('action.working');
  try {
    return await run();
  } finally {
    btn.disabled = false;
    if (was) btn.textContent = was;
  }
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
  // Only when there are numbers. A room whose payload is missing those fields — an older daemon, a
  // half-written record — interpolated them as they came and the screen read "Round undefined of
  // undefined".
  // Falsy, not absent. Go writes an int with no omitempty, so the "older daemon" this guards
  // against sends `"round":0,"max":0` rather than leaving the keys out — and the screen read
  // "Round 0 of 0". A meeting's rounds start at one.
  if (!m.round || !m.max) return '';
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
  // Nothing new, nothing redrawn. The room is polled every two seconds and rebuilt from scratch,
  // which throws away the chips, the say box and the buttons — the same churn the workspace pane
  // had. A meeting changes when somebody says something, when the floor moves, or when it ends,
  // and all of that is in the answer.
  const shape = JSON.stringify(m);
  if (shape === drawRoom.shape && meetEl.children.length) return;
  drawRoom.shape = shape;
  const box = cell('meetbox');
  box.append(sectionHead('meet.title', toBack()));
  // The topic and the roster stay put while the transcript scrolls under them. Five rounds of four
  // companions is several screens, and what the meeting is ABOUT — and who is speaking now — are
  // the two things a reader needs at every point of it. They were at the top, so by the third lap
  // they were two screens above the sentence being read.
  const head = cell('meethead');
  // The question is the headline of this screen, so it is a heading and not a styled line: with
  // only the section's own h2 above it, everything below — the roster, the transcript, the
  // conclusions — hung off "Meeting" and a reader moving by headings never met the topic.
  const topic = document.createElement('h3');
  topic.className = 'meettopic';
  topic.textContent = m.topic;
  head.append(topic);
  head.append(cell('meetmeta', meetWhere(m)));
  // Before the room opens, say so and say how far along it is. A meeting that has been convened
  // and is silent looks exactly like one that has hung, and the difference is minutes of model
  // time somebody is deciding whether to wait for.
  const getting = !m.opened && !m.closed;
  const all = (m.speakers || []).filter(sp => !sp.person);
  const set = all.filter(sp => sp.ready || sp.trouble).length;
  if (getting) head.append(cell('meetgetting', tr('meet.getting', {n: set, of: all.length})));
  // Something is happening and it takes a minute. Without this the room is a still page between
  // turns — the same picture as a room that has stopped — and the guidance is explicit that a wait
  // whose length nobody can predict gets an indeterminate indicator rather than nothing.
  //
  // Three narrower conditions than "the meeting is not over", each measured on a phone:
  //
  //  - Not while the floor is held by the PERSON. The bar ran under "waiting for you — say it, or
  //    leave the box empty", which is a wait of zero: nothing is loading, and the guide's own
  //    table says show nothing at all for that.
  //  - Not while a companion is composing, because the block under this head already says "design
  //    is working on it" with its own indicator, and the guide asks for one indicator per wait.
  //  - Determinate while getting ready, because the numbers are right there in the sentence above
  //    it: "1 of 2 have read their workspace" over an indeterminate bar is a page withholding what
  //    it knows, and "정보가 생기면 indeterminate → determinate로 바뀌어야 한다".
  const composing = m.holder && all.some(sp => sp.name === m.holder);
  const heldByYou = m.holder && !composing;
  if ((getting || m.collecting || (!m.closed && !composing && !heldByYou))) {
    const bar = document.createElement('md-linear-progress');
    bar.className = 'meetbar-progress';
    if (getting && all.length) { bar.value = set / all.length; } else { bar.indeterminate = true; }
    // Named for what is being waited on, not "loading": whoever has the floor is the answer to
    // "why is nothing on the screen changing".
    bar.setAttribute('aria-label', !m.opened ? tr('meet.getting_ready')
      : m.collecting ? tr('meet.collecting')
      : tr('meet.waiting_on', {who: m.holder || upNextName(m) || tr('meet.somebody')}));
    head.append(bar);
  }
  // What went wrong, where it happened, rather than in a log nobody has open. A participant whose
  // daemon has gone is a fact about this meeting.
  if (m.trouble) head.append(cell('meettrouble', tr('meet.trouble', {why: m.trouble})));

  head.append(roster(m));
  box.append(head);
  // What whoever has the floor is doing while they compose. A meeting was a screen that said
  // nothing for a minute at a time and then produced a paragraph; the participants are working the
  // whole while, in conversations this console can already read.
  const speaking = m.holder && (m.speakers || []).find(sp => sp.name === m.holder && !sp.person);
  if (speaking && !m.closed) box.append(nowBlock(speaking.name));
  box.append(transcript(m));
  meetSayField = null;
  if (!m.closed) box.append(sayBox(m));
  if (m.closed && (m.tasks || []).length) box.append(conclusions(m));
  // A closed room, and a person who was not here for the end of it. The participants stop when
  // they have nothing left to add, which is also what happens while somebody is away for two
  // minutes — so there is a way back in, and it carries the reason with it.
  if (m.closed) box.append(reopenBox(m));
  meetEl.replaceChildren(box);
}

// nowBlock is the working of whoever is speaking, live.
function nowBlock(who) {
  const box = cell('meetnow');
  box.dataset.who = who;
  // A spinner that turns, since something is actually running. The class exists and this call
  // was not passing it: measured, animationName "none" on a glyph drawn as a spinner, under a
  // sentence saying a companion is working. The sibling that draws the same icon in the transcript
  // asks for it; this one had the other half of the pair.
  box.append(markedKey('#i-sl-spinner-third', tr('meet.doing_now', {who: who}), '', 'spin'));
  const rows = cell('meetnowrows');
  box.append(rows);
  const have = (roomLive.get(who) || []).filter(r => r.who !== 'user').slice(-liveTail);
  rows.replaceChildren(...(have.length ? have.map(rowNode) : [cell('dnote', tr('meet.thinking'))]));
  return box;
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

// Which colour each participant speaks in.
//
// Six, cycled, in the order they are in the room — the same trick the team chips use, and for the
// same reason: a transcript of four companions in one voice is a wall of text somebody has to read
// the attributions of to follow. The colour is never the only telling. The name is on every line,
// and the roster says the name too.
function speakerTints(m) {
  const by = {};
  let i = 0;
  for (const sp of (m.speakers || [])) {
    // The person is not one of the cycle. They already have a colour of their own on the roster,
    // and taking a turn in the rotation meant the fourth companion and the person wore the same
    // one — measured, both #FF7A1A.
    if (sp.person) continue;
    by[sp.name] = 'sp' + (i++ % 6);
  }
  return by;
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
  const tints = speakerTints(m);
  for (const s of (m.speakers || [])) {
    const holding = m.holder === s.name;
    // Before the room opens there is no floor and no turn — what there is, is homework. The chip
    // says which of the three states this participant is in, because a reader watching a blank
    // meeting cannot otherwise tell preparation from a hang.
    const waiting = !m.opened && !s.person;
    // Assist chips: each one does something. Not filter chips — nothing here is being filtered, and
    // a filter chip's tick would say "included" about a participant who is in the room either way.
    // A filter chip, because the bundle has those and assist chips are not in it — and the
    // semantic survives the substitution: the set has exactly one selected member, and the
    // selected one is whoever holds the floor. Pressing another is changing that selection.
    const c = document.createElement('md-filter-chip');
    // Speaking wins over next. While a companion is composing it is BOTH — it holds the floor and
    // it is still the one whose turn this round is — and a chip wearing two markers at once says
    // neither: the eye wants "this one now, that one after".
    c.className = 'meetsp ' + (tints[s.name] || '') + (holding ? ' holding' : '') + (s.next && !holding ? ' next' : '') +
                  (s.person ? ' person' : '') + (s.passes >= 2 ? ' resting' : '') +
                  (waiting && !s.ready && !s.trouble ? ' getting' : '') +
                  (waiting && s.ready ? ' set' : '') + (s.trouble ? ' lost' : '');
    c.setAttribute('label', s.name);
    c.selected = holding;
    // The state in words as well as in colour, where a colour alone would be the only telling.
    const what = s.trouble ? s.trouble
      : waiting && !s.ready ? tr('meet.getting_ready')
      : waiting ? tr('meet.ready')
      : holding ? tr('meet.holding')
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
      // One token, one selected chip. A filter chip toggles itself, so pressing a second one left
      // two chips ticked until the next poll answered — the roster saying two companions held the
      // floor — and pressing the holder's own chip un-ticked it while nothing had changed. The set
      // is single-select because the thing it draws is single: whoever is being called now.
      //
      // In the change event, and again after the component has finished with it: a chip writes its
      // own `selected` a microtask after the click, so a value assigned during the click is
      // overwritten. Same lesson as the pane handles.
      const only = () => {
        for (const other of box.children) other.selected = other === c;
      };
      c.onclick = async () => {
        // On click, and again once the component has finished with it. A filter chip flips its own
        // `selected` when its inner button is pressed and then re-dispatches the click out here, so
        // a value written now can still be undone by the chip's own update — the same beat the pane
        // handles had to learn about. (Not a `change` listener: only chips with `toggle` set fire
        // that, and these are filter chips.)
        only();
        await Promise.resolve();
        only();
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
  const tints = speakerTints(m);
  const rooms = {}, sockets = {}, turn = {};
  for (const sp of (m.speakers || [])) { rooms[sp.name] = sp.room || ''; sockets[sp.name] = sp.socket || ''; }
  let round = 0;
  for (const u of (m.said || [])) {
    if (u.round !== round) {
      round = u.round;
      box.append(cell('meetlap', tr('meet.lap', {n: round})));
    }
    const line = cell('meetline ' + (tints[u.who] || 'you') + (u.pass ? ' passed' : ''));
    line.append(cell('meetwho2', u.who));
    // Which of this participant's turns this sentence came out of. Its conversation holds them in
    // order — the preparation first, then one per time it was asked — so the count IS the address.
    turn[u.who] = (turn[u.who] || 0) + 1;
    // A pass is a contribution: somebody read the room and had nothing to add, which is worth
    // seeing. Drawn quieter than a sentence, and never dropped.
    if (u.pass) {
      line.append(cell('meettext', u.text ? tr('meet.passed_why', {why: u.text}) : tr('meet.passed')));
    } else {
      // Rendered, because the participants are models and models write markdown: the first live
      // meetings came back with lists, bold and fenced code, and this pane showed the asterisks
      // and the backticks. The same renderer the conversation uses — one markdown on this page,
      // not two — which also means the raw-HTML token is handled the one way it is handled there.
      // `txt` as well as `meettext`: the markdown styling on this page belongs to that class —
      // paragraphs, lists, fences, tables, and the pre-wrap that a rendered block must give up.
      // A second set of rules for the same job is how the two drift apart.
      line.append(md(cell('meettext txt'), u.text || ''));
    }
    // …and how it got there. A participant reads its own files and thinks before it says anything,
    // and none of that was anywhere on this screen: the room showed the sentence and a reader
    // wondering where a claim came from had nowhere to look. Behind a control rather than in the
    // transcript, because the thinking of four companions inline is not a discussion any more.
    if (rooms[u.who] && sockets[u.who]) {
      line.append(workingBox(u.who, sockets[u.who], rooms[u.who], turn[u.who]));
    }
    box.append(line);
  }
  if (!(m.said || []).length) box.append(cell('meetwait', tr('meet.waiting')));
  return box;
}

// What each participant's own conversation says right now, pushed by the meeting's stream.
//
// Kept by name rather than by session id: the screen asks "what is design doing", and a daemon
// that restarted mid-meeting answers in a new conversation without becoming a different companion.
const roomLive = new Map();

// paintRooms redraws the two places a participant's own working shows: the block under whoever is
// speaking, and any fold somebody has opened.
//
// Rebuilt in place rather than through drawRoom, which throws the room away and builds it again —
// that would take the fold, the say box and the caret with it, several times a minute.
function paintRooms(who) {
  const rows = roomLive.get(who) || [];
  const now = document.querySelector('.meetnow[data-who="' + cssq(who) + '"] .meetnowrows');
  if (now) {
    const tail = rows.filter(r => r.who !== 'user').slice(-liveTail);
    now.replaceChildren(...(tail.length ? tail.map(rowNode) : [cell('dnote', tr('meet.thinking'))]));
  }
  for (const box of document.querySelectorAll('.meetworkrows')) {
    if (box.hidden || box.dataset.who !== who) continue;
    const n = Number(box.dataset.turn || 0);
    const steps = turnRows(rows, n).filter(r => r.who !== 'assistant');
    box.replaceChildren(...(steps.length ? steps.map(rowNode) : [cell('dnote', tr('meet.working_gone'))]));
  }
}

// How much of a participant's working is shown while it is speaking. Enough to see what it is
// doing, not so much that three participants' reasoning becomes the screen.
const liveTail = 6;

// cssq escapes a name for an attribute selector — a companion may be called anything.
const cssq = s => String(s).replace(/["\\]/g, '\\$&');

// roomRows is one participant's meeting conversation, fetched once and kept.
//
// Kept for the life of the screen: the transcript is redrawn whenever the room changes, and a
// re-fetch per redraw would be a request per participant per turn for a fold nobody has opened.
const roomRows = new Map();

async function readRoom(socket, room, who) {
  // What the stream has already delivered, when it has: the meeting's own connection carries every
  // participant's conversation, so a fold that opened a second later has the rows in hand.
  if (who && roomLive.has(who)) return roomLive.get(who);
  const key = socket + '|' + room;
  if (!roomRows.has(key)) {
    roomRows.set(key, await fetchList('/transcript?d=' + encodeURIComponent(socket) +
                                      '&session=' + encodeURIComponent(room)) || []);
  }
  return roomRows.get(key);
}

// turnRows are the rows of one turn out of a conversation: everything after the nth thing it was
// asked, up to the next one.
//
// The participant's conversation is a run of turns — the preparation, then one per time the room
// asked it — so "the nth turn" is countable rather than guessed at. When the count does not reach
// n the answer is nothing at all: an approximate span of somebody else's thinking, shown under a
// sentence as though it produced it, is worse than a fold that opens on "not here".
function turnRows(rows, n) {
  const starts = [];
  rows.forEach((r, i) => { if (r.who === 'user') starts.push(i); });
  if (starts.length <= n) return [];
  const from = starts[n] + 1;
  const to = starts.length > n + 1 ? starts[n + 1] : rows.length;
  return rows.slice(from, to);
}

// Which of these boxes are open, across redraws.
//
// The room is rebuilt whenever something is said, and a fold somebody opened is inside what gets
// thrown away: measured live — opened it, the next participant spoke two seconds later, and it was
// shut again with no way to tell it from a press that did not land. Remembered by speaker and turn
// rather than by element, because the element is exactly the thing that does not survive.
const workOpen = new Set();

// workingBox is the control and the box it opens: what this companion read and thought before it
// said what it said.
function workingBox(who, socket, room, nth) {
  const key = who + '|' + nth;
  const box = cell('meetwork');
  const rows = cell('meetworkrows');
  const b = label(withMark(document.createElement('md-text-button'), '#i-sl-chevron-down'),
                  tr('meet.working'));
  b.className = 'meetworkgo';
  rows.dataset.who = who;
  rows.dataset.turn = String(nth);
  const fill = async () => {
    if (rows.children.length) return;
    const got = turnRows(await readRoom(socket, room, who), nth);
    // Everything except the answer itself, which is the line this box hangs under.
    const steps = got.filter(r => r.who !== 'assistant');
    rows.replaceChildren(...(steps.length ? steps.map(rowNode)
                                          : [cell('dnote', tr('meet.working_gone'))]));
  };
  rows.hidden = !workOpen.has(key);
  if (!rows.hidden) fill();       // rebuilt with it open: fill it again, from the kept answer
  b.onclick = () => whileItRuns(b, async () => {
    if (!rows.hidden) { rows.hidden = true; workOpen.delete(key); return; }
    await fill();
    rows.hidden = false;
    workOpen.add(key);
  });
  box.append(b, rows);
  return box;
}

// Putting a finished meeting back into session.
//
// One line and one button, in the room where it ended. The line is not optional: a round reopened
// without a reason is a round answering nothing, and the participants have all just said they had
// nothing to add — what changes their minds is what the person types here.
function reopenBox(m) {
  const box = cell('meetsay');
  const f = document.createElement('md-outlined-text-field');
  f.setAttribute('label', tr('meet.reopen_why'));
  f.setAttribute('type', 'textarea');
  f.setAttribute('rows', '2');
  const go = label(withMark(document.createElement('md-filled-tonal-button'), '#i-sl-play'),
                   tr('meet.reopen'));
  go.onclick = () => whileItRuns(go, async () => {
    const why = String(f.value || '').trim();
    if (!why) { says(tr('meet.reopen_needs_why')); return; }
    const r = await fetch('/meet-open',
      {method: 'POST', body: new URLSearchParams({id: m.id, why: why})});
    if (!r.ok) { says((await r.text()).trim().slice(0, 120)); return; }
    f.value = '';
    loadMeet();
  });
  box.append(f, go);
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
  // Stepping out. The meeting is driven by this console, not by the page — so leaving is exactly
  // navigating away, and the only thing missing was somewhere to press that says so. Beside the
  // other two because it is the third thing you might do with a room you are in.
  const leave = label(withMark(document.createElement('md-text-button'), '#i-sl-chevron-left'),
                      tr('meet.leave'));
  tip(leave, tr('meet.leave_why'));
  leave.onclick = () => { history.pushState({}, '', at(HREF.meet)); render(); };
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
  box.append(f, send, leave, stop);
  return box;
}

// The receipt, and the way to the work it just made.
//
// Sending a conclusion starts a turn in somebody else's session, and the two things a person wants
// next are opposite: watch it, or carry on reading the room they are still in. Answered as a link
// rather than a question — staying is what happens if nothing is pressed, and going is one press —
// because a dialog at every send would tax the common case (send three, then go to one) to serve
// the rare one. The link is only drawn when the socket is known: a participant may be a person, or
// a companion on a machine this console cannot address, and an anchor to nowhere is worse than
// plain text (the hand-off list learned this first).
function sentWithWayThere(m, who) {
  const box = cell('meetsent');
  box.append(cell('sentsaid', tr('meet.handed')));
  const sp = (m.speakers || []).find(x => x.name === who && x.socket && !x.person);
  if (!sp) return box;
  const go = el('a', tr('meet.go_there'));
  go.className = 'sentgo';
  go.setAttribute('href', at('?d=' + encodeURIComponent(sp.socket)));
  go.setAttribute('aria-label', tr('meet.go_there_named', {name: who}));
  box.append(go);
  return box;
}

// What each participant leaves with, and the one control that makes any of it happen.
function conclusions(m) {
  const box = cell('meettasks');
  box.append(sectionHead('meet.tasks', null, 3));
  for (const t of (m.tasks || [])) {
    const row = cell('meettask' + (t.what ? '' : ' nothing'));
    row.append(cell('meettaskwho', t.who));
    // Nothing to do is an outcome and is drawn as one. A participant missing from this list would
    // read as one nobody asked.
    row.append(cell('meettaskwhat', t.what || tr('meet.task_none')));
    if (t.what && meetHanded.has(m.id + '|' + t.who)) {
      row.append(sentWithWayThere(m, t.who));
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
        go.replaceWith(sentWithWayThere(m, t.who));
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
// Redrawn when the box they are drawn against changes size.
//
// The paths are absolute coordinates measured from getBoundingClientRect at the moment the map was
// built, inside an <svg width="100%"> — so a window resized afterwards scales every coordinate by
// whatever the width ratio happens to be. Measured 1920 → 1280: the same viewBox, squeezed to 81%,
// with one end of a wire inside a box and the other 50px short of its node.
let wiresWatch = null;
function watchWires(canvas, redraw) {
  if (typeof ResizeObserver !== 'function' || !canvas) return;
  if (wiresWatch) wiresWatch.disconnect();
  wiresWatch = new ResizeObserver(() => redraw());
  wiresWatch.observe(canvas);
}

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
    if (together) curve(svg, a, b, cls, frame.width);
    else around(svg, a, b, laneY(), cls, frame.width);
    drawn++;
  }
  return drawn;
}

// Two nodes in one box: a short curve out of the side that has room for it.
//
// It always left the trailing edge, which is fine while the boxes are half the canvas and wrong
// the moment they are all of it: on a phone every node's right edge IS the canvas edge, so the
// curve was drawn outside the frame and clipped there — measured at 390px, a path running to 387
// in a canvas 359 wide, which reads as grey stubs hanging off the cards.
function curve(svg, a, b, cls, width) {
  const y1 = a.y, y2 = b.y;
  const want = Math.max(16, Math.abs(y2 - y1) / 2);
  const right = (width || Infinity) - Math.max(a.r, b.r) - 4;
  if (right >= 12) {
    const out = Math.min(want, right);
    path(svg, 'M' + a.r + ' ' + y1 + ' C' + (a.r + out) + ' ' + y1 + ' ' +
               (b.r + out) + ' ' + y2 + ' ' + b.r + ' ' + y2, cls);
    return;
  }
  // No room on that side: leave and arrive on the leading edge, which is the same picture mirrored.
  const out = Math.min(want, Math.max(4, Math.min(a.l, b.l) - 4));
  path(svg, 'M' + a.l + ' ' + y1 + ' C' + (a.l - out) + ' ' + y1 + ' ' +
             (b.l - out) + ' ' + y2 + ' ' + b.l + ' ' + y2, cls);
}

// Two nodes in different boxes: out of the side, down into the lane, along it, and up.
function around(svg, a, b, y, cls, width) {
  // Inside the canvas at both ends: a machine box that reaches the frame left no room for the
  // 10px step out of it, and that end of the wire was drawn past the edge and cut off.
  const leave = Math.min(a.machine.r + 10, (width || a.machine.r + 10) - 4);
  const enter = Math.max(b.machine.l - 10, 4);
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
unloop(cardTabs, 'md-secondary-tab');
const filesToggle = document.getElementById('filesToggle');

// What is open, and which of them is showing. Paths, not contents: the file is fetched when its
// tab is chosen, so a tab left open for an hour shows what the file is now rather than what it was
// when somebody clicked it.
let openFiles = [];
// The companion the panes are drawn for, so opening one later can fill it without waiting for the
// next poll. Set where the page draws a companion, cleared when it leaves.
let lastDrawnFor = null;
let cardShows = 'facts';
// On a phone the workspace is a list you drill into, not a column of everything.
//
// "Compact and medium breakpoints: A single pane works best" — and list-detail's compact form is
// the list and the detail taking turns. So this says which of the three the workspace screen is
// showing: the tree, the git card, or whatever was opened out of them. Above the breakpoint it is
// not read at all; there both cards are in the pane and the open thing is in the slot beside them.
let wsShows = 'files';
// Which directories the reader has opened, so a redraw does not close the tree under them.
const openDirs = new Set();

// The attribute is written both ways by paneHandle, so this is the whole of the question.
// Whether the workspace is being shown at all, whichever thing is saying so.
//
// The handle, on a screen wide enough to have one; the tab, below that — where the handle is
// hidden and the panel IS the answer. Reading only the handle left the workspace tab loading
// nothing: the attribute still said "shut" from the last time somebody folded the pane on a
// desktop, and a tab that opens an empty box is worse than the handle it replaced.
const filesOpen = () => document.body.getAttribute('panel') === 'files' ||
  (!document.body.hasAttribute('panel') && document.body.getAttribute('files') !== 'shut');

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
// The way into a search, and what it found — not the search itself.
//
// The box and its two chips lived at the top of the tree, so a pane 18rem wide gave three of its
// rows to a control used a few times a day: a field, a chip for names, a chip for contents, above
// the forty filenames somebody is actually reading. It is a dialog now — the question is asked in
// one place, with room for the whole of it, and the pane goes back to being the tree.
//
// What stays here is the state of a search that is running: a line saying what was searched for
// and a way to drop it, because a tree showing eleven of four hundred files with nothing saying
// why is the worst version of this.
function findRow(a) {
  const box = cell('filefind');
  if (!findQ.trim()) {
    const open = label(withMark(document.createElement('md-text-button'), '#i-sl-magnifying-glass'),
                       tr('files.find'));
    open.onclick = () => askFind(a);
    box.append(open);
    return box;
  }
  const said = cell('findnow', tr(findIn === 'text' ? 'files.found_in_text' : 'files.found_in_names',
                                  {q: findQ.trim()}));
  const again = label(withMark(document.createElement('md-text-button'), '#i-sl-magnifying-glass'),
                      tr('files.find_again'));
  again.onclick = () => askFind(a);
  const clear = label(withMark(document.createElement('md-text-button'), '#i-sl-xmark'), tr('files.find_clear'));
  clear.onclick = () => { findQ = ''; loadTree(a); };
  box.append(said, cell('findacts', ''), again, clear);
  return box;
}

// askFind is the search itself: what to look for, and whether to look at names or at contents.
//
// Not typed-as-you-go any more. That was right when the box was in the pane and wrong for a dialog
// — a dialog that searched on every keystroke would redraw the thing behind it while somebody is
// still typing the word.
function askFind(a) {
  askLine({
    head: tr('files.find'), body: tr('files.find_who'), label: tr('files.find'), value: findQ,
    pick: {label: tr('files.find_where'), options: [tr('files.by_name'), tr('files.by_text')],
           value: tr(findIn === 'text' ? 'files.by_text' : 'files.by_name')},
    doIt: tr('files.find_go'), doMark: '#i-sl-magnifying-glass',
    go: (q, where) => {
      findQ = q;
      findIn = where === tr('files.by_text') ? 'text' : 'names';
      runFind(a);
    },
  });
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
  // The results replace the TREE, and only the tree.
  //
  // Written as replaceChildren on the whole pane, a search also deleted the git card and — on a
  // phone — the list row that is the only way to the git screen. Measured on the live console:
  // before a find the pane held [list, files, git]; after one it held [files], and it stayed that
  // way through every poll, because the poll re-runs the search. So looking for a file put the
  // branch, the changes and the way to them out of reach until somebody found Clear.
  const card = paneCard('files', shortPath(a.workdir || '') || tr('nav.files'), kids,
                        async () => { forgetTree(a); loadTree.busy = ''; await loadTree(a); });
  const was = filesEl.querySelector('.pane-files');
  if (was) was.replaceWith(card); else filesEl.replaceChildren(card);
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
// backToList is the way out of an open file on a phone: to the list it was opened from.
//
// Only where the list and the detail take turns. On a wide screen the tree is still on the left
// and a back button would be pointing at something already on the screen.
function backToList() {
  if (!onePane()) return null;
  const b = label(withMark(document.createElement('md-text-button'), '#i-sl-chevron-left'),
                  tr('nav.files'));
  b.className = 'fileback';
  // Named for what it does, like the panel's back row: the word on it is where it goes, and read
  // aloud that is the same name as the tab this screen is under.
  b.setAttribute('aria-label', tr('action.back_to', {name: tr('nav.files')}));
  b.onclick = () => toWorkspaceList('files');
  return b;
}

// waitingFor is the box a slow answer will land in: the room it needs, and a bar saying it is on
// its way. Indeterminate, because nothing here knows how long a directory walk on somebody else's
// machine takes — which is the case the guide reserves the indeterminate one for.
function waitingFor(key) {
  const box = cell('paneloading');
  const bar = document.createElement('md-linear-progress');
  bar.indeterminate = true;
  bar.setAttribute('aria-label', tr(key));
  box.append(bar, cell('filesnote', tr(key)));
  return box;
}


// loadTree(a) reads the workspace. loadTree(a, true) may use what it read a moment ago — for the
// walks that are only a redraw: arriving at the panel, coming back to the tab.
async function loadTree(a, kept) {
  if (!a || !filesOpen()) return;
  // A search is what the pane is showing, and it stays showing it.
  //
  // Everything that changes the workspace redraws this pane — opening a file, renaming one, the
  // refresh control — and each of those threw the results away and put the plain tree back. Live:
  // search, open the first hit, and the list you were working through is gone; the second hit is
  // now a directory that expands when you press where it was. Reported as "the second file does
  // not open".
  if (findQ.trim()) {
    await runFind(a);
    return;
  }
  // One walk at a time per companion. Arriving at this screen asks for the tree, and so does the
  // first frame that follows a second later — measured on the live console as two /files and two
  // /git for one arrival, both answering the same question about the same directory. The second is
  // not fresher for having been asked later; it is the same request, and on a big repository it is
  // the same walk.
  const key = (a.socket || '') + '|' + (a.peer || '');
  if (loadTree.busy === key) return;
  loadTree.busy = key;
  try {
    await walkTree(a, kept);
  } finally {
    loadTree.busy = '';
  }
}

async function walkTree(a, kept) {
  // A companion known only by gossip has no socket this console can open — the path in its row is a
  // path on ITS filesystem, and the fleet door carries work rather than file contents. Say so, and
  // say the way round it: a magi-web running there is a peer, and a peer's companions come through
  // its own console with their files intact. A row with a peer on it is NOT this case.
  if (a.elsewhere) {
    filesEl.replaceChildren(paneCard('files', tr('nav.files'), [cell('filesnote', tr('files.elsewhere'))]));
    return;
  }
  // Somewhere for the answer to appear, while it is on its way.
  //
  // The tree is a walk of a directory and the git card is a `git status`; on a workspace with a
  // few files both come back before the frame is drawn, and on a big repository — or a companion
  // reached over a link — they do not. Empty, the pane says "this companion has no files", which
  // is a different thing and the reader believes it. So the cards are drawn with the room they
  // will need and a bar saying it is coming.
  //
  // Only when there is nothing there yet. The guide is explicit that a loading indicator goes in
  // the empty place the new content will appear and does not cover what is already on screen —
  // and this runs on the poll, so covering would flash a bar over a tree somebody is reading.
  if (!filesEl.children.length) {
    filesEl.replaceChildren(paneCard('files', shortPath(a.workdir || ''), [waitingFor('files.reading')]),
                            paneCard('git', tr('git.section'), [waitingFor('git.reading')]));
  }
  treeAt.seen = [];
  // The git card is ASKED FOR HERE and awaited at the bottom, so the two halves of this pane are
  // two requests in flight rather than one after the other. They are answered on separate
  // connections (see the browse helper on the server): the daemon serves each connection in its
  // own goroutine, so this is real overlap and not two calls taking turns on one mutex — which is
  // what they did before, with the tree in hand and the pane still waiting on `git status`.
  //
  // Guarded, because a rejection now happens while nothing is awaiting it: an unhandled rejection
  // is a worse failure than the empty card the section already draws for a git it cannot reach.
  const gitAsking = gitSection(a).catch(() => [paneCard('git', tr('git.section'),
    [cell('filesnote', tr('git.unreachable'))])]);
  // Everything the tree will need, asked for in one go before any of it is drawn.
  await fetchDirs(a, wantedDirs(a, kept));
  const rows = treeAt(a, '.');
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
  // An empty directory says it is empty. Every other list on this page has an empty state and this
  // one did not: measured with the walk answering [], the card drew a heading, the find control,
  // and two hundred pixels of nothing — a console offering to search a list it never admitted was
  // empty.
  const branchRows = branches(a, '.', rows, 0);
  const tree = paneCard('files', shortPath(a.workdir || ''),
                        [findRow(a), ...(branchRows.length ? branchRows
                                         : [emptyState('files.empty', 'files.empty_how')])],
                        async () => { forgetTree(a); loadTree.busy = ''; await loadTree(a); });
  // ── the tree is drawn NOW, and the git card when it answers ───────────────
  //
  // The two used to be written together, so a tree that had arrived sat in hand while `git status`
  // ran — on a big repository that is the whole of a first paint. They are separate cards on the
  // screen and separate answers off the wire, so they are separate writes: the tree takes its
  // place as soon as it is read, and the git card replaces its own placeholder later WITHOUT
  // touching the tree beside it (a rebuilt tree is a shut menu and a row moving out from under the
  // pointer — the flicker the compare below exists to prevent).
  //
  // Two comparisons for two cards, for the same reason. One key over both meant a git answer that
  // changed nothing still counted as "something is different" for the tree, and vice versa.
  const treeNow = JSON.stringify([a.workdir, treeAt.seen, [...openDirs].sort(), cardShows, findQ, wsShows]);
  const held = (loadTree.gitNodes || []).filter(n => n.parentNode === filesEl);
  const gitHold = held.length ? held : [paneCard('git', tr('git.section'), [waitingFor('git.reading')])];
  // Not while a menu is open in it.
  //
  // The pane is rebuilt when the workspace changes, and in a workspace an agent is working in
  // something changes every few seconds — so a person who right-clicked a file and was reading
  // the menu had it vanish under the pointer. The menu is a child of the row that opened it, and
  // rebuilding the tree takes both. Same rule as the meeting room's "not while somebody is
  // typing": a redraw waits for the person, not the other way round.
  //
  // The next poll draws it: this drops the frame, it does not drop the change — loadTree.drawn is
  // left as it was, so the comparison still says there is something new to draw.
  const busyMenu = () => !!filesEl.querySelector('.showing');
  if ((treeNow !== loadTree.drawn || !filesEl.children.length) && !busyMenu()) {
    loadTree.drawn = treeNow;
    const pickNow = gitPickRow(a);
    filesEl.replaceChildren(...(pickNow ? [pickNow] : []), tree, ...gitHold);
    loadTree.pickNode = pickNow;
    loadTree.gitNodes = gitHold;
  }

  const git = await gitAsking;
  const gitNow = JSON.stringify([gitSection.raw, wsShows, onePane()]);
  if (gitNow === loadTree.gitDrawn && (loadTree.gitNodes || []).some(n => n.parentNode === filesEl)) return;
  if (busyMenu()) return;
  loadTree.gitDrawn = gitNow;
  // Only the git children move. The tree node stays exactly where it is, with whatever the reader
  // had open in it.
  const mine = (loadTree.gitNodes || []).filter(n => n.parentNode === filesEl);
  if (!mine.length) {
    const pickNow = gitPickRow(a);
    filesEl.replaceChildren(...(pickNow ? [pickNow] : []), tree, ...git);
    loadTree.pickNode = pickNow;
    loadTree.gitNodes = git;
    return;
  }
  for (const n of mine.slice(1)) n.remove();
  mine[0].replaceWith(...git);
  loadTree.gitNodes = git;
  // The compact pane's git row is drawn from the same answer, so it is replaced with it.
  const pickNow = gitPickRow(a);
  const old = loadTree.pickNode;
  if (old && old.parentNode === filesEl) {
    if (pickNow) old.replaceWith(pickNow); else old.remove();
  } else if (pickNow) {
    filesEl.prepend(pickNow);
  }
  loadTree.pickNode = pickNow;
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
function paneCard(key, title, kids, again) {
  // Git starts collapsed until someone opens it. Where the tree and git stack (the desk column, a
  // phone in landscape) an expanded git card of a busy repo pushed the tree — the thing a person
  // came to the workspace for — down to a strip; the tree leads, and git is a heading you open. An
  // explicit choice, either way, is remembered. (On a compact PHONE git is not a card here at all
  // but a summary row above the tree, so this only bears on the stacked layouts.)
  const stored = localStorage.getItem('pane.' + key);
  const shut = stored === 'shut' || (stored === null && key === 'git');
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
  const row = cell('panerow');
  row.append(head);
  // Read it again, now. The listings are kept for a few seconds so that four redraws in one second
  // are one walk — which is right until somebody has just written a file in another window and is
  // looking at a tree that does not have it. Then the answer is not "wait", it is a control.
  //
  // Beside the title rather than inside the button that folds the card: a control inside a control
  // is a press that does one of two things depending on where in it you land.
  if (again) {
    const b = document.createElement('md-icon-button');
    b.className = 'paneagain';
    const m = iconOr('#i-sl-arrows-rotate', '⟳');
    if (m) b.append(m);
    b.setAttribute('aria-label', tr('files.again'));
    tip(b, tr('files.again'));
    b.onclick = e => { e.stopPropagation(); whileItRuns(b, again); };
    row.append(b);
  }
  const body = cell('panebody');
  body.append(...kids);
  card.append(row, body);
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
  // A card that says why, rather than a card that is not there.
  //
  // Returning nothing was right about "there is no git here" and wrong about everything the reader
  // then thinks: a companion whose workspace is not a checkout, one whose daemon has just gone, and
  // one where the pane happens to be folded all looked identical — the git card simply absent, with
  // the report coming back as "깃은 아예 보이지도 않네". Two of those three are facts worth saying.
  if (!g) {
    return [paneCard('git', tr('git.section'), [cell('filesnote', tr('git.unreachable'))])];
  }
  if (!g.repo) {
    return [paneCard('git', tr('git.section'), [cell('filesnote', tr('git.not_a_repo'))])];
  }
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
  if (may('shell')) box.append(commitRow(a, staged.length));
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
  const acts = gitActs(a, c);
  line.append(row, acts);
  // The right button opens the same menu, the way the tree rows do — a changed-file row is where
  // a person reaches for stage/unstage/discard first, and it had no secondary click at all.
  line.addEventListener('contextmenu', ev => {
    const opener = acts.children && acts.children[0];
    if (!opener || !opener.onclick) return;
    ev.preventDefault();
    opener.onclick(ev);
  });
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
  // A pull request is the end of the same errand as pushing, so it sits with push rather than in a
  // menu somewhere else. One box: the first line is the title and the rest is the body, which is
  // the shape everybody already writes a commit in — and gh reads them the same way round.
  // Up into the slot, where what is being sent can be read. A request written without its own
  // commits in front of it is the request that says "update".
  act('git.pr', '#i-sl-share-from-square', () => openPR(a), g.repo);
  act('git.new_branch', '#i-sl-plus', () => {
    askLine({head: tr('git.new_branch'), body: tr('git.new_branch_who'), label: tr('git.branch'),
             doIt: tr('git.new_branch'), doMark: '#i-sl-plus',
             go: name => gitRun(a, 'new-branch', {message: name})});
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
  // Named by its row, like the skills screen's per-item actions: a menu opened by one of five
  // identical "More" buttons in a list is "More" five times to a screen reader with no way to tell
  // which file it acts on. The tooltip stays the short word; the accessible name carries the target.
  open.setAttribute('aria-label', tr('files.more_named', {name: baseName(c.path)}));
  tip(open, tr('files.more'));
  const menu = popMenu(document.createElement('md-menu'), box);
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
  open.onclick = ev => { ev.stopPropagation(); atPointer(menu, open, ev); menu.open = !menu.open; };
  box.append(open, menu);
  return box;
}

// One button, and the message is written in a dialog.
//
// The box used to be in the card: an 18rem column holding a one-line field, which is where a
// commit message goes to become "wip". A message has a subject and a reason, and the room to write
// one has to be somewhere — so pressing the button opens a dialog with a box six lines tall, and
// the card keeps a control instead of a form.
//
// Always drawn, disabled when there is nothing staged. Hidden, the button appeared and disappeared
// as files were staged, which is a control that moves; disabled, it says the thing the screen
// knows — there is nothing to commit yet.
function commitRow(a, staged) {
  const box = cell('gitcommit');
  // Tonal, not filled. The guide keeps filled for the action that ends the page's own flow and
  // says to have one — measured on this screen there were three: this, the workbench's, and the
  // composer's Send, which is the one that is always there and always the page's. Tonal is what
  // the guide calls the step below: an action that needs more weight than an outline and is not
  // the page's last word.
  const go = label(withMark(document.createElement('md-filled-tonal-button'), '#i-sl-check'),
                   tr('git.commit'));
  go.disabled = !staged;
  // Two "Commit" buttons are on screen once the workbench is open — this one opens the review,
  // the workbench's one writes the commit. Same word, different act, so they carry different names
  // for a reader who cannot see which is which.
  go.setAttribute('aria-label', tr('git.commit_open'));
  tip(go, staged ? tr('git.commit_who') : tr('git.nothing_staged'));
  // Up into the slot, where there is room to read what is being committed. A message written
  // without the diff in front of it is the message this console kept getting.
  go.onclick = () => { commitPick = ''; openCommit(a); };
  box.append(go);
  return box;
}

// Literal keys in a lookup, for the reason every other one on this page is: a key built by
// concatenation is invisible to the check that finds phrases nobody asks for.
const GIT_KIND = {staged: 'git.staged', unstaged: 'git.unstaged', both: 'git.both',
                  untracked: 'git.untracked', conflict: 'git.conflict'};

// What each directory held, and when it was read.
//
// A walk asks for the root and for every directory the reader has opened — six requests for a tree
// with five folders open. Some of those walks are asking because something CHANGED, and some are
// asking only because the page is being drawn again: arriving at the workspace panel, coming back
// to the tab, a redraw for an unrelated reason. The second kind is the same answer again.
//
// So the caller says which it is. A walk that is following a change reads; a walk that is only
// redrawing may use what was read, up to treeKept. The default is to READ — a cache that has to be
// opted out of is one that goes stale in the place nobody thought about.
//
// Cleared outright by a mutation this console made (a save, a new file, a rename) and by the
// refresh control, because those are the moments a kept listing is known to be wrong rather than
// merely old.
const treeSeen = new Map();
const treeKept = 10000;

function forgetTree(a) {
  if (!a) { treeSeen.clear(); return; }
  const from = (a.socket || '') + '|' + (a.peer || '') + '|';
  for (const k of [...treeSeen.keys()]) if (k.startsWith(from)) treeSeen.delete(k);
}

// fetchDirs asks for a set of directories at once and writes each answer into the cache.
//
// One request and one acquisition of the companion's connection for the whole open subtree. It
// used to be one of each PER DIRECTORY, awaited in turn: eight open directories were eight round
// trips, each of which could queue behind a running turn (measured in withClient: 2.7s against
// 0.6ms idle). The listings were never the cost — one os.ReadDir apiece — the waiting was.
async function fetchDirs(a, paths) {
  if (!paths.length) return;
  const q = paths.map(p => '&path=' + encodeURIComponent(p)).join('');
  const got = await fetchOne('/files' + qFor(a) + q);
  const dirs = got && got.dirs && typeof got.dirs === 'object' ? got.dirs : null;
  if (!dirs) return;
  for (const p of paths) {
    const rows = dirs[p];
    if (!Array.isArray(rows)) continue;
    const line = rows.map(e => e.name + (e.isDir ? '/' : '')).join(',');
    treeSeen.set((a.socket || '') + '|' + (a.peer || '') + '|' + p, {rows, line, at: Date.now()});
  }
}

// treeAt answers from the cache fetchDirs filled. It is no longer a request of its own: a walk
// asks for everything it will need before it starts, so the drawing below never waits.
function treeAt(a, path) {
  const had = treeSeen.get((a.socket || '') + '|' + (a.peer || '') + '|' + path);
  if (!had) return null;
  // Recorded on every walk, because the comparison in loadTree is over what THIS walk saw: a
  // directory left out of it would read as a directory that had emptied.
  treeAt.seen.push(path + ':' + had.line);
  return had.rows;
}
treeAt.seen = [];

// wantedDirs is the set the next walk will need: the root, plus every directory the reader has
// opened UNDER a directory that is itself open — asked breadth-first, because a child only counts
// when its parent is on the screen. Cached directories are left out unless the walk is a fresh
// one; `kept` is the same "may use what it read a moment ago" the walk carries.
function wantedDirs(a, kept) {
  const fresh = p => {
    const had = treeSeen.get((a.socket || '') + '|' + (a.peer || '') + '|' + p);
    return kept && had && Date.now() - had.at < treeKept;
  };
  const want = fresh('.') ? [] : ['.'];
  // openDirs holds paths, so the parent chain can be checked without walking the tree first.
  for (const p of openDirs) {
    const parts = String(p).split('/');
    let ok = true;
    for (let i = 1; i < parts.length; i++) {
      if (!openDirs.has(parts.slice(0, i).join('/'))) { ok = false; break; }
    }
    if (ok && !fresh(p)) want.push(p);
  }
  return want;
}

// gitPickRow is the compact pane's one row for git. It is drawn from the git ANSWER, so it is
// rebuilt with the git card rather than with the tree.
function gitPickRow(a) {
  const pick = onePane() ? cell('panelist') : null;
  if (pick && wsShows === 'git') {
    pick.replaceChildren();
    pick.append(panelBack(tr('nav.files_short'), () => toWorkspaceList('files')).firstChild);
    pick.className = 'panelback';
  } else if (pick) {
    const g = (() => { try { return JSON.parse(gitSection.raw || 'null'); } catch { return null; } })();
    const row = document.createElement('button');
    row.type = 'button';
    row.className = 'panelrow state';
    // A leading mark for what is behind the press — the workspace's version history — so the row
    // says what it opens rather than only naming it. The word "Git" carries the metaphor for a
    // reader who knows it; the mark and the chevron carry it for one who reads the screen as shapes.
    { const m = iconOr('#i-sl-clock-rotate-left', '', 'panelmark'); if (m) row.append(m); }
    row.append(cell('panelword', tr('git.section')));
    if (g && g.repo) {
      row.append(cell('panelcount',
        // The branch, or the head it is detached at — the same fallback the card below uses. Written
        // as `[g.branch, …].filter(Boolean)`, a detached HEAD (a rebase, a bisect) simply dropped
        // out and the row read "4 changed", with two of those four being conflicts.
        [g.branch || (g.head ? '@' + g.head : ''),
         (g.changes || []).length ? tr('git.n_changed', {n: (g.changes || []).length}) : '']
          .filter(Boolean).join(' · ')));
    }
    const mark = iconOr('#i-sl-chevron-right', '›', 'panelgo');
    if (mark) row.append(mark);
    row.onclick = () => toWorkspaceList('git');
    pick.append(row);
  }
  return pick;
}

// branches renders one directory, and the ones the reader has opened under it.
//
// Depth is drawn as an indent rather than as a nested box: a tree of boxes at 18rem runs out of
// width four levels down, and the indent is what every file tree has used since they existed.
function branches(a, dir, rows, depth) {
  const out = [];
  for (const e of rows) {
    const path = dir === '.' ? e.name : dir + '/' + e.name;
    out.push(treeRow(a, e, path, depth));
    if (e.isDir && openDirs.has(path)) {
      const kids = treeAt(a, path);
      if (kids) out.push(...branches(a, path, kids, depth + 1));
    }
  }
  return out;
}

// popMenu puts a menu in the top layer, where a menu belongs.
//
// A menu positioned the default way is laid out inside the page, so it is clipped by every
// scrolling ancestor and offset against the nearest positioned one. Measured in the file pane: six
// items 56dp tall opening 40px above the pane's bottom edge, the first one already 16px past it
// and not clickable — elementFromPoint at its middle answered the pane — and the whole thing drawn
// 234px to the left of the button that opened it, because it was placed against the pane rather
// than against its anchor.
//
// The popover positioning takes it out of the page's boxes entirely. Feature-detected rather than
// assumed: where the API is missing, fixed positioning still escapes the clipping, which is the
// half that matters most.
// atPointer puts the menu where the pointer is, when a pointer is what opened it.
//
// A menu anchors to a control, which is right when the control was pressed and wrong when the
// right button was used somewhere along a row: the menu appeared beside a button at the end of the
// line rather than under the cursor, which is where everything else on a desktop puts it. The
// offsets are measured from the anchor, so this is the distance from the anchor to the pointer.
// A press with no coordinates — a keyboard, a synthetic click — leaves the anchor alone.
function atPointer(menu, anchor, ev) {
  const x = ev && typeof ev.clientX === 'number' ? ev.clientX : 0;
  const y = ev && typeof ev.clientY === 'number' ? ev.clientY : 0;
  if (!x && !y) { menu.xOffset = 0; menu.yOffset = 0; return; }
  if (!anchor.getBoundingClientRect) return;
  const r = anchor.getBoundingClientRect();
  menu.xOffset = Math.round(x - r.left);
  menu.yOffset = Math.round(y - r.bottom);
}

function popMenu(menu, holder) {
  // Read off the constructor rather than off a global that may not be there: this script also runs
  // in a fake DOM, where there is no HTMLElement at all, and a menu that throws while being built
  // takes the whole pane with it.
  const el = typeof HTMLElement === 'function' ? HTMLElement.prototype : null;
  menu.setAttribute('positioning', el && typeof el.showPopover === 'function' ? 'popover' : 'fixed');
  // The button that opens this lives in a box shown only while its row is hovered, and the menu is
  // that box's child however it is positioned — so walking the pointer over to the menu took the
  // pointer off the row, hid the box, and took the menu with it. It stays shown while it is open.
  if (holder && menu.addEventListener) {
    menu.addEventListener('opening', () => holder.classList.add('showing'));
    menu.addEventListener('closed', () => holder.classList.remove('showing'));
  }
  return menu;
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
  // Named by its row (see gitActs): "More" repeated down a tree of forty names tells a screen
  // reader nothing about which one it opens. The tooltip keeps the short word.
  open.setAttribute('aria-label', tr('files.more_named', {name: baseName(path)}));
  tip(open, tr('files.more'));
  const menu = popMenu(document.createElement('md-menu'), box);
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
    a.socket || '', a.peer || '').then(why => {
      if (why) return why;
      // A directory this console just changed is one whose kept listing is wrong, not merely old.
      forgetTree(a);
      loadTree(a);
      return why;
    });
  const under = e.isDir ? path + '/' : (path.includes('/') ? path.slice(0, path.lastIndexOf('/') + 1) : '');
  item('files.new_file', () => {
    askLine({head: tr('files.new_file'), body: tr('files.new_file_who'), label: tr('files.path'),
             value: under, doIt: tr('files.new_file'), doMark: '#i-sl-plus',
             go: name => send('new-file', {path: name}).then(() => openFile(a, name))});
  }, '#i-sl-plus');
  item('files.new_dir', () => {
    askLine({head: tr('files.new_dir'), body: tr('files.new_dir_who'), label: tr('files.path'),
             value: under, doIt: tr('files.new_dir'), doMark: '#i-sl-plus',
             go: name => send('new-dir', {path: name})});
  }, '#i-sl-plus');
  item('files.rename', () => {
    askLine({head: tr('files.rename'), body: tr('files.rename_who'), label: tr('files.path'),
             value: path, doIt: tr('files.rename'), doMark: '#i-sl-pen-to-square',
             go: to => { if (to !== path) send('rename', {to}); }});
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
  open.onclick = ev => { ev.stopPropagation(); atPointer(menu, open, ev); menu.open = !menu.open; };
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
      // Opening a folder is one directory to read, not the whole tree again: everything else on
      // screen is what it was a moment ago. Measured before this — two folders opened cost five
      // requests, three of them for directories already drawn.
      loadTree(a, true);
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
  toWorkspacePanel();
  drawCardTabs(a);
  fileWaiting(tabName(path) + ' ±');
  const got = await fetchOne('/diff' + qFor(a) + '&path=' + encodeURIComponent(path) +
                             '&which=' + encodeURIComponent(which || ''));
  if (cardShows !== key) return;
  drawDiff(path, which, got && typeof got.text === 'string' ? got.text : '');
  loadTree(a);
}

// The pull request workbench: which branch onto which, what is on it, and the request itself.
//
// The same shape as the commit workbench below, in the same slot, for the same reason: a request
// is written while reading what it carries, and what it carries is a branch — its commits and the
// whole difference against the base. The dialog it replaces was a title and a body over nothing,
// which is how "update" gets written twice a day.
const PR = 'pr:';
let prDraft = '';   // the request as it is being typed, kept across redraws
let prRules = '';   // the PR draft rules as edited, kept across redraws (empty = not loaded yet)
let prRulesOpen = false;

async function openPR(a) {
  if (!openFiles.includes(PR)) openFiles.push(PR);
  cardShows = PR;
  toWorkspacePanel();
  drawCardTabs(a);
  fileWaiting(tr('git.pr'));
  const st = await fetchOne('/pr' + qFor(a));
  if (cardShows !== PR) return;
  // null is "this console got no answer" and {} is "a companion that answered with nothing".
  // Kept apart, because drawPR turns an empty answer into "this workspace is not a checkout" —
  // which is a sentence, and a wrong one: a live run showed it for a companion running an older
  // build that had never heard of the question.
  drawPR(a, st === null ? {unreachable: true} : st);
}

function drawPR(a, st) {
  const bar = cell('filebar');
  { const back = backToList(); if (back) bar.append(back); }
  bar.append(cell('filedir', tr('git.pr') + (st.branch && st.base
    ? '  ·  ' + st.branch + ' → ' + st.base : '')));
  const box = cell('commitbox');

  // Left: what is going up. The commits, newest first, which is the order somebody writing the
  // request reads them in.
  const list = cell('commitfiles');
  if (st.unreachable) list.append(cell('filesnote', tr('pr.unreachable')));
  else if (!st.repo) list.append(cell('filesnote', tr('git.not_a_repo')));
  else if (!st.base) list.append(cell('filesnote', tr('pr.no_base')));
  else if (!(st.commits || []).length) list.append(cell('filesnote', tr('pr.nothing_to_send')));
  for (const c of (st.commits || [])) {
    const row = cell('treerow state');
    row.append(cell('gitkind', c.sha), cell('treename', c.subject));
    if (c.when) tip(row, c.when);
    list.append(row);
  }
  box.append(list);

  // Right: the whole difference against the base, which is what a reviewer will see.
  const diff = document.createElement('pre');
  diff.className = 'filecode diffbody commitdiff';
  fillDiff(diff, st.diff || '');
  box.append(diff);

  const foot = cell('commitfoot');
  const msg = document.createElement('md-outlined-text-field');
  msg.setAttribute('type', 'textarea');
  msg.setAttribute('rows', '5');
  msg.setAttribute('label', tr('git.pr_text'));
  msg.className = 'commitmsg';
  msg.value = prDraft;
  msg.addEventListener('input', () => { prDraft = msg.value; });
  // The PR house rules, editable inline like the commit card: this draft as typed, savable as the
  // default for whoever may configure.
  const rulesField = document.createElement('md-outlined-text-field');
  rulesField.setAttribute('type', 'textarea');
  rulesField.setAttribute('rows', '3');
  rulesField.setAttribute('label', tr('git.rules'));
  rulesField.className = 'commitrules';
  rulesField.value = prRules;
  rulesField.addEventListener('input', () => { prRules = rulesField.value; });
  const rulesWrap = cell('commitruleswrap');
  rulesWrap.hidden = !prRulesOpen;
  const rulesRow = cell('commitrulesrow');
  rulesRow.append(rulesField);
  if (may('configure')) {
    const rulesSave = label(withMark(document.createElement('md-text-button'), '#i-sl-floppy-disk'),
                            tr('git.rules_save'));
    rulesSave.onclick = () => whileItRuns(rulesSave, async () => {
      const body = new URLSearchParams({prTemplate: rulesField.value || ''});
      if (!a.socket) body.set('tier', 'global');
      const why = await post('/autocomplete', body, a.socket || '', a.peer || '');
      if (!why) says(tr('git.rules_saved'));
    });
    rulesRow.append(rulesSave);
  }
  rulesWrap.append(rulesRow);
  if (!prRules) {
    fetchOne('/autocomplete' + (a.socket ? qFor(a) : '?tier=global')).then(got => {
      if (got && !prRules) { prRules = got.prTemplate || ''; rulesField.value = prRules; }
    });
  }
  const acts = cell('commitacts');
  const rulesToggle = label(withMark(document.createElement('md-text-button'), '#i-sl-sliders'),
                            tr('git.rules'));
  rulesToggle.setAttribute('aria-expanded', String(!rulesWrap.hidden));
  rulesToggle.onclick = () => {
    prRulesOpen = rulesWrap.hidden;
    rulesWrap.hidden = !rulesWrap.hidden;
    rulesToggle.setAttribute('aria-expanded', String(prRulesOpen));
  };
  const draft = label(withMark(document.createElement('md-text-button'), '#i-sl-wand-magic-sparkles'),
                      tr('git.draft'));
  draft.onclick = () => whileItRuns(draft, async () => {
    const said = await postText('/pr-msg' + qFor(a), new URLSearchParams({rules: rulesField.value || ''}));
    if (said) { prDraft = said; msg.value = said; if (msg.focus) msg.focus(); }
  });
  const go = label(withMark(document.createElement('md-filled-tonal-button'), '#i-sl-share-from-square'),
                   tr('git.pr'));
  go.disabled = !!st.unreachable || !st.repo || !st.base || !(st.commits || []).length;
  go.onclick = () => whileItRuns(go, async () => {
    const text = String(msg.value || '').trim();
    if (!text) { says(tr('pr.needs_words')); return; }
    // The first line is the title and the rest is the body — the way a commit is written and the
    // way gh reads it.
    const at = text.indexOf('\n');
    const title = at < 0 ? text : text.slice(0, at).trim();
    const body = at < 0 ? '' : text.slice(at + 1).trim();
    const url = await postText('/git-pr' + qFor(a), new URLSearchParams({title: title, body: body}));
    if (!url) return;                      // the refusal is already on the screen
    prDraft = '';
    says(url);
    openFiles = openFiles.filter(p => p !== PR);
    cardShows = openFiles[openFiles.length - 1] || 'facts';
    drawCardTabs(a);
    if (cardShows === 'facts') showCard();
    loadTree(a);
  });
  acts.append(rulesToggle, draft, go);
  // What pressing it will do, said before it is pressed: a branch nobody has pushed is pushed by
  // this, which is a fact about the remote somebody should meet in advance.
  if (st.repo && st.base && !st.pushed) foot.append(cell('filesnote', tr('pr.will_push')));
  foot.append(msg, rulesWrap, acts);
  box.append(foot);
  fileViewEl.classList.add('commitmode');
  fileViewEl.replaceChildren(bar, box);
  showCard();
}

// The commit workbench: what is staged, what changed in it, and the message.
//
// Not a dialog. A commit message is written while reading a diff, and a diff does not fit in a
// box 560dp wide — which is what the guide caps a dialog at, and the reason the first attempt at
// this read as a form with a text box floating in it. The slot above the transcript is already the
// place this console puts a file, a diff and a card, and it is as wide as the conversation: the
// list of staged files on the left, the diff of whichever one is picked on the right, the message
// under both.
const COMMIT = 'commit:';
let commitPick = '';     // which staged file is being read; '' is everything at once
let commitDraft = '';    // the message as it is being typed, kept across redraws
let commitRules = '';    // the commit draft rules as edited, kept across redraws (empty = not loaded yet)
let commitRulesOpen = false;

async function openCommit(a) {
  if (!openFiles.includes(COMMIT)) openFiles.push(COMMIT);
  cardShows = COMMIT;
  toWorkspacePanel();
  drawCardTabs(a);
  fileWaiting(tr('git.commit'));
  const g = await fetchOne('/git' + qFor(a));
  if (cardShows !== COMMIT) return;
  drawCommit(a, g || {});
  // The diff comes after the shape, so the list is readable while git is still being asked.
  const text = await fetchOne('/diff' + qFor(a) + '&path=' + encodeURIComponent(commitPick) +
                              '&which=staged');
  if (cardShows !== COMMIT) return;
  const into = fileViewEl.querySelector('.commitdiff');
  if (into) fillDiff(into, text && typeof text.text === 'string' ? text.text : '');
}

// fillDiff paints a unified diff into a box: + is added, - is removed, @@ is where, and everything
// else is context. The same reading drawDiff does, factored out because the workbench needs it in
// a box of its own rather than in the whole pane.
// The class one diff line belongs to. A unified diff's first characters carry the meaning — but
// the file headers begin with the same +/- as content, and painting `+++ b/path` as an added line
// and `--- a/path` as a removed one meant, on a real multi-file diff, that EVERY red line was a
// header and 12% of the "added" lines were too. The headers get their own quiet class instead.
function diffLineClass(line) {
  if (/^(diff --git |index |--- |\+\+\+ |old mode |new mode |deleted file |new file |rename |similarity |copy |Binary )/.test(line)) return 'dl dfile';
  const c = line[0];
  return 'dl' + (c === '+' ? ' add' : c === '-' ? ' cut' : c === '@' ? ' at' : '');
}

function fillDiff(into, text) {
  into.replaceChildren();
  if (!String(text).trim()) {
    into.append(cell('filesnote', tr('diff.same')));
    return;
  }
  for (const line of String(text).split('\n')) {
    const row = document.createElement('span');
    row.className = diffLineClass(line);
    row.textContent = line + '\n';
    into.append(row);
  }
}

function drawCommit(a, g) {
  const staged = (g.changes || []).filter(c => c.kind === 'staged' || c.kind === 'both');
  const bar = cell('filebar');
  { const back = backToList(); if (back) bar.append(back); }
  bar.append(cell('filedir', tr('git.commit') + (g.branch ? '  ·  ' + g.branch : '')));
  const box = cell('commitbox');

  // Left: what is going in. Every one of them, and "all of it" at the top — reading the whole
  // staged diff at once is how somebody writes the subject line, and reading one file is how they
  // check a detail in it.
  const list = cell('commitfiles');
  const pick = (path, words, kind) => {
    const b = document.createElement('button');
    b.className = 'treerow state' + (commitPick === path ? ' now' : '');
    b.type = 'button';
    if (kind) b.append(cell('gitkind', tr(kind)));
    b.append(cell('treename', words));
    b.onclick = () => { commitPick = path; openCommit(a); };
    list.append(b);
    return b;
  };
  pick('', tr('git.all_staged', {n: staged.length}), '');
  for (const c of staged) pick(c.path, c.path, 'git.kind_staged');
  box.append(list);

  // Right: the diff of whatever is picked.
  const diff = document.createElement('pre');
  diff.className = 'filecode diffbody commitdiff';
  diff.append(cell('filesnote', tr('diff.reading')));
  box.append(diff);

  // Under both: the message, and the two things that can be done with it.
  const foot = cell('commitfoot');
  const msg = document.createElement('md-outlined-text-field');
  msg.setAttribute('type', 'textarea');
  // Four lines: a subject, a blank line and two of body is what most messages are, and the box
  // has to leave room for the diff above it in a card that is not always tall.
  msg.setAttribute('rows', '4');
  msg.setAttribute('label', tr('git.message'));
  msg.className = 'commitmsg';
  msg.value = commitDraft;
  msg.addEventListener('input', () => { commitDraft = msg.value; });
  // The house rules the draft follows, editable right here: pre-filled from the saved [templates]
  // commit, used for THIS draft as typed, and — for somebody who may configure — savable as the new
  // default. Hidden until asked for, so the common case (just draft) stays one button.
  const rulesField = document.createElement('md-outlined-text-field');
  rulesField.setAttribute('type', 'textarea');
  rulesField.setAttribute('rows', '3');
  rulesField.setAttribute('label', tr('git.rules'));
  rulesField.className = 'commitrules';
  rulesField.value = commitRules;   // kept across a card rebuild (picking a file to inspect rebuilds it)
  rulesField.addEventListener('input', () => { commitRules = rulesField.value; });
  const rulesWrap = cell('commitruleswrap');
  rulesWrap.hidden = !commitRulesOpen;
  const rulesRow = cell('commitrulesrow');
  rulesRow.append(rulesField);
  if (may('configure')) {
    const rulesSave = label(withMark(document.createElement('md-text-button'), '#i-sl-floppy-disk'),
                            tr('git.rules_save'));
    rulesSave.onclick = () => whileItRuns(rulesSave, async () => {
      const body = new URLSearchParams({commitTemplate: rulesField.value || ''});
      if (!a.socket) body.set('tier', 'global');
      const why = await post('/autocomplete', body, a.socket || '', a.peer || '');
      if (!why) says(tr('git.rules_saved'));
    });
    rulesRow.append(rulesSave);
  }
  rulesWrap.append(rulesRow);
  // The saved template pre-fills the field the FIRST time only — never over an edit the person is in
  // the middle of (a rebuild would otherwise wipe it and put the default back).
  if (!commitRules) {
    fetchOne('/autocomplete' + (a.socket ? qFor(a) : '?tier=global')).then(got => {
      if (got && !commitRules) { commitRules = got.commitTemplate || ''; rulesField.value = commitRules; }
    });
  }
  const acts = cell('commitacts');
  const rulesToggle = label(withMark(document.createElement('md-text-button'), '#i-sl-sliders'),
                            tr('git.rules'));
  rulesToggle.setAttribute('aria-expanded', String(!rulesWrap.hidden));
  rulesToggle.onclick = () => {
    commitRulesOpen = rulesWrap.hidden;
    rulesWrap.hidden = !rulesWrap.hidden;
    rulesToggle.setAttribute('aria-expanded', String(commitRulesOpen));
  };
  const draft = label(withMark(document.createElement('md-text-button'), '#i-sl-wand-magic-sparkles'),
                      tr('git.draft'));
  draft.onclick = () => whileItRuns(draft, async () => {
    const said = await postText('/git-msg' + qFor(a), new URLSearchParams({rules: rulesField.value || ''}));
    if (said) { commitDraft = said; msg.value = said; if (msg.focus) msg.focus(); }
  });
  const go = label(withMark(document.createElement('md-filled-tonal-button'), '#i-sl-check'),
                   tr('git.commit'));
  go.disabled = !staged.length;
  go.setAttribute('aria-label', tr('git.commit_do'));
  go.onclick = () => whileItRuns(go, async () => {
    const text = String(msg.value || '').trim();
    if (!text) { says(tr('git.need_message')); return; }
    const why = await post('/git-do', new URLSearchParams({do: 'commit', message: text}),
                           a.socket || '', a.peer || '');
    if (why) return;
    // Committed: the message is spent and the workbench has nothing left to show.
    commitDraft = '';
    commitPick = '';
    openFiles = openFiles.filter(p => p !== COMMIT);
    cardShows = openFiles[openFiles.length - 1] || 'facts';
    drawCardTabs(a);
    if (cardShows === 'facts') showCard();
    loadTree(a);
  });
  acts.append(rulesToggle, draft, go);
  foot.append(msg, rulesWrap, acts);
  box.append(foot);
  // The card is taller in this mode and holds its own scrolling boxes — see the rule in the stylesheet.
  fileViewEl.classList.add('commitmode');
  fileViewEl.replaceChildren(bar, box);
  showCard();
}

// The diff, coloured by what each line does and nothing else.
//
// No parsing beyond the first character of a line, which is the whole of what a unified diff
// promises: + is added, - is removed, @@ is where, and anything else is context. A renderer that
// tried to understand more would be a second implementation of the thing git just did.
function drawDiff(path, which, text) {
  const bar = cell('filebar');
  { const back = backToList(); if (back) bar.append(back); }
  bar.append(cell('filedir', path + (which ? '  ·  ' + tr(DIFF_WHICH[which] || 'diff.unstaged')
                                            : '  ·  ' + tr('diff.unstaged'))));
  const body = document.createElement('pre');
  body.className = 'filecode diffbody';
  const lines = String(text).split('\n');
  if (!String(text).trim()) {
    fileViewEl.classList.remove('commitmode');
    fileViewEl.replaceChildren(bar, cell('filesnote', tr('diff.same')));
    showCard();
    return;
  }
  for (const line of lines) {
    const row = document.createElement('span');
    row.className = diffLineClass(line);
    row.textContent = line + '\n';
    body.append(row);
  }
  fileViewEl.classList.remove('commitmode');
  // In the same scroller the file viewer uses. Appended bare, the <pre> made the CARD the thing
  // that scrolls — measured: the path bar and the way back slid 220px off the left edge while the
  // reader was still in the first hunk, because a bar that is sticky in a box that does not scroll
  // pins against nothing.
  const wrap = cell('filebody diffscroll');
  wrap.append(body);
  fileViewEl.replaceChildren(bar, wrap);
  showCard();
}

const DIFF_WHICH = {staged: 'diff.staged', untracked: 'diff.untracked', '': 'diff.unstaged'};

// openFile puts a file in the slot the facts card is in, behind a tab of its own.
// The file slot, waiting. The four openers below all move the TAB first and fetch after, so
// between the press and the answer the slot still held the file somebody had just left — with a
// working Edit button on it. Pressing it there opened the OLD file under the NEW tab's name, and
// the next Save wrote to a file nobody had chosen (reported from the console, 2026-08-17: a slow
// read, a second file picked, an edit that landed in the first). The stale-response guard already
// stopped a late answer from painting over a newer choice; what was missing is that the CHOICE
// itself must clear the slot. It says which file it is waiting for, because on a slow read the
// name is the only thing on the screen.
function fileWaiting(name) {
  fileViewEl.classList.remove('commitmode');
  const bar = cell('filebar');
  { const back = backToList(); if (back) bar.append(back); }
  bar.append(cell('filedir', name));
  fileViewEl.replaceChildren(bar, waitingFor('loading.file'));
  showCard();
}

async function openFile(a, path) {
  if (!openFiles.includes(path)) openFiles.push(path);
  cardShows = path;
  toWorkspacePanel();
  drawCardTabs(a);
  fileWaiting(path);
  const got = await fetchOne('/file' + qFor(a) + '&path=' + encodeURIComponent(path));
  if (cardShows !== path) return;            // somebody moved on while it was fetching
  // A file that could not be read is not a file with one sentence in it.
  //
  // The failure text used to be passed through the same argument as the contents, so the viewer
  // could not tell them apart: Edit was offered, pressing it put "This companion did not answer
  // for its workspace." into the editor as line 1, and Save would have written that sentence over
  // the file. Marked as a failure instead, and the viewer offers nothing to do with it.
  // Three states, three answers: a file, a file with nothing in it, and a read that failed. The
  // middle one used to draw a 19px grey box that read the same as the other two.
  const text = got && typeof got.text === 'string' ? got.text : null;
  // Empty is a state of the FILE; unreadable is a state of the read. Collapsing them into one flag
  // took the editor away from an empty file, so `touch note.md` produced something this console
  // would show and never let anybody write into.
  drawFile(path, text === null ? tr('files.unreadable') : text, text === null, text === '');
  loadTree(a);
}

// The file, as the agent's own read tool rendered it — line numbers and all. Not re-numbered here
// and not stripped: a person and their companion pointing at different line 40s is the whole cost
// of tidying this up.
// Whether the file showing is being edited, and what it said when it was opened for editing — so
// "cancel" can put back exactly what was there rather than re-fetching a file the agent may have
// changed in the meantime.
let editing = null;
// What has been typed and not saved, by path. The buffer used to live only in the DOM that
// drawFile destroys, so switching card tabs threw the typing away without a word — measured: type
// a marker, press another tab, come back, the marker is gone and the toolbar still says Save.
// The draft outlives the DOM; Save and Cancel are what end it.
const drafts = new Map();

function drawFile(path, text, unreadable, empty) {
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
  { const back = backToList(); if (back) bar.append(back); }
  bar.append(cell('filedir', path));
  const acts = cell('fileacts');
  bar.append(acts);
  // Editing is offered only to somebody who may — the server refuses regardless, and a button that
  // answers 403 is one people learn not to press. `shell` is the gate: anybody who can run a
  // command in that workspace can already write any file in it.
  if (may('shell') && editing !== path && !unreadable) {
    const go = withMark(document.createElement('md-text-button'), '#i-sl-pen-to-square');
    label(go, tr('action.edit'));
    go.onclick = () => { editing = path; drawFile(path, text, false, empty); };
    acts.append(go);
  }
  if (editing === path) {
    fileViewEl.classList.remove('commitmode');
    fileViewEl.replaceChildren(bar, editor(path, text, acts));
    showCard();
    return;
  }
  const box = cell('filebody');
  // An empty file says so where its first line would be, and keeps its Edit: a grey rectangle
  // reads the same as a file still loading and the same as a read that failed.
  if (empty) box.append(cell('filesnote', tr('file.empty')));
  else box.append(...codeBlocks(text, path));
  // …and the file folds, from the same control and the same preference as the facts card beside
  // it. A file open in the slot is 60vh of the screen whether or not anybody is reading it right
  // now, and the way to put it away was to close it and open it again.
  const caret = document.createElement('button');
  caret.type = 'button';
  caret.className = 'foldcaret hit48';
  caret.setAttribute('aria-expanded', fileViewEl.hasAttribute('folded') ? 'false' : 'true');
  // Named by what it folds. It carried the About card's heading — a label naming a different
  // panel on a control that folds this file.
  caret.setAttribute('aria-label', tr('action.fold_named', {name: path}));
  { const c = iconOr('#i-sl-chevron-down', '▾', 'caret'); if (c) caret.append(c); }
  caret.onclick = () => setFolded(!fileViewEl.hasAttribute('folded'), true);
  bar.prepend(caret);
  const wrap = cell('foldwrap');
  wrap.append(box);
  fileViewEl.classList.remove('commitmode');
  fileViewEl.replaceChildren(bar, wrap);
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
// The open editor's note-clearer, so the Preferences switch can wipe remarks already drawn the
// moment look-over is turned off — the flag alone only stops the NEXT review. null when no editor
// is open. Set by editor(), cleared when it closes.
let lookClearActive = null;
let lookAt = 0;
let lookSrSeq = 0; // unique ids for each editor's sr-only remark line (aria-describedby target)
// Inline code completion and composer suggestion, both on by default and remembered — unlike
// look-over, which is a bill on every pause, these are cheap (the server does nothing until a fast
// profile is routed) and are the kind of help people expect on by default. Module-scope so the
// Preferences switches can flip them live, the way lookOn is flipped. Read where the editor and the
// composer decide whether to ask.
let acOn = localStorage.getItem('autocomplete') !== 'off';
let sugOn = localStorage.getItem('suggest') !== 'off';

function editor(path, text, acts) {
  const box = cell('fileedit');
  // A bare textarea, which this page otherwise does not build.
  //
  // The reason for the ban is a field that inherits the body's 14px, because iOS Safari zooms the
  // page in on anything under 16 and does not zoom back — so this one states its own size and
  // takes 16 where a finger is the input, which is the rule the ban exists to enforce.
  //
  // The component cannot do this job. Measured on the live page: its inner textarea sets
  // `white-space: pre-wrap` and `overflow-x: hidden` in its own shadow CSS, and neither can be
  // reached from out here — the host inheriting `pre` changes nothing. So every long line wrapped,
  // pressing edit re-flowed the file, and a column of line numbers could not be made to line up
  // with text whose lines are not the file's lines. Wrapping off, the editor and the reading view
  // are the same picture with a caret in it.
  const area = document.createElement('textarea');
  area.className = 'fileeditarea';
  area.setAttribute('spellcheck', 'false');
  area.setAttribute('wrap', 'off');
  area.setAttribute('aria-label', path);
  // The draft first: coming back to a half-edited file is coming back to the half-edit, not to
  // the file as it was.
  area.value = drafts.has(path) ? drafts.get(path) : plainText(text);
  // The same marking as the reading view, behind the text being typed.
  //
  // A textarea cannot hold coloured runs — it holds a string — so the colour goes on a copy of the
  // text UNDER it, in the same face at the same size, with the field itself made transparent. That
  // is how every browser editor that is not a rewritten text engine does it, and it costs nothing
  // when it drifts: the worst case is colour a pixel out of place behind a perfectly readable
  // caret. It is redrawn as you type.
  const behind = document.createElement('pre');
  behind.className = 'filecode editghost';
  behind.setAttribute('aria-hidden', 'true');
  // The numbers, in the reading view's own column: same element, same class, same sticky left edge
  // — so the two modes cannot drift apart in the one place a reader compares them.
  const nums = document.createElement('pre');
  nums.className = 'filegutter';
  nums.setAttribute('aria-hidden', 'true');
  let lines = -1;
  // The inline completion the model offered, spliced into the mirror at the caret so it reads as
  // grey text the caret sits in front of — null when there is none. It lives on the buffer copy the
  // reader sees, never in area.value, so it is text the person can take (Tab) or type past and it is
  // gone. See complete()/accept() below.
  let ghost = null;
  // What the model made of the region, keyed by absolute line number → one short clause. Drawn at
  // the end of the line it is about (a grey/amber note in the mirror, the way editors show an
  // end-of-line diagnostic), so the remark sits where the code is instead of a paragraph over the
  // top. null when there is nothing to say. See ask()/applyNotes below.
  let notes = null;
  const repaint = () => {
    const src = String(area.value || '');
    const comment = commentMark(path);
    behind.replaceChildren();
    const gAt = ghost && ghost.text ? Math.min(ghost.at, src.length) : -1;
    let placed = false;
    const emit = (text, cls) => {
      if (!cls) { behind.append(document.createTextNode(text)); return; }
      const m = document.createElement('span');
      m.className = cls;
      m.textContent = text;
      behind.append(m);
    };
    const ghostSpan = () => {
      const g = document.createElement('span');
      g.className = 'editcomplete';
      g.textContent = ghost.text;
      behind.append(g);
      placed = true;
    };
    // Line by line, because the scanner is: a comment runs to the end of ITS line, and handing it
    // the whole buffer made the first `//` in the file swallow everything after it — one grey span
    // where the reading view had ten marks. The reading view has always painted it this way. A
    // running char index locates the caret across lines so the ghost lands exactly where the model
    // was asked to continue.
    let pos = 0;
    let ln = 0;
    for (const line of src.split('\n')) {
      ln++;
      let col = pos;
      for (const part of codeParts(line, comment)) {
        const t = part.text;
        if (gAt >= col && gAt <= col + t.length && !placed) {
          emit(t.slice(0, gAt - col), part.cls);
          ghostSpan();
          emit(t.slice(gAt - col), part.cls);
        } else {
          emit(t, part.cls);
        }
        col += t.length;
      }
      if (!placed && gAt === col) ghostSpan(); // caret at the (possibly empty) line's end
      // The remark for this line, after its code and before the newline: it rides the same mirror,
      // scrolls with the line, and cannot drift off it because it IS on it.
      if (notes && notes.has(ln)) {
        const nspan = document.createElement('span');
        nspan.className = 'linenote';
        nspan.textContent = '  ‹ ' + notes.get(ln);
        behind.append(nspan);
      }
      behind.append(document.createTextNode('\n'));
      pos = col + 1; // + the newline
    }
    // Only when the count changed: typing inside a line is most of what typing is, and rebuilding
    // a two-thousand-line column for each keystroke is work nobody asked for.
    const n = src.split('\n').length;
    if (n !== lines) {
      lines = n;
      let g = '';
      for (let i = 1; i <= n; i++) g += i + '\n';
      nums.textContent = g;
    }
  };
  // The fallback line for anything the model says that is NOT a numbered finding — a stray sentence
  // it added despite the format. Above the buffer; empty (hidden) in the normal case, where every
  // remark went inline against its line.
  const said = cell('looksaid');
  said.hidden = true;
  // The same findings, for ears. The mirror that draws them is aria-hidden — it has to be, it
  // duplicates the textarea's text — so a screen reader heard NOTHING of a review a sighted
  // reader gets inline: the one channel that parsed cleanly was the one that was silent. This
  // sr-only status line carries "line: remark" pairs — announced politely when they arrive, and
  // re-readable from the field itself via aria-describedby.
  const srNotes = document.createElement('div');
  srNotes.className = 'sr-only';
  srNotes.id = 'looksr-' + (++lookSrSeq);
  srNotes.setAttribute('role', 'status');
  srNotes.setAttribute('aria-live', 'polite');
  area.setAttribute('aria-describedby', srNotes.id);
  // Fold the model's answer into per-line notes. Each finding is `<line><TAB><clause>` (or a colon
  // where a model used one); a line that does not parse is not dropped — it goes to the block above,
  // so a model that ignores the format still says its piece.
  const applyNotes = (out) => {
    notes = new Map();
    const extra = [];
    for (const raw of String(out || '').split('\n')) {
      const m = raw.match(/^\s*(\d+)\s*[\t:·]\s*(.+?)\s*$/);
      if (m) notes.set(parseInt(m[1], 10), m[2]);
      else if (raw.trim()) extra.push(raw.trim());
    }
    if (notes.size === 0) notes = null;
    const spoken = [];
    if (notes) for (const [ln, clause] of notes) spoken.push(ln + ': ' + clause);
    srNotes.textContent = spoken.concat(extra).join(' · ');
    said.textContent = extra.join('\n');
    said.hidden = !said.textContent;
    repaint();
  };
  const clearNotes = () => { notes = null; said.hidden = true; srNotes.textContent = ''; repaint(); };
  lookClearActive = clearNotes; // so the Preferences switch can wipe this editor's notes when turned off
  // The switch, and the only thing that turns this on. A model reading over somebody's shoulder is
  // a good idea and a bill; which of the two it is depends on whether they asked for it.
  const ask = async () => {
    if (!lookOn || !may('prompt')) { clearNotes(); return; }
    const mine = ++lookAt;
    // Only the region around the caret, and numbered with its real line numbers: a 40,000-line file
    // is not sent on every pause (their context window and their bill), and the model cites a line
    // that exists rather than counting from the top. Sixty lines each way is what a reviewer would
    // read around the change without asking to see the rest.
    const caret = area.selectionStart == null ? area.value.length : area.selectionStart;
    const all = area.value.split('\n');
    const caretLine = area.value.slice(0, caret).split('\n').length; // 1-based
    const R = 60;
    const from = Math.max(1, caretLine - R), to = Math.min(all.length, caretLine + R);
    let payload = '';
    for (let i = from; i <= to; i++) payload += i + '\t' + all[i - 1] + '\n';
    const out = await postText('/look' + qFor(lastDrawnFor || {socket: ''}),
                               new URLSearchParams({path: path, text: payload}));
    if (mine !== lookAt) return;             // they kept typing; this answer is about older text
    if (!lookOn) { clearNotes(); return; }   // turned off while this was in flight — do not paint it
    // Silence is the answer when there is nothing worth saying. No notes, no "looks good" — a
    // reviewer that always finds three things is one people stop reading.
    applyNotes(out);
  };
  // Inline completion is on by default and remembered; acOn is module-scope (Preferences flips it).
  // The server also self-disables when no fast profile is routed — it returns nothing before spending
  // any model — so a console with none configured simply never shows ghost text.
  let compAt = 0;
  const dismiss = () => { if (ghost) { ghost = null; repaint(); } };
  // Take the offered text: splice it into the buffer at the caret and move the caret past it. The
  // ghost only ever lived in the mirror, so this is the first time it becomes real text.
  const accept = () => {
    if (!ghost || !ghost.text) return false;
    // Only where the ghost actually sits. If the caret moved since the completion arrived — a mouse
    // click, a key we did not catch — splicing at ghost.at would drop the text far from where the
    // person is working, so bail and clear instead.
    if (area.selectionStart != null && area.selectionStart !== ghost.at) { dismiss(); return false; }
    const at = Math.min(ghost.at, area.value.length);
    area.value = area.value.slice(0, at) + ghost.text + area.value.slice(at);
    area.selectionStart = area.selectionEnd = at + ghost.text.length;
    ghost = null;
    drafts.set(path, area.value);
    repaint();
    pushOpen();   // the buffer changed; keep the companion's ambient copy in step
    return true;
  };
  const complete = async () => {
    if (!acOn || !may('prompt')) return;
    const caret = area.selectionStart;
    if (caret == null) return;
    const prefix = area.value.slice(0, caret);
    const suffix = area.value.slice(caret);
    if (!prefix.trim() && !suffix.trim()) return;
    const mine = ++compAt;
    const out = await postText('/complete' + qFor(lastDrawnFor || {socket: ''}),
                               new URLSearchParams({path: path, prefix: prefix, suffix: suffix}));
    if (mine !== compAt) return;               // a newer request has superseded this one
    if (area.selectionStart !== caret) return; // the caret moved while we waited
    const t = (out || '');
    ghost = t ? {at: caret, text: t} : null;
    repaint();
  };
  // Which file is open, and what is in the buffer that is not on disk, so the companion's next turn
  // can answer about the unsaved edit (the ambient context). Debounced like the rest.
  let openAt = 0;
  const pushOpen = () => {
    if (!may('prompt')) return;
    const mine = ++openAt;
    setTimeout(() => {
      if (mine !== openAt) return;
      post('/open-file', new URLSearchParams({path: path, text: area.value}),
           (lastDrawnFor || {}).socket || '', (lastDrawnFor || {}).peer || '');
    }, 600);
  };
  // The other half of the ambient contract: an empty text CLEARS the daemon's copy. Every teardown
  // below sends it — without this the buffer rode into every later turn of the session as "the file
  // the user is editing", hours after the tab closed, up to 24KB of stale text in the most
  // recency-privileged slot of the model's context. ++openAt first, so a debounced push in flight
  // cannot land after the clear and resurrect the buffer.
  const clearOpen = () => {
    if (!may('prompt')) return;
    ++openAt;
    post('/open-file', new URLSearchParams({path: path, text: ''}),
         (lastDrawnFor || {}).socket || '', (lastDrawnFor || {}).peer || '');
  };
  // On a pause, not on a keystroke. The review waits two seconds — long enough that a sentence being
  // typed is not sent five times; completion waits less, because a suggestion that arrives after you
  // have moved on is worth nothing. Typing dismisses any standing ghost first: it is about older text.
  area.addEventListener('input', () => {
    drafts.set(path, area.value);
    dismiss();
    notes = null;        // the remarks were about the text before this keystroke; ask() refreshes on the pause
    said.hidden = true;  // and so was any prose in the block above it — don't leave it up until the next pause
    repaint();
    pushOpen();
    const mine = ++lookAt;
    setTimeout(() => { if (mine === lookAt) { lookAt = mine - 1; ask(); } }, 2000);
    const cm = ++compAt;
    setTimeout(() => { if (cm === compAt) { compAt = cm - 1; complete(); } }, 350);
  });
  // Tab takes the ghost when there is one, and is a TAB otherwise — an editor where Tab walks
  // to the next control is a text field pretending, and this comment used to claim "otherwise Tab
  // is a tab" while nothing made it one: the browser default moved focus and the keystroke left
  // the file. execCommand keeps the insertion on the undo stack, which assignment to .value would
  // wipe. Shift+Tab still moves focus backwards on purpose: it is the keyboard reader's one way
  // OUT of a field that eats Tab, and losing it would trade an annoyance for a trap.
  area.addEventListener('keydown', (e) => {
    // Tab and Enter are also an input method's keys — see composing() — so an editor that grabs
    // them has to ask first, or committing a Korean syllable inserts a tab instead.
    // ⌘S/⌃S saves the file, because in an editor that is what the key MEANS — the browser default
    // (save the page as HTML) is never what somebody mid-edit wants, and a keystroke every editor
    // honours going to the browser reads as data loss until the save button is spotted.
    if ((e.metaKey || e.ctrlKey) && (e.key === 's' || e.key === 'S')) {
      e.preventDefault();
      save.click();
      return;
    }
    if (e.key === 'Tab' && !e.shiftKey && !composing(e)) {
      e.preventDefault();
      // Take the ghost only if the caret is still where the ghost is; otherwise it is stale.
      if (ghost && ghost.text) {
        if (area.selectionStart == null || area.selectionStart === ghost.at) { accept(); return; }
        dismiss();
      }
      document.execCommand('insertText', false, '\t');
      return;
    }
    if (e.key === 'Escape' || e.key === 'ArrowLeft' || e.key === 'ArrowRight' ||
        e.key === 'ArrowUp' || e.key === 'ArrowDown' || e.key === 'Home' || e.key === 'End' ||
        e.key === 'PageUp' || e.key === 'PageDown') {
      dismiss();
    }
  });
  area.addEventListener('blur', dismiss);
  // A mouse click repositions the caret without a keystroke or a blur, which would leave the grey
  // ghost stranded at the old spot; drop it so the next Tab is a tab, not a mis-placed splice.
  area.addEventListener('pointerdown', dismiss);
  // Tell the companion this file is open the moment the editor is (before any typing), so a question
  // asked straight away is still answered against the buffer.
  pushOpen();
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
    // Saving can create the file as well as change it, so what the tree holds is out of date the
    // moment this returns.
    if (!why) forgetTree(lastDrawnFor);
    if (why) {
      // A refusal here is usually the file having moved: the companion edited it while this was
      // open. Said where the model's own remarks go, because it is about this buffer.
      said.textContent = why;
      said.hidden = false;
      return;
    }
    editing = null;
    lookClearActive = null; // this editor is going away; the switch has nothing here to clear
    clearOpen();            // and the daemon's ambient copy goes with it — the file is on disk now
    drafts.delete(path);
    // Read back rather than drawn from what was typed: the file on disk is the fact, the tool may
    // have written it differently (a missing final newline), and the companion has just been told
    // in its own log that this happened.
    openFile(lastDrawnFor, path);
  };
  const stop = withMark(document.createElement('md-text-button'), '#i-sl-xmark');
  label(stop, tr('action.cancel'));
  stop.onclick = () => { editing = null; lookClearActive = null; clearOpen(); drafts.delete(path); drawFile(path, text); };
  // Into the bar at the top, where the edit button was: the control that starts this and the two
  // that end it are the same control in three states, and a control that moves is one you look for.
  acts.append(save, stop);
  // The reading view's own shape: a column of numbers and a column of file, scrolling as one box.
  // Written as the same grid rather than something that resembles it, because "the same picture
  // with a caret in it" is the whole requirement here.
  const wrap = cell('filebody editbody');
  const stack = cell('editstack');
  stack.append(behind, area);
  wrap.append(nums, stack);
  box.append(said, srNotes, wrap);
  // Nothing measures anything. The ghost is the text, in flow, so it is exactly the size of the
  // file; the field is laid over it and takes that size; the pane around them is what scrolls. An
  // earlier turn at this sized the field from its own scrollHeight and got zero — a box measuring
  // itself while it is the thing being sized.
  repaint();
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
  // Not on a phone: "what this is" has a screen of its own there — the About tab — and offering it
  // again inside the workspace's own strip is the same card in two places, which is what a reader
  // asked about ("why is this companion's card in the workspace tab?").
  if (!onePane()) {
    const facts = document.createElement('md-secondary-tab');
    facts.textContent = tr('field.facts');
    facts.onclick = () => { cardShows = 'facts'; showCard(); drawCardTabs(a); };
    tabs.push(facts);
  }
  for (const path of openFiles) {
    const t = document.createElement('md-secondary-tab');
    t.append(cell('tablbl', path === PR ? tr('git.pr')
                           : path === COMMIT ? tr('git.commit')
                           : isDiff(path) ? tabName(diffPath(path)) + ' ±' : tabName(path)));
    // A way to shut it, on the tab, which is where an editor puts it. An icon button inside a tab
    // would be a target inside a target; this is a plain mark with its own click, and the tab
    // keeps its own.
    // A button, not an svg with a handler on it.
    //
    // It was a 14×14 &lt;svg aria-hidden="true"&gt; carrying an onclick: a quarter of the 48dp a thumb
    // needs, inside a 48dp tab whose own press switches file — so a miss switches instead of
    // closing — with no role, no name, no tab stop, and an attribute asserting to a screen reader
    // that it is not there. The one icon on this strip that does something was the one marked
    // decoration.
    const x = document.createElement('button');
    x.type = 'button';
    x.className = 'tabclose hit48';
    x.setAttribute('aria-label', tr('action.close_named', {name: tabName(path)}));
    // And the tab is named by the file, not by the file plus its close button: a tab takes its
    // name from its contents, so this one was reading "README.md Close README.md".
    t.setAttribute('aria-label', tabName(path));
    { const mark = iconOr('#i-sl-xmark', '×'); if (mark) x.append(mark); }
    {
      x.onclick = ev => {
        ev.stopPropagation();
        const shut = () => {
          // Closing the editor's tab ends the edit. Left set, `editing` outlived the tab: the
          // next opening of the same file from the tree landed straight in the editor — a mode
          // nobody asked for, with a buffer reset to the file.
          if (editing === path) {
            editing = null; lookClearActive = null; drafts.delete(path);
            // Clear the daemon's ambient copy too — the editor is gone, and a buffer left behind
            // rode into every later turn as "the file the user is editing" (see clearOpen in editor()).
            if (may('prompt')) {
              post('/open-file', new URLSearchParams({path: path, text: ''}),
                   (lastDrawnFor || {}).socket || '', (lastDrawnFor || {}).peer || '');
            }
          }
          openFiles = openFiles.filter(p => p !== path);
          if (cardShows === path) cardShows = openFiles[openFiles.length - 1] || 'facts';
          drawCardTabs(a);
          if (cardShows !== 'facts') {
            if (cardShows === COMMIT) openCommit(a);
            else if (cardShows === PR) openPR(a);
            else if (isDiff(cardShows)) openDiff(a, diffPath(cardShows), diffWhich(cardShows));
            else openFile(a, cardShows);
          }
          loadTree(a);
        };
        // Unsaved typing is asked about before it is discarded — the basic dialog the guide puts
        // in front of exactly this. Only when there IS typing: an editor opened and left alone
        // closes without a question.
        if (editing === path && drafts.has(path)) {
          confirmThis({
            head: tr('edit.discard_headline', {name: tabName(path)}),
            body: tr('edit.discard_body'),
            keep: tr('action.keep_editing'), keepMark: '#i-sl-pen-to-square',
            // Eraser, not ✕. Everywhere else in the app ✕ means dismiss/cancel/close; the one place
            // it sat on a DESTRUCTIVE button was here, throwing away unsaved edits. The eraser is the
            // mark git-discard already uses for "throw the change away", so ✕ stays "close" alone.
            doIt: tr('action.discard'), doMark: '#i-sl-eraser',
            go: shut,
          });
          return;
        }
        shut();
      };
      t.append(x);
    }
    t.onclick = () => {
      if (path === COMMIT) openCommit(a);
      else if (path === PR) openPR(a);
      else if (isDiff(path)) openDiff(a, diffPath(path), diffWhich(path));
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
  const first = onePane() ? 0 : 1;   // the facts tab is only there on a wide screen
  const at = cardShows === 'facts' ? 0 : openFiles.indexOf(cardShows) + first;
  const which = at < 0 ? 0 : at;
  tabs.forEach((t, i) => { t.active = i === which; });
  cardTabs.replaceChildren(...tabs);
  cardTabs.hidden = false;
  showCard();
}

// showCard draws whichever of the two the tab strip says.
function showCard() {
  const file = cardShows !== 'facts' && openFiles.includes(cardShows);
  // On a phone the workspace is one pane and the facts are a screen of their own — the About tab —
  // so standing the facts card in the empty slot (the wide-screen fallback when no file is open) is
  // wrong here: closing the last file must land on the workspace LIST, not on the info card, which
  // is what a reader means by "작업공간 탭 내용이 정보 내용으로 바뀜". drawPanels is the panel
  // authority; it shows the list and keeps #detail hidden while panel==='files'. Safe from
  // recursion — drawPanels calls neither showCard nor drawCardTabs.
  if (!file && onePane() && panel === 'files') {
    wsShows = 'files';
    drawPanels();
    return;
  }
  fileViewEl.hidden = !file;
  // The facts card folds its own body away; here it goes altogether, because something else is
  // standing in its place. Empty means there is no companion drawn yet, and an empty card is not
  // shown either.
  const facts = document.getElementById('detail');
  facts.hidden = file || !facts.children.length;
}

function baseName(p) { return String(p).split('/').pop() || p; }

// tabName is what a tab is called: the file's name, and as much of the path as it takes to tell it
// from the others that are open.
//
// Three tabs reading "SKILL.md" is three tabs nobody can choose between — measured, opening three
// changed files from the git card. The names are how a reader finds the file they were in, and a
// name shared by all of them is not one.
function tabName(path) {
  const name = baseName(path);
  const clash = openFiles.some(other => {
    const p = isDiff(other) ? diffPath(other) : other;
    return p !== path && baseName(p) === name;
  });
  if (!clash) return name;
  const parts = String(path).split('/');
  return parts.length > 1 ? parts[parts.length - 2] + '/' + name : name;
}

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
  closeX(fmtDialog, fmtK);
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
// A card in the pane, beside the plan and the queue. It was a strip in the dock, which put it over
// every screen at every width: on a phone 90px of it sat above whatever you were doing, and the
// file card's own actions were drawn underneath it. What is running is a fact about the state of
// the work, like the plan and the queue — read when you want it, not held in front of the thing
// you came to use.
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
  box.replaceChildren(head3(countedKey('#i-sl-layer-group', tr('field.queued'), items.length)), ...rows);
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
  if (!a) { showSide(stripEl, false); stripEl.replaceChildren(); drawQueued(null); return; }
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
  // A heading and the chips under it, like every other card in this pane. The count went: the
  // chips are two or three and countable by looking, and "· 2" beside a word is a number nobody
  // asked for.
  const box = cell('stripjobs');
  box.append(...chips);
  stripEl.replaceChildren(...(chips.length
    ? [head3(markedKey('#i-ss-play', tr('field.running'))), box] : []));
  showSide(stripEl, chips.length > 0);
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

// turnbar says only one thing: this companion has a turn open.
//
// Driven by the session's own `turn` frame and NOT by the rows' pending marks, because the wait
// that needed showing is the one no row can carry — several tool calls in, the last result has
// landed, and the model has not yet said what to do next. Nothing is being waited ON at that
// moment; the turn is simply still running. The prompt that started it has scrolled away by then,
// which is the other half of why this is at the top of the page and not in the transcript.
const turnwrap = document.getElementById('turnwrap');
const turnfor = document.getElementById('turnfor');
let turnOpen = false;
// When the turn started, on THIS page's clock. The frame carries the age in seconds rather than a
// timestamp, so the reading never depends on the browser's clock agreeing with the daemon's — a
// thing that is often hours wrong and never says so. Counted from here on, the same way the roster
// ticks its idle column.
let turnFrom = 0;
let turnTick = 0;
const paintTurnFor = () => {
  if (turnfor) turnfor.textContent = turnOpen ? dur(Math.max(0, Math.round((Date.now() - turnFrom) / 1000))) : '';
};
const showTurnbar = () => {
  if (turnwrap) turnwrap.hidden = !turnOpen;
  clearInterval(turnTick);
  turnTick = 0;
  paintTurnFor();
  // One second, and only while it is on: a timer left running against a hidden element is a wakeup
  // per second for the life of the tab, on a page that is often left open all day.
  if (turnOpen) turnTick = setInterval(paintTurnFor, 1000);
};
// Off whenever this page stops being able to know. A bar still moving after the stream dropped is
// the console claiming work it can no longer see.
const clearTurnbar = () => { turnOpen = false; showTurnbar(); };


// childLinks turns the session id in a spawned child's account into the way into its transcript.
//
// magi writes that account onto the tool call's own result — verbatim, opening with
// `[child <session id>, N step(s)]` (spawn.go's childAccount) — so the answer is already in the
// conversation, in the order the work happened. This is the last step: the id is the door, and a
// door nobody can press is a string.
//
// It replaces the LIST that used to sit on the companion's detail card. That list existed because
// the transcript could not answer for a child at all; now that it can, a second copy of the same
// children in a place with no surrounding context was one list too many.
const CHILD_ID = /\[child (s_[A-Za-z0-9_-]+)/g;
function childLinks(pre) {
  const text = pre.textContent || '';
  if (!/\[child s_/.test(text)) return pre;
  pre.textContent = '';
  let at = 0;
  for (const m of text.matchAll(CHILD_ID)) {
    pre.append(document.createTextNode(text.slice(at, m.index + '[child '.length)));
    const b = el('button', m[1]);
    b.type = 'button';
    b.className = 'childlink';
    b.onclick = () => goDeep('sub', m[1]);
    pre.append(b);
    at = m.index + m[0].length;
  }
  pre.append(document.createTextNode(text.slice(at)));
  return pre;
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
  if (win.length > i && panel !== 'talk' && !ptabs.hidden) {
    // Only what was added at the end. A compaction rewrites the head and rebuilds rows nobody has
    // to be told about.
    unread += Math.max(0, win.length - Math.max(i, shown.rows.length));
    paintUnread();
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

let es, fleetTimer, fleetES;

// The roster, pushed rather than asked for.
//
// The fleet screens polled every three seconds: one request per screen per viewer, for ever, and a
// companion that started work took up to three seconds to say so. This is the same connection the
// transcript uses, addressed at nobody, and it sends only when the list is different.
//
// The caller is handed the list. Screens that need more than the roster (the map's edges, a
// meeting's transcript) fetch what they need when the event says something moved — which is still
// nothing at all while a fleet is quiet.
function watchFleet(onList, extra, only) {
  stopFleetWatch();
  // On a companion's page the transcript's connection carries these frames too, so this listens to
  // that one instead of opening a second.
  //
  // A browser allows six connections to one host and a stream never ends. Two per window meant
  // three windows filled the budget and every ordinary request from every window queued behind a
  // stream that would never finish: measured, the third window's first fetch never came back. It
  // looked like a deadlock and, for the page, it was one.
  //
  // Only when there is nothing extra to ask for: a meeting screen addresses its room in the query,
  // which is a different subscription from the one the transcript opened.
  const shared = es && !extra;
  fleetES = shared ? es : new EventSource('/events' + (extra || ''));
  // A meeting screen listens for its room and NOT for the roster.
  //
  // The roster carries how long each companion has been idle, which is a number that changes every
  // second — so a screen that redrew on every roster frame re-read the meeting once a second, which
  // is worse than the two-second poll it replaced. Measured: twenty requests in a quiet ten.
  if (only !== 'meet') {
    fleetES.addEventListener('fleet', e => {
      let list = null;
      try { list = JSON.parse(e.data); } catch { return; }
      onList(list);
    });
  }
  fleetES.addEventListener('meet', () => onList(null));
  // What each participant is thinking, as it thinks it — the same rows the conversation screen
  // draws, for everybody in the room, on this one connection.
  fleetES.addEventListener('room', e => {
    let f = null;
    try { f = JSON.parse(e.data); } catch { return; }
    if (!f || !f.who) return;
    roomLive.set(f.who, f.rows || []);
    paintRooms(f.who);
  });
  // A console that restarts is ordinary, and so is a laptop waking up. Reconnect quietly, and only
  // while this screen is still the one that wanted it.
  // A shared connection is the transcript's to reconnect: connect() already does that, and two
  // handlers racing to reopen one socket is how a page ends up with three.
  if (!shared) {
    // A console that restarts is ordinary, and so is a laptop waking up. Reconnect quietly, and
    // only while this screen is still the one that wanted it.
    fleetES.onerror = () => {
      const mine = fleetES;
      if (mine) mine.close();
      if (fleetES === mine) { fleetES = null; setTimeout(() => { if (!fleetES) watchFleet(onList, extra); }, 1500); }
    };
  }
  fleetShared = shared;
}

// Whether the roster frames are arriving on the transcript's connection. A shared one is not this
// screen's to close — closing it would take the conversation with it.
let fleetShared = false;

function stopFleetWatch() {
  if (!fleetES) return;
  const going = fleetES;
  fleetES = null;
  if (fleetShared) { fleetShared = false; return; }
  going.close();
}
function connect() {
  es = new EventSource('/events' + q());
  // Connected is the steady state, and the steady state is the dot's job.
  //
  // This used to WRITE "Live" into the status line, where nothing ever cleared it: on a
  // companion's page that line read "Live" for the life of the tab, beside a green dot saying the
  // same thing. On a phone the bar cut it to "L…", which is a word that answers nothing and was
  // taking the room the companion's own name needed. The line is for what has gone wrong or just
  // happened — so opening CLEARS it (the "reconnecting" it replaces is over) and the fact is
  // announced to anyone who cannot see the dot.
  es.onopen = () => { conn('live'); says(''); say(tr('state.live')); };
  es.onmessage = e => { lastRows = JSON.parse(e.data); draw(lastRows); };
  // The session's own state, on its own frame (see the events handler): sent when it changes, so
  // this is the whole of keeping the bar honest.
  es.addEventListener('turn', e => {
    try {
      const d = JSON.parse(e.data);
      turnOpen = !!d.open;
      turnFrom = Date.now() - (Number(d.forSec) || 0) * 1000;
    } catch (_) { turnOpen = false; }
    showTurnbar();
  });
  // The daemon outliving this page is normal, and so is the reverse. Reconnect quietly rather
  // than making a restart look like a failure.
  es.onerror = () => { conn('lost'); says(tr('state.reconnecting')); clearTurnbar();
                       es.close();
                       // Not while the fleet says this companion is gone. A socket that no longer
                       // exists answers 404 to every attempt, and the page was making one every
                       // 1.5 seconds for as long as the tab stayed open — saying "Reconnecting…"
                       // over a page that had already said the companion has stopped.
                       if (sock() && companionOK) setTimeout(connect, 1500); };
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
  document.getElementById('ptabTalk').textContent = tr('panel.talk');
  paintUnread();   // the label is rewritten wholesale, badge included
  document.getElementById('ptabFacts').textContent = tr('panel.facts');
  document.getElementById('ptabFiles').textContent = tr('panel.files');
  document.getElementById('ptabPlan').textContent = tr('panel.plan');
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
  for (const id of ['ptabTalk', 'ptabFacts', 'ptabFiles', 'ptabPlan']) {
    document.getElementById(id).fullWidthIndicator = true;
  }
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
  // The two side columns are both `complementary`, and a landmark list offering the same word
  // twice with nothing to tell them apart is the case the guide names. Same words the pane
  // toggles and the tab strip already use for them.
  filesEl.setAttribute('aria-label', tr('panel.files'));
  sideEl.setAttribute('aria-label', tr('panel.plan'));
  // The label beside it, and the one it announces. Both from the pack: the row in the dialog is a
  // preference like the two below it and has to say which.
  document.getElementById('themeK').textContent = tr('pref.theme');
  paintTheme();
  prefsEl.setAttribute('aria-label', tr('nav.preferences'));
  // The palette opens on ⌘K/Ctrl+K, and a shortcut written down nowhere is one nobody finds — the
  // page's own principle. Named in the tooltip (the platform's own chord) and to assistive tech.
  palOpen.setAttribute('aria-label', tr('pal.head'));
  palOpen.setAttribute('aria-keyshortcuts', 'Meta+K Control+K');
  const nav = typeof navigator !== 'undefined' ? navigator : {};
  const cmdKey = /mac|iphone|ipad/i.test(nav.platform || nav.userAgent || '') ? '⌘K' : 'Ctrl+K';
  tip(palOpen, tr('pal.head') + '  ·  ' + cmdKey);
  document.getElementById('lookK').textContent = tr('files.look');
  document.getElementById('lookWhy').textContent = tr('files.look_why');
  document.getElementById('acK').textContent = tr('pref.autocomplete');
  document.getElementById('acWhy').textContent = tr('pref.autocomplete_why');
  document.getElementById('sugK').textContent = tr('pref.suggest');
  document.getElementById('sugWhy').textContent = tr('pref.suggest_why');
  document.getElementById('grpAppearance').textContent = tr('pref.grp.appearance');
  document.getElementById('grpNotify').textContent = tr('pref.grp.notify');
  document.getElementById('grpAssist').textContent = tr('pref.grp.assist');
  document.getElementById('grpComplete').textContent = tr('ac.head');
  document.getElementById('grpConsole').textContent = tr('pref.grp.console');
  document.getElementById('acsWhy').textContent = tr('ac.head_why');
  document.getElementById('ambientK').textContent = tr('ac.ambient');
  document.getElementById('ambientWhy').textContent = tr('ac.ambient_why');
  document.getElementById('crossK').textContent = tr('ac.cross');
  document.getElementById('crossWhy').textContent = tr('ac.cross_why');
  document.getElementById('codeProfK').textContent = tr('ac.code_profile');
  document.getElementById('codeProfWhy').textContent = tr('ac.code_profile_why');
  document.getElementById('compProfK').textContent = tr('ac.composer_profile');
  document.getElementById('compProfWhy').textContent = tr('ac.composer_profile_why');
  document.getElementById('commitTplK').textContent = tr('ac.commit_tpl');
  document.getElementById('prTplK').textContent = tr('ac.pr_tpl');
  if (codeProfSel) codeProfSel.setAttribute('label', tr('ac.profile_pick'));
  if (compProfSel) compProfSel.setAttribute('label', tr('ac.profile_pick'));
  if (commitTpl) commitTpl.setAttribute('label', tr('ac.commit_tpl'));
  if (prTpl) prTpl.setAttribute('label', tr('ac.pr_tpl'));
  { const g = document.getElementById('grpProfiles'); if (g) g.textContent = tr('prof.head'); }
  { const e = document.getElementById('profWhy'); if (e) e.textContent = tr('prof.head_why'); }
  if (profName) profName.setAttribute('label', tr('prof.name'));
  if (profBase) profBase.setAttribute('label', tr('prof.base_url'));
  if (profModel) profModel.setAttribute('label', tr('prof.model'));
  if (profKey) {
    // The label is the noun alone; the hint rides supporting text. A hint folded into the label
    // clipped 94px off it at compact width, and the guide forbids truncating a label at all.
    profKey.setAttribute('label', tr('prof.api_key'));
    profKey.setAttribute('supporting-text', tr('prof.api_key_hint'));
  }
  if (profSave) label(profSave, tr('prof.add'));
  // A switch's visible label is a sibling div, not a <label for> — so give each an accessible name
  // (the way notifySwitch already gets one), or a screen reader announces a nameless "switch".
  const ariaLabel = (id, key) => { const el = document.getElementById(id); if (el) el.setAttribute('aria-label', tr(key)); };
  ariaLabel('lookSwitch', 'files.look');
  ariaLabel('acSwitch', 'pref.autocomplete');
  ariaLabel('sugSwitch', 'pref.suggest');
  ariaLabel('ambientSwitch', 'ac.ambient');
  ariaLabel('crossSwitch', 'ac.cross');
  // The group subheaders look like headings; expose them as headings so AT can navigate the groups.
  for (const id of ['grpAppearance', 'grpNotify', 'grpAssist', 'grpComplete', 'grpConsole', 'grpProfiles']) {
    const g = document.getElementById(id);
    if (g) { g.setAttribute('role', 'heading'); g.setAttribute('aria-level', '3'); }
  }
  document.getElementById('accessK').textContent = tr('nav.access');
  document.getElementById('accessWhy').textContent = tr('access.why');
  label(document.getElementById('accessGo'), tr('access.open'));
  const provWhyText = document.getElementById('provWhyText');
  if (provWhyText) provWhyText.textContent = tr('prof.from_provider_why');
  const provSelEl = document.getElementById('provSel');
  if (provSelEl) provSelEl.setAttribute('label', tr('prof.provider'));
  const provModelSelEl = document.getElementById('provModelSel');
  if (provModelSelEl) provModelSelEl.setAttribute('label', tr('prof.provider_model'));
  prefsK.textContent = tr('nav.preferences');
  consoleK.textContent = tr('nav.this_console');
  // Both keys written out rather than built as key + '_sub': the phrase pack's own audit finds
  // unused phrases by grepping for the literal, and a key assembled at runtime is invisible to it
  // — which would leave four translated lines nobody could tell were still reachable.
  // The access door's VISIBLE label is the short word at both widths — the guide wants one-word
  // navigation labels, and three of the four already were. The three-word name stays as the
  // accessible one, which is the arrangement the guide itself blesses (a more descriptive label
  // where the visible words are terse), and it stays on the crumb, the title and the heading.
  for (const [el, key, lbl, sub, short] of [[railFleet, 'nav.companions', 'nav.companions', 'nav.companions_sub', 'nav.companions'],
                                      [railSkills, 'nav.shared', 'nav.shared', 'nav.shared_sub', 'nav.shared'],
                                      [railMeet, 'nav.meet', 'nav.meet', 'nav.meet_sub', 'nav.meet'],
                                      [railAccess, 'nav.access', 'nav.access_short', 'nav.access_sub', 'nav.access_short']]) {
    // The word is on the item whether or not it is drawn: collapsed, the icon is all there is to
    // see, and a rail nobody can read aloud is not a navigation. The icon itself is markup and is
    // not touched here — a shape does not need translating, and rebuilding it on every language
    // change would throw away four elements to replace them with the same four.
    el.setAttribute('aria-label', tr(key));
    el.querySelector('.lbl').textContent = tr(lbl);
    // …and the companions door carries a count in its name when somebody is waiting, which this
    // loop has just overwritten with the plain word. Put back below, after the loop.
    // The same destination in one or two words, for the bar at the bottom of a phone. The guide is
    // explicit that a bar's label is not shrunk or truncated to fit — so where the long name does
    // not fit in a quarter of a 390px screen, there is a short one, and where it does the two are
    // the same word.
    el.querySelector('.lblshort').textContent = tr(short);
    // And what is behind it, one line, drawn only when the rail is open. Open and closed carried
    // the same four words at two sizes, so widening the rail bought room and spent it on nothing —
    // while "shared" and "meeting" are exactly the two a newcomer cannot guess. The stylesheet
    // hides it collapsed; the text is written either way so a language change reaches it.
    el.querySelector('.sub').textContent = tr(sub);
  }
  markWaiting(markWaiting.n || 0);
  closeX(mcpDialog, mcpDialogK);
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
  if (view() === 'skills') { loadSkills(); loadWiki(); loadMCP(); }
  else if (view() === 'access' && mayEl(accessEl)) loadAccess();
  else if (view() === 'board') loadBoard();
  // Map and meet had no branch here, so `!sock()` was true on both and the loader repainted a
  // HIDDEN fleet list while the visible screen kept its seeded English — measured on the demo in
  // Korean: title and crumb 배치도, body "Where the fleet is running…".
  else if (view() === 'map') loadMap();
  else if (view() === 'meet') loadMeet();
  else if (!sock()) loadFleet();
  // The screen's own name in the heading tree is words from the pack too: a Korean console read
  // its every destination out as English to anybody listening, because this heading was written
  // only by showDestination.
  if (!sock() && !screenHead.hidden) {
    const OWN = {skills: 1, access: 1, meet: 1, map: 1};
    screenHead.textContent = OWN[view()] ? '' : (SECTION[view()] || tr('nav.companions'));
    screenHead.hidden = !screenHead.textContent;
  }
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
// What draw() remembers about the transcript it drew last: the rows and the nodes it made for
// them, and where the window sits. Anything that empties #log behind draw()'s back has to say so
// here, or the next frame reuses nodes that are no longer in the document.
function forgetShownRows() {
  shown.rows = [];
  shown.nodes = [];
  winFrom = 0;
  above = 0;
}

function clearCompanionView() {
  const sideCol = document.getElementById('side');
  if (refreshSideToggle.blank) { refreshSideToggle.blank.remove(); refreshSideToggle.blank = null; }
  for (const card of sideCol.children) { card.hidden = true; card._sideSig = null; }
  for (const el of [stripEl, document.getElementById('prompt'), document.getElementById('detail')]) {
    el.hidden = true;
    el.replaceChildren();
  }
  log.replaceChildren();
  forgetShownRows();
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
  wsShows = 'files';
  editing = null;
  drafts.clear();
  openDirs.clear();
  findQ = '';
  // The tree's draw memo belongs to the companion that was open. It is a signature of what was
  // last painted, and emptying filesEl above without clearing it meant coming BACK to the same
  // companion found a matching signature and returned early — leaving the two loading placeholders
  // that get inserted before the comparison as the pane's permanent contents, with no in-page way
  // back. Measured on the live console: second visit to any companion, tree gone, two bars
  // forever. The whole group of per-companion drafts and picks is reset here so none outlives its
  // companion: a commit message, a PR body, a staged-file pick were each keyed to nobody and
  // carried across a companion switch.
  loadTree.drawn = null;
  commitDraft = '';
  prDraft = '';
  // The draft rules were this companion's too — its config template, pre-fetched. Cleared so the next
  // companion re-fetches its own rather than inheriting the one before it.
  commitRules = '';
  prRules = '';
  // The update button's last outcome likewise: a finished line has said its piece — the next visit
  // starts clean. In-flight entries stay, though: glancing at another companion while an update
  // downloads must not re-arm the button, or coming back mid-download could fire a second POST.
  if (updateControl.state) {
    for (const [k, s] of updateControl.state) {
      if (!s.working) updateControl.state.delete(k);
    }
  }
  commitRulesOpen = false;
  prRulesOpen = false;
  commitPick = '';
  // The past-work search is this companion's, like the drafts above: left set, opening B's history
  // after typing on A's showed A's words in the field and pre-filtered B's sessions by A's term.
  findQuery = '';
  filesEl.replaceChildren();
  fileViewEl.replaceChildren();
  fileViewEl.hidden = true;
  cardTabs.hidden = true;
  cardTabs.replaceChildren();
}

// paintCrumbs writes the trail in the masthead: where you are, and every level that is a way back.
//
// Its own function because it is a self-contained answer to one question — the four slots and what
// each of them links to — and because render() is a sequence of such answers rather than one long
// procedure. Nothing here reads anything but the address.
// An anchor whose href is the empty string is a link to this page — focusable, in the tab order,
// and on a crumb
// that is carrying no level, nameless. Measured on a companion's page: the third tab stop was an
// empty link, on the one screen where the trail is the only way out. Empty means no href at all,
// which makes it a plain span again.
function leadsTo(el, to) { if (to) el.setAttribute('href', to); else el.removeAttribute('href'); }

function paintCrumbs(s, v) {
  // Where you are, in the masthead: magi / lessons, or magi / companions / design. The crumb that
  // names the section IS the way back to it, so "where am I" and the way out are one thing.
  //
  // It names the SECTION, not always the fleet. A crumb that read "fleet" while you stood in the
  // connections tab answered a question nobody asked and offered a way back to somewhere you had
  // not been.
  const section = s ? tr('nav.companions') : SECTION[v] || tr('nav.companions');
  back.textContent = section;
  back.setAttribute('href', at(s ? '' : HREF[v] || ''));
  crumbSep.hidden = !s;
  crumbHere.textContent = s ? nameOf(s) : '';
  // One level in, the companion's own name becomes a way BACK to its conversation and the third
  // crumb says where you are. Without that the only way out of a detail screen is the browser's
  // back button, which is not a control the page put there.
  const deep = deepIn();
  leadsTo(crumbHere, s ? at('?d=' + encodeURIComponent(s) + (peerOf() ? '&p=' + encodeURIComponent(peerOf()) : '')) : '');
  crumbHere.className = deep ? '' : 'here';
  crumbSep2.hidden = !deep;
  paintDeepCrumb();
  // Past work has a level of its own inside it — the list, and one session out of it — so the third
  // crumb becomes the way back to the list and a fourth says which session. Without that, opening
  // one left the crumb saying "past work" while showing a transcript, with no way back to the list
  // short of the browser's own button.
  // A verdict read inside a past session is INSIDE it, and the trail said the opposite: standing
  // in one, the crumbs read "api / ⚖ Melchior / s_8a30…", which puts the session inside the vote,
  // and the only link out went to the list of past work rather than to the transcript being read.
  // Measured on the live console.
  //
  // So when something is open inside a session, the session takes the third slot and IS the way
  // back to it, and the fourth says which of its things you are looking at. The list of past work
  // is one press further — from the session, where the trail is the four levels again.
  const inSession = pastOn() && pastOf();
  const deeper = inSession && (crOf() || subOf() || askOf() || inspOf());
  const backTo = tail => at('?d=' + encodeURIComponent(s) +
    (peerOf() ? '&p=' + encodeURIComponent(peerOf()) : '') + tail);
  const leaf = inSession;
  leadsTo(crumbDeep, !leaf ? ''
    : deeper ? backTo('&past=' + encodeURIComponent(pastOf()))
    : backTo('&past='));
  crumbDeep.className = leaf ? '' : 'here';
  crumbSep3.hidden = !leaf;
  crumbLeaf.textContent = !leaf ? '' : deeper ? deepWord() : pastOf();
  back.className = s ? '' : 'here';
  // The two ends of the trail, named — because a phone shows only those two.
  //
  // A compact app bar has room for a leading control and a headline, so the stylesheet draws the
  // level ABOVE as a back arrow and the level you are ON as the headline, and hides the rest. Both
  // are decided here rather than guessed in CSS from the shape of the markup: the trail is three
  // rungs deep on a companion, four inside a past session, and one on a destination, and only this
  // function knows which of the four elements are carrying a level at this moment.
  //
  // Appended after the className assignments above, all of which overwrite rather than add.
  const rungs = [back, crumbHere, crumbDeep, crumbLeaf].filter(e => e.textContent.trim());
  for (const e of rungs) e.classList.remove('up', 'leaf');
  if (rungs.length) rungs[rungs.length - 1].classList.add('leaf');
  // Only when it has somewhere to go. The last rung is where you are; the one before it is the way
  // out, and it is a link — except on a destination, where there is one rung and no way up.
  if (rungs.length > 1) {
    const up = rungs[rungs.length - 2];
    up.classList.add('up');
    // Named by its word, not by its glyph. The arrow is drawn with generated content, and a browser
    // folds that into the name computed from contents — measured, the link read "←Companions", and
    // many readers say "left arrow" for that character. The label wins over contents.
    up.setAttribute('aria-label', (up.textContent || '').trim());
  }
}

// showDestination hides everything that is not the screen being asked for, and lights the rail and
// the tabs to match.
//
// One place, so the rail and the tabs cannot come to disagree about where you are: they used to be
// set at each of the four click handlers, which is exactly how they did.
function showDestination(s, v) {
  // Which kind of page this is, for the rules that differ between them. On a companion's page the
  // tabs are gone, so anything that leans on them being there has to know.
  document.body.setAttribute('at', s ? 'agent' : 'list');
  document.body.setAttribute('view', s ? '' : v);
  // The rail says the same thing the tabs do. A list item has no selected state of its own, so
  // this is an attribute of ours and the stylesheet draws it — said once here rather than at each
  // of the four click handlers, which is how the two used to fall out of step.
  // A companion's page is INSIDE the companions destination, so that is the one that stays lit.
  // Marked by view alone it went dark the moment you opened a row, and the rail then said you were
  // nowhere — on the screen you reach it from most often.
  // The map is the companions destination seen another way, so that is the one that stays lit
  // while you stand in it — the same reason a companion's own page keeps it lit.
  for (const [el, key] of RAILS) {
    // The board is a view of the companions destination, like the map: it is reached from that
    // list and it shows that list's work. Left out of this test it matched no destination at all,
    // so standing on the board the bar said you were nowhere — measured, all four unlit and no
    // aria-current anywhere in the tree.
    const on = s || v === 'map' || v === 'board' ? key === 'fleet' : v === key;
    el.toggleAttribute('selected', on);
    // Drawn AND said. `selected` is an attribute of ours that the stylesheet reads; it means
    // nothing to anything else, so in the accessibility tree all four destinations looked alike
    // and the one question a navigation answers — which of these am I on — had no answer for
    // anybody listening. aria-current="page" is the standard word for it, and it goes on the four
    // links whether they are drawn as a rail or as the bar at the foot of a phone.
    if (on) el.setAttribute('aria-current', 'page'); else el.removeAttribute('aria-current');
  }
  // The screen's name in the heading tree. Only where nothing else says it: the meeting, people
  // and shared screens draw their own h2, and a second one would be the same word twice.
  // The board draws team names and, on a quiet day, no heading at all — so it needs this one. The
  // three screens that draw their own section head are the exceptions, not the rule.
  const OWN_HEAD = {skills: 1, access: 1, meet: 1, map: 1, settings: 1};
  const named = s ? nameOf(s) : OWN_HEAD[v] ? '' : (SECTION[v] || tr('nav.companions'));
  screenHead.textContent = named;
  screenHead.hidden = !named;
  fleetEl.hidden = !!s || v !== 'fleet';
  summaryEl.hidden = !!s || v !== 'fleet';
  // Two things share this destination — what companions have learned, and the servers they can
  // reach — and on a phone that is two purposes on one screen. Measured: 2.2 screens of scrolling
  // with thirteen controls. The strip in the destination's own head switches them there; above the
  // breakpoint they are one column and both are drawn.
  const sharedOne = onePane();
  skillsEl.hidden = !!s || v !== 'skills' || (sharedOne && sharedShows !== 'skills');
  boardEl.hidden = !!s || v !== 'board';
  mapEl.hidden = !!s || v !== 'map';
  // Hidden by the view AND by the capability, like the access screen: a meeting spends model turns
  // on several companions at once, so somebody who may not prompt should not arrive at the form by
  // editing the address either. The server refuses regardless.
  meetEl.hidden = !!s || v !== 'meet' || !mayEl(meetEl);
  wikiEl.hidden = !!s || v !== 'skills' || (sharedOne && sharedShows !== 'wiki');
  mcpEl.hidden = !!s || v !== 'skills' || (sharedOne && sharedShows !== 'mcp');
  // The switch, drawn once and only where it means something.
  sharedTabs.hidden = !!s || v !== 'skills' || !sharedOne;
  if (!sharedTabs.hidden) drawSharedTabs();
  // Hidden by the view AND by the capability: a screen somebody may not use is one they should not
  // be able to arrive at by editing the address either.
  accessEl.hidden = !!s || v !== 'access' || !mayEl(accessEl);
  // The settings destination. Loads on arrival — by the rail button, the palette, or the address
  // bar, which are the same door now — and flushes the blur-saved templates on the way out, which
  // the dialog's close event used to cover.
  // The one destination a companion's page does NOT take over (see render): settings opened from
  // a companion edits that companion's config, which is the whole reason the scope line exists.
  const showSettings = v === 'settings';
  if (!settingsEl.hidden && !showSettings) { flushTpl(commitTpl, 'commitTemplate'); flushTpl(prTpl, 'prTemplate'); }
  if (settingsEl.hidden && showSettings) { loadAutocomplete(); loadProfiles(); paintNotify(); }
  settingsEl.hidden = !showSettings;
}

// paintCompanionChrome is what a companion's page has that a list does not: the composer, the
// interrupt, the panels, and the reveal of whichever body of content this navigation arrived at.
//
// Returns true when the screen is one level in (a subagent, a verdict, a past session), because
// that is the one case where render() stops before starting a destination.
function paintCompanionChrome(s) {
  // Only on a companion's own page. Addressing one by typing its name into a box, from a list where
  // it is already on screen and one click away, is a second way to do the thing the list does — and
  // the harder one: it asks somebody to spell a name they can see.
  // The conversation and everything that acts on it belong to the companion's page, not to a
  // screen about one piece of what happened there. Standing in a verdict, "send" would put a
  // message into a conversation that is not on screen.
  // Settings is the one destination a companion's page does not take over, so while it is on
  // screen the conversation and its chrome stand down — deepIn's own screens do exactly this, and
  // a settings screen with a composer under it would be offering to send a message into a
  // conversation that is not being shown.
  const onSettings = !!s && view() === 'settings';
  const deepNow = deepIn() || onSettings;
  // One deep screen keeps the composer: a session's own transcript. The rule the others fail is
  // that the conversation is not on screen — standing in a verdict, "send" would put a message
  // into a conversation you cannot see. Standing in a session, you are looking at the conversation
  // the message would go to; it is simply not the one the companion is in yet, and that is a
  // question the composer asks before it sends rather than a reason to take the box away.
  const onSession = pastOn() && !!pastOf();
  document.getElementById('agentdetail').hidden = !deepNow || onSettings;
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
  // landing on the facts — or the workspace — of an agent you just opened is a screen nobody asked
  // for. The reset fires on arriving at a DIFFERENT companion too, not only on leaving to the
  // fleet: a companion→companion jump (the palette row, a history owner link) kept the last one's
  // panel, so you landed on the new agent's Workspace with its conversation hidden. Same condition
  // as clearCompanionView below, since it is the same event — the content moved on, so the control
  // over it must not keep its old position. unread belongs to the companion left behind, so it goes
  // with the panel rather than painting a badge from A onto B's Talk tab.
  // Zeroing the count is not enough — the previous companion's badge is a node on the Talk tab, and
  // nothing repaints it here (draw() only repaints the badge while panel!=='talk', and we just set
  // it to talk), so A's "6" sat frozen on B's tab until an unrelated repaint. Clear the node too.
  if (!s || s !== drawnFor) { setPanel('talk'); unread = 0; paintUnread(); }
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
    return true;
  }
  return false;
}

// render draws the whole page for the address in the bar: the trail, which destination is showing,
// a companion's own chrome, and then the screen itself.
//
// The four are in that order because each depends on the one before it — the panels need the body
// attribute the destination sets, and the screen's own read needs the panels to exist. What it is
// NOT any more is a procedure: each step is a function that answers one question, and the last of
// them is a table (SCREENS) rather than a chain of ifs.
function render() {
  // The transcript's connection goes, and with it any roster listener that was sharing it — said
  // here rather than left to stopFleetWatch, which would otherwise be holding a closed socket.
  if (es) { es.close(); es = null; fleetES = null; fleetShared = false; }
  clearTurnbar();
  if (fleetTimer) { clearInterval(fleetTimer); fleetTimer = null; }
  const s = sock();
  // A companion's page takes over the view — except for settings, which is the one destination
  // that means MORE with a companion named, not less: it is that companion's own config being
  // edited, and the screen says so. Every other view is about the fleet and would be answering a
  // question nobody standing in a companion asked.
  const v = s ? (view() === 'settings' ? 'settings' : '') : view();
  retitle(0);
  paintCrumbs(s, v);
  showDestination(s, v);
  // Settings draws itself in showDestination and has nothing to read from a screen table, so it
  // returns here like the deep screens do.
  if (paintCompanionChrome(s) || (s && v === 'settings')) return;
  const screen = SCREENS[v] || SCREENS.fleet;
  screen.read();
  screen.watch?.();
}

// What each destination reads, and what it listens to.
//
// One table rather than an if-chain in render() and a second copy of the same list in freshen():
// arriving at a screen and coming back to it are the same question asked twice, and the two lists
// drifted the moment one of them was edited. `read` is the one-shot — it is also exactly what
// coming back to a tab needs — and `watch` is the subscription, which only render() starts.
const SCREENS = {
  board: {
    // Live, like the fleet beside it. A board that showed the day as it stood when you opened it
    // went stale the moment an agent finished something — and the day you watch it is the day work
    // is happening.
    read: () => loadBoard(),
    // Watched rather than polled. It used to poll every three seconds by building a signature —
    // the roster, and then every companion's history, one request each — so three companions cost
    // four requests every three seconds for as long as the tab was open: measured, twenty-four in
    // a quiet ten. A run finishing changes that companion's row, which is what the roster frame is.
    //
    // The one thing the signature did that the stream does not is stay out of the way of somebody
    // typing in the header, so that guard stays here.
    watch: () => watchFleet(() => {
      if (boardEl.contains(document.activeElement)) return;
      loadBoard();
    }),
  },
  map: {
    // A picture of who is talking to whom is worth nothing if it is a picture of five minutes ago —
    // and it costs nothing while nothing moves.
    read: () => loadMap(),
    watch: () => watchFleet(() => loadMap()),
  },
  meet: {
    // The room is where a turn lands. The stream carries the room itself, so a sentence appears
    // when the driver writes it rather than up to two seconds later.
    //
    // The frame is not drawn from the listener — loadMeet is called and does its own reading —
    // because the room's redraw has rules of its own (it holds still while somebody is typing, and
    // rebuilds only when the answer is different), and a second path into it would be a second set
    // of them. A meeting that has gone stops the reading rather than asking a console that has
    // already answered "no such meeting" once every two seconds for the evening.
    read: () => { if (!meetGone) loadMeet(); },
    watch: () => watchFleet(() => { if (!meetGone) loadMeet(); },
                            meetOf() ? '?m=' + encodeURIComponent(meetOf()) : '', 'meet'),
  },
  access: {
    // Read once and not watched: this list changes when somebody joins or leaves, which is not on a
    // three-second clock — and a table that reorders itself while an admin is picking a role is
    // worse than one a minute old.
    //
    // Not asked for at all when it may not be had. The server refuses either way; a fetch that
    // exists only to be refused is a 403 in somebody's audit record with nothing behind it.
    read: () => { if (mayEl(accessEl)) loadAccess(); },
  },
  skills: {
    // Both halves of the same story, in the order it happens: what has been said often enough to
    // become a rule, then the rules, then the servers those companions can reach. Not watched —
    // this is read and thought about, and a list that reorders itself under the cursor while
    // somebody decides what to promote is worse than one a minute old.
    //
    // BOTH halves, from here. There used to be a separate mcp branch: view() folds mcp into skills,
    // so it could not run, and the servers arrived only when something else happened to call
    // loadMCP — which is why the list was there on one visit and empty on the next.
    read: () => {
      loadSkills();
      // The server picker names companions, so the fleet is read before the list is drawn.
      fetchList('/fleet').then(list => { if (list) fleetSeen = list; loadMCP(); });
    },
  },
  fleet: {
    // The list, and a companion's own page: both are the roster, which carries the facts about a
    // companion that its log cannot.
    read: () => loadFleet(),
    watch: () => watchFleet(list => { if (list) loadFleet(list); }),
  },
};

// freshen reads the screen's content once, without touching what it is listening to.
//
// The page is event-driven now: frames arrive when something changes, and a screen nobody is
// looking at is not redrawn. That is the right bargain for cost and the wrong one for coming back
// — everything that happened while you were on another panel arrived as a frame that drew into a
// hidden box, or as no frame at all, and the screen you return to is the one you left.
//
// So: a read on arrival, and the subscription carries on. Deliberately the same loaders render()
// calls, rather than a second path that can answer differently — the failure this tree keeps
// writing down is two ways to learn one fact.
function freshen() {
  const s = sock();
  const v = s ? '' : view();
  if (deepIn()) {
    const known = (fleetSeen || []).find(x => x.socket === s && (x.peer || '') === peerOf());
    drawDeep(known || {socket: s, peer: peerOf()});
    return;
  }
  // The same one-shot render() starts the screen with. A second list of the destinations here is
  // what made the two disagree the first time one of them was edited.
  (SCREENS[v] || SCREENS.fleet).read();
}

// A tab nobody is looking at gives its stream back, and takes it again on the way in.
//
// Two reasons, and the second is the one that bites. A hidden tab is not being drawn, so its frames
// are work done for nobody. And a browser allows six connections to one host: a stream never ends,
// so six console windows hold every connection there is and the seventh cannot make an ordinary
// request at all — measured, its first fetch never came back. Windows left open in the background
// are exactly the ones paying for that, and they are the ones that need it least.
//
// Coming back re-reads the screen, which is the same thing it would do for a tab that had been
// hidden with its stream running: the frames that arrived while nobody was rendering were lost
// either way.
document.addEventListener('visibilitychange', () => {
  if (document.hidden) {
    if (es) { es.close(); es = null; }
    clearTurnbar();
    stopFleetWatch();
    conn('');
    return;
  }
  render();
});

// ── one keystroke to anything ────────────────────────────────────────────────
//
// Every control on this page is reachable by eye and slow by hand: another companion is rail →
// list → row, a file is pane → tree → scroll, and the verbs that have no home on the screen you
// are standing on (convene, compact, interrupt) are not reachable at all without leaving it.
//
// What it lists is what this console can already name — nothing here is a new capability, and
// every entry ends in a call the page already had. Four sources, in the order somebody means them:
// the verbs, the companions, the files of the workspace on screen, and what has been talked about
// (meetings, and this companion's past sessions).
const palDialog = document.getElementById('palDialog');
const palK = document.getElementById('palK'), palField = document.getElementById('palField');
const palList = document.getElementById('palList'), palNone = document.getElementById('palNone');
const palCancel = document.getElementById('palCancel');

// What the palette is showing right now, and which row the keyboard is on.
let palRows = [], palAt = 0, palAsked = 0;

// palVerbs is the page's own actions, each with the condition under which offering it is honest.
//
// `when` is not decoration: a palette that lists "interrupt" on a screen with no companion is a
// palette that teaches somebody to press things that do nothing. Every one of these is the same
// call the control on the screen makes — there is no second path to any of them.
function palVerbs() {
  const s = sock();
  const here = () => (fleetSeen || []).find(x => x.socket === s && (x.peer || '') === peerOf());
  return [
    {word: tr('pal.kind_verb'), name: tr('pal.conversation'), when: !!s && (deepIn() || panel !== 'talk'),
     go: () => { if (deepIn()) goDeep('past', null); setPanel('talk'); drawPanels(); }},
    {word: tr('pal.kind_verb'), name: tr('pal.workspace'), when: !!s,
     go: () => { setPanel('files'); drawPanels(); freshen(); }},
    {word: tr('pal.kind_verb'), name: tr('pal.find_file'), when: !!s, go: () => askFind(here() || {socket: s, peer: peerOf()})},
    {word: tr('pal.kind_verb'), name: tr('pal.interrupt'), when: !!s && mayEl(document.getElementById('stop')),
     go: () => confirmStop(nameOf(s), () => post('/interrupt', null).then(loadFleet))},
    {word: tr('pal.kind_verb'), name: tr('pal.compact'), when: !!s, go: () => post('/compact', null).then(loadFleet)},
    {word: tr('pal.kind_verb'), name: tr('pal.commit'), when: !!s, go: () => openCommit(here() || {socket: s, peer: peerOf()})},
    {word: tr('pal.kind_verb'), name: tr('pal.meetings'), when: mayEl(meetEl), go: () => goto(HREF.meet)},
    {word: tr('pal.kind_verb'), name: tr('nav.companions'), go: () => goto(HREF.fleet)},
    {word: tr('pal.kind_verb'), name: tr('nav.shared'), go: () => goto(HREF.skills)},
    {word: tr('pal.kind_verb'), name: tr('nav.board'), go: () => goto(HREF.board)},
    {word: tr('pal.kind_verb'), name: tr('pal.prefs'), go: () => { history.pushState({}, '', at(HREF.settings)); render(); }},
  ].filter(v => v.when !== false);
}

// goto is the palette's way of moving between destinations — the same pushState + render the rail
// does, so an address reached from here is an address that can be copied out of the bar.
function goto(href) {
  history.pushState({}, '', at(href));
  render();
  landOnScreen();
}

// A screen that arrives says where you are, to the keyboard as well as to the eye.
//
// Every activation that re-renders a region — opening a companion, the board, the map, answering a
// blocked companion, confirming a search — left document.activeElement on <body>: no ring anywhere
// on the new screen, nothing announced, and a reader who navigates by key starts again from a
// position they cannot see. The heading is where a screen begins, so that is where focus goes; it
// is not a tab stop afterwards, which is what tabindex="-1" is for.
function landOnScreen() {
  const h = document.getElementById('screenHead');
  // The leaf crumb by id, not a descendant selector: the render harness's DOM has no
  // document.querySelector, and this now runs on every navigation rather than only the palette's.
  const leaf = document.getElementById('crumbLeaf');
  const to = h && !h.hidden ? h : (leaf && (leaf.textContent || '').trim() ? leaf : null);
  if (!to || !to.focus) return;
  to.setAttribute('tabindex', '-1');
  to.focus({preventScroll: true});
}

// palMatch scores a row against what has been typed.
//
// Substring first, then a subsequence — "cmt" finds "commit" — and a match at the start of the
// name outranks one in the middle, because that is where people aim. Nothing clever: the list is
// tens of rows, not thousands, and a ranking nobody can predict is worse than a short list.
function palMatch(name, q) {
  const hay = String(name).toLowerCase(), needle = q.toLowerCase();
  if (!needle) return 0;
  const at = hay.indexOf(needle);
  if (at === 0) return 3;
  if (at > 0) return 2;
  let i = 0;
  for (const ch of hay) if (ch === needle[i]) i++;
  return i === needle.length ? 1 : -1;
}

// palGather is everything the palette can offer for one query.
//
// The verbs and the companions are already in this page's memory, so they answer instantly and
// without asking anybody. The rest is asked for only when there is a query to ask about — a
// palette that fetched the file list of every companion on open would be a keystroke that costs a
// walk of somebody's repository.
async function palGather(q) {
  const rows = [];
  for (const v of palVerbs()) {
    const score = q ? palMatch(v.name, q) : 3;
    if (score > 0) rows.push({...v, score});
  }
  for (const a of (fleetSeen || [])) {
    if (a.elsewhere) continue;
    const score = q ? palMatch(a.name + ' ' + (a.role || ''), q) : 2;
    if (score > 0) {
      rows.push({word: tr('pal.kind_companion'), name: a.name, hint: a.role || a.workdir, score,
                 go: () => go(a.socket, a.peer)});
    }
  }
  const s = sock();
  if (q.length >= 2 && s) {
    const a = (fleetSeen || []).find(x => x.socket === s && (x.peer || '') === peerOf()) ||
              {socket: s, peer: peerOf()};
    const got = await fetchOne('/find' + qFor(a) + '&in=names&q=' + encodeURIComponent(q));
    for (const hit of ((got && got.hits) || []).slice(0, 8)) {
      rows.push({word: tr('pal.kind_file'), name: baseName(hit), hint: hit, score: 2,
                 go: () => openFile(a, hit)});
    }
  }
  rows.sort((x, y) => y.score - x.score);
  return rows.slice(0, 20);
}

// palDraw puts the rows on screen and keeps the keyboard's place in range.
// Bring the chosen row into view. The list is half the window tall over twice that in content, so
// arrowing past the sixth row moved a selection nobody could see — and Enter then ran a command
// off the bottom of the box.
function palShow(row) {
  if (row && row.scrollIntoView) row.scrollIntoView({block: 'nearest'});
}

let palAtEl = null;
function palDraw() {
  palAtEl = null;
  palList.replaceChildren(...palRows.map((r, i) => {
    const row = document.createElement('button');
    row.type = 'button';
    row.className = 'palrow' + (i === palAt ? ' at' : '');
    row.id = 'palrow-' + i;
    row.setAttribute('role', 'option');
    row.setAttribute('aria-selected', String(i === palAt));
    // The word for what this row IS, carried on the row rather than built from a key here: the
    // phrase pack is checked by reading literal tr() calls out of this file, and a key assembled
    // at runtime is a phrase the check cannot see anybody asking for.
    row.append(cell('palkind', r.word));
    row.append(cell('palname', r.name));
    if (r.hint) row.append(cell('palhint', r.hint));
    row.onclick = () => palRun(i);
    if (i === palAt) palAtEl = row;
    return row;
  }));
  palNone.hidden = palRows.length > 0;
  palNone.textContent = tr('pal.nothing');
  // Keep the marked row in view, and name it for a reader. Arrowing the selection 300px below the
  // fold left the list scrolled to the top with no ring on screen; and with the rows now carrying
  // ids, the field can point aria-activedescendant at the current one so a keystroke that moves
  // the selection is announced.
  palShow(palAtEl);
  if (palAtEl) palField.setAttribute('aria-activedescendant', palAtEl.id);
  else palField.removeAttribute('aria-activedescendant');
  palAria(palAtEl ? palAtEl.id : '');
}

// The combobox semantics have to reach the input AT actually focuses, which is inside the field's
// shadow root — set on the host, aria-activedescendant is inert (the component does not forward it,
// and delegatesFocus puts real focus on the inner input). So they are written on the inner input
// directly. No shadow root (the render harness) means nothing to do, which is fine there.
function palAria(activeId) {
  const input = palField.shadowRoot && palField.shadowRoot.querySelector &&
    palField.shadowRoot.querySelector('input, textarea');
  if (!input) return;
  input.setAttribute('role', 'combobox');
  input.setAttribute('aria-controls', 'palList');
  input.setAttribute('aria-expanded', palRows.length ? 'true' : 'false');
  input.setAttribute('aria-autocomplete', 'list');
  if (activeId) input.setAttribute('aria-activedescendant', activeId);
  else input.removeAttribute('aria-activedescendant');
}

// palRun closes first and acts second: several of these draw the screen the palette is sitting on
// top of, and a dialog closing after that is a redraw somebody watches happen twice.
function palRun(i) {
  const row = palRows[i];
  if (!row) return;
  palDialog.close('go');
  row.go();
}

async function palAsk() {
  const q = String(palField.value || '').trim();
  const mine = ++palAsked;
  const rows = await palGather(q);
  if (mine !== palAsked) return;   // a later keystroke is already on its way
  palRows = rows;
  palAt = 0;
  palDraw();
}

function openPalette() {
  if (palDialog.open) return;
  palK.textContent = tr('pal.head');
  closeX(palDialog, palK);
  palField.setAttribute('label', tr('pal.label'));
  palList.setAttribute('aria-label', tr('pal.results'));
  palField.value = '';
  palCancel.textContent = tr('action.cancel');
  withMark(palCancel, '#i-sl-xmark');
  palRows = []; palAt = 0;
  palDraw();
  palDialog.show();
  if (palField.focus) palField.focus();
  // After show(), the field's shadow input exists to carry the combobox role.
  if (typeof requestAnimationFrame === 'function') requestAnimationFrame(() => palAria(''));
  else palAria('');
  palAsk();
}

palField.addEventListener('input', palAsk);
// The keys a chooser has to answer: move, take, and leave. Handled on the FIELD, because that is
// where the caret is — a listener on the dialog would fire after the field had already used the
// arrow to move the caret in the text.
palField.addEventListener('keydown', e => {
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault();
    if (!palRows.length) return;
    palAt = (palAt + (e.key === 'ArrowDown' ? 1 : palRows.length - 1)) % palRows.length;
    palDraw();
    return;
  }
  if (e.key === 'Enter' && !composing(e)) { e.preventDefault(); palRun(palAt); }
});
palCancel.onclick = () => palDialog.close('cancel');
// The door in the masthead. A phone has no modifier key, and a shortcut nobody has been told about
// is a shortcut nobody uses — the same reason every editor that has a palette also has a way to
// press it.
const palOpen = document.getElementById('palOpen');
palOpen.onclick = () => openPalette();
// Ctrl+K, or ⌘+K where that is the modifier. Not a bare key: this page has a text box on every
// screen, and a palette that opened on a letter would open while somebody was writing to their
// companion.
document.addEventListener('keydown', e => {
  if ((e.metaKey || e.ctrlKey) && (e.key === 'k' || e.key === 'K')) {
    e.preventDefault();
    openPalette();
  }
});

// nameOf is the crumb for a socket before the fleet has been fetched — the file name carries the
// workspace's base name, which is what a person calls the agent.
function nameOf(socket) {
  const base = socket.replace(/^.*\//, '').replace(/^daemon-/, '').replace(/\.sock$/, '');
  return base.replace(/-[a-z0-9]{8}$/, '');
}

function go(s, peer) {
  history.pushState({}, '', at(s ? '?d=' + encodeURIComponent(s) + (peer ? '&p=' + encodeURIComponent(peer) : '') : ''));
  render();
  // The new screen gets the focus, so a keyboard user is not left on <body> with no ring after
  // every activation that re-renders. goto() does this for the palette; the card, crumb and rail
  // paths render() straight and were leaving focus behind.
  landOnScreen();
}
// The crumb goes where it SAYS it goes. It names the section you are standing in, so sending it to
// the fleet regardless made the label and the click disagree — you would read "lessons" and land on
// companions. render() keeps its href current; this just follows it.
back.onclick = e => {
  e.preventDefault();
  const url = back.getAttribute('href') || '/';
  history.pushState({}, '', url);
  render();
  landOnScreen();
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
    if (!sock() && view() === key) { scrollTo({top: 0, behavior: stillness()}); return; }
    history.pushState({}, '', at(HREF[key]));
    render();
    landOnScreen();
  };
}
// The rail moves under the arrow keys, not only Tab. It is a set of destinations — a navigation
// landmark — and a reader arrowing through one expects to land on the next, not to fall out of it.
// Non-wrapping, like the tab strips: the guide asks a linear set not to loop.
railEl.addEventListener('keydown', e => {
  if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
  const items = RAILS.map(([el]) => el).filter(el => el && !el.hidden && el.offsetParent !== null);
  const at = items.indexOf(document.activeElement);
  if (at < 0) return;
  e.preventDefault();
  const to = e.key === 'ArrowDown' ? Math.min(at + 1, items.length - 1) : Math.max(at - 1, 0);
  if (to !== at && items[to].focus) items[to].focus();
});

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
// The two pane handles, by the attribute they own, so anything else that has to open a pane says
// it through the control that owns it — the attribute, what is remembered and what is announced all
// move together, which is the whole reason this function exists.
const paneSays = {};
function paneHandle(el, key, opened, words) {
  const say = open => {
    document.body.setAttribute(key, open ? 'open' : 'shut');
    localStorage.setItem(key, open ? 'open' : 'shut');
    // What a screen reader is told, from the same fact the drawing comes from, so the two cannot
    // come apart. The markup says "false" until this runs, which is the safe thing to have said.
    el.setAttribute('aria-expanded', String(open));
    // And WHAT it is: an icon button with no text and no label is a control a screen reader can
    // only announce as "button". The state pane's handle had one because a separate function
    // rewrote it whenever the pane's contents changed; the workspace handle, which has no such
    // function, had nothing at all — measured, the one unnamed control on a companion's page.
    //
    // Given by the caller rather than built from the key, because the phrase pack is checked by
    // reading literal tr() calls out of this file: a phrase addressed as key + '.show' is a
    // phrase the check cannot see anybody asking for.
    if (words) {
      const word = open ? words.hide : words.show;
      el.setAttribute('aria-label', word);
      tip(el, word);
    }
  };
  paneSays[key] = say;
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
paneHandle(filesToggle, 'files', () => loadTree(lastDrawnFor),
           {show: tr('files.show'), hide: tr('files.hide')});
// The state pane's handle says more than open and shut — it also says when there is nothing to
// open — so its words stay with the function that knows that: refreshSideToggle.
paneHandle(sideToggle, 'side');

// The look-over preference, wired where the other two preferences are. Remembered rather than
// asked again: it is true of the reader and not of the file, which is why it is here rather than
// on the editor.
const lookSwitch = document.getElementById('lookSwitch');
lookSwitch.selected = lookOn;
lookSwitch.addEventListener('change', () => {
  lookOn = !!lookSwitch.selected;
  localStorage.setItem('lookover', lookOn ? 'on' : 'off');
  // Turning it off clears what is already on screen, not just the next review — an editor open right
  // now obeys at once, the way the completion switches below promise for their ghost text.
  if (!lookOn && lookClearActive) lookClearActive();
});

// The two completion switches, wired the same way and flipping their module-scope flags live so an
// editor or composer already open obeys at once. On by default; storing 'off' is the only state
// worth writing, and absence reads as on.
const acSwitch = document.getElementById('acSwitch');
if (acSwitch) {
  acSwitch.selected = acOn;
  acSwitch.addEventListener('change', () => {
    acOn = !!acSwitch.selected;
    localStorage.setItem('autocomplete', acOn ? 'on' : 'off');
  });
}
const sugSwitch = document.getElementById('sugSwitch');
if (sugSwitch) {
  sugSwitch.selected = sugOn;
  sugSwitch.addEventListener('change', () => {
    sugOn = !!sugSwitch.selected;
    localStorage.setItem('suggest', sugOn ? 'on' : 'off');
  });
}

// The server-side completion settings — which fast profile does each kind, the ambient file, the
// draft rules. Loaded from and saved to the config of whichever companion the console is looking at
// (or the global config), the same file the MCP screen writes. Loaded when the dialog opens; each
// control saves its own field so flipping one does not rewrite the templates.
const ambientSwitch = document.getElementById('ambientSwitch');
const crossSwitch = document.getElementById('crossSwitch');
const codeProfSel = document.getElementById('codeProfSel');
const compProfSel = document.getElementById('compProfSel');
const commitTpl = document.getElementById('commitTpl');
const prTpl = document.getElementById('prTpl');
// Where the read and the write go: the companion in front of the reader, or the global config when
// there is none.
// Which config the settings screen reads and writes, from the ADDRESS.
//
// It used to come from lastDrawnFor, which is filled while drawing a companion's CONVERSATION —
// a page this screen never draws. So on a companion's settings the socket was null and every
// switch wrote the machine-wide file while the screen was open on that companion: the hidden
// axis this screen exists to make visible, getting the answer wrong in silence. sock()/peerOf()
// read the same query the scope header does, so what the header names is what gets written.
const acQ = () => (sock() ? qFor({socket: sock(), peer: peerOf()}) : '?tier=global');
const acSave = (field, value) => {
  // Same source as acQ, for the same reason: the screen writes where it says it is writing.
  const socket = sock();
  const body = new URLSearchParams();
  body.set(field, value);
  if (!socket) body.set('tier', 'global');   // the global config is not addressed by a socket
  return post('/autocomplete', body, socket || null, peerOf() || null);
};
const fillProfiles = (sel, profiles, current) => {
  if (!sel) return;
  sel.replaceChildren();
  const opt = (value, label) => {
    const o = document.createElement('md-select-option');
    o.value = value;
    const h = document.createElement('div');
    h.slot = 'headline';
    h.textContent = label;
    o.append(h);
    // The ATTRIBUTE, not just the property. md-select reads `hasAttribute("selected")` when it
    // resets, so a selection carried only as a property is one a reset silently drops — and these
    // fields sit inside a form.
    if (value === (current || '')) { o.selected = true; o.setAttribute('selected', ''); }
    sel.append(o);
  };
  opt('', tr('ac.profile_none'));
  // Each entry says WHERE it is defined. The name alone filled the picker and could not answer the
  // question a reader has in front of it — why a profile they just added is not offered here. It is
  // offered on the tier they wrote it to: a profile in a companion's own .magi/config.toml is
  // resolvable by that companion and by nobody else, which is what the daemon's own merge does.
  const have = (profiles || []).map(p => (typeof p === 'string' ? {name: p, tier: 'global'} : p));
  for (const p of have) {
    opt(p.name, p.name + ' — ' + tr(p.tier === 'project' ? 'ac.profile_here' : 'ac.profile_everywhere'));
  }
  // A profile assigned but no longer defined (deleted from [llm.profiles.*]) would otherwise render
  // as a blank select, hiding the stale assignment. Show it, marked, so the operator can see and fix it.
  if (current && !have.some(p => p.name === current)) {
    opt(current, current + ' — ' + tr('ac.profile_missing'));
  }
  // Assigning value here would do nothing: the setter is `select(v)`, which looks the value up in
  // `this.menu?.items ?? []` and gives up quietly when it finds none — and immediately after these
  // options were appended the menu has not rendered, so it finds none EVERY time. The saved choice
  // came back from the server correctly and the picker still opened blank. Wait for the component,
  // then set it, so the display text matches the option marked above.
  if (sel.updateComplete) {
    sel.updateComplete.then(() => { sel.value = current || ''; }).catch(() => {});
  } else {
    sel.value = current || '';
  }
};
// The scope line: which config this screen is editing, and where it lands. Drawn from the same
// answer the controls are filled from, so the header cannot claim one file while a switch writes
// another.
function drawScope(file) {
  const k = document.getElementById('settingsScopeK');
  const f = document.getElementById('settingsScopeFile');
  if (!k || !f) return;
  // From the ADDRESS, not from lastDrawnFor: that is filled while drawing a companion's
  // conversation, and this screen never draws one — so on a companion's settings it was still
  // null and the header said "machine-wide" over a project config. sock() is the same source
  // acQ() builds the request from, which is what makes the header and the write agree.
  const socket = sock();
  k.textContent = socket
    ? tr('settings.scope_project', {name: nameOf(socket) || ''})
    : tr('settings.scope_global');
  f.textContent = file ? tr('settings.scope_file', {file: file}) : '';
}

async function loadAutocomplete() {
  if (!may('configure')) return;
  const got = await fetchOne('/autocomplete' + acQ());
  if (!got) return;
  drawScope(got.file);
  if (ambientSwitch) ambientSwitch.selected = got.ambient !== false; // absent/true = default on
  if (crossSwitch) crossSwitch.selected = got.crossSession !== false;
  fillProfiles(codeProfSel, got.profiles, got.codeProfile);
  fillProfiles(compProfSel, got.profiles, got.composerProfile);
  if (commitTpl) { commitTpl.value = got.commitTemplate || ''; commitTpl._saved = commitTpl.value; }
  if (prTpl) { prTpl.value = got.prTemplate || ''; prTpl._saved = prTpl.value; }
}
// A textarea saves on 'change', which for a multiline field is blur — so an edit followed by Escape
// (dismissing the dialog while the field is still focused) could be lost. Flush on close catches it.
const flushTpl = (el, field) => {
  if (el && el.value !== el._saved) { acSave(field, el.value || ''); el._saved = el.value; }
};
if (ambientSwitch) ambientSwitch.addEventListener('change', () => acSave('ambient', ambientSwitch.selected ? 'on' : 'off'));
if (crossSwitch) crossSwitch.addEventListener('change', () => acSave('crossSession', crossSwitch.selected ? 'on' : 'off'));
if (codeProfSel) codeProfSel.addEventListener('change', () => acSave('codeProfile', codeProfSel.value || ''));
if (compProfSel) compProfSel.addEventListener('change', () => acSave('composerProfile', compProfSel.value || ''));
if (commitTpl) commitTpl.addEventListener('change', () => flushTpl(commitTpl, 'commitTemplate'));
if (prTpl) prTpl.addEventListener('change', () => flushTpl(prTpl, 'prTemplate'));
// Leaving the page does not blur a focused textarea first, so the templates flush on pagehide;
// leaving the SETTINGS SCREEN flushes in the view switch, where the screen is hidden.
addEventListener('pagehide', () => { flushTpl(commitTpl, 'commitTemplate'); flushTpl(prTpl, 'prTemplate'); });

// The profile editor — define/edit/remove the [llm.profiles.*] the pickers above choose from, so a
// fast completion backend can be set up here rather than in config.toml or the TUI. The key is
// write-only: it is never sent back (hasKey only), and a save leaves it untouched unless a new one is
// typed.
const profList = document.getElementById('profList');
const profName = document.getElementById('profName');
const profBase = document.getElementById('profBase');
const profModel = document.getElementById('profModel');
const profKey = document.getElementById('profKey');
const profSave = document.getElementById('profSave');
// profWrite targets a config. Called with no home, it writes where the PAGE is — the global
// config from the fleet screen, the companion's project config from its page — which is right for
// a NEW profile. A row's edit and remove pass the profile's OWN home instead: the list carries
// rows from both tiers, and a remove aimed at "wherever I happen to be standing" deleted nothing,
// silently, whenever the row's tier was not the page's — measured as "제거를 눌렀는데 아무 반응이
// 없다" from a companion page with a global profile.
const profWrite = (body, home) => {
  const f = home !== undefined ? {socket: home} : (lastDrawnFor || {});
  if (!f.socket) body.set('tier', 'global');
  // An explicit global home is post(null): '' would fall back to the page's own query and, on a
  // companion's page, aim a global row's write at the project config.
  const sock = home !== undefined && !home ? null : (f.socket || '');
  return post('/profiles', body, sock, f.peer || '');
};
// The home of the profile the form is editing, so saving it goes back where it came from rather
// than copying it into the current page's config. Cleared on save; a renamed profile is a new one
// and lands in the page's own config like any other new profile.
let editOrigin = null;
function renderProfiles(list) {
  if (!profList) return;
  profList.replaceChildren();
  for (const p of (list || [])) {
    const row = cell('profrow');
    const name = cell('profnm');
    name.textContent = p.name;
    const meta = cell('profmeta');
    meta.textContent = [p.model || tr('prof.no_model'), p.hasKey ? tr('prof.keyed') : '',
      p.tier === 'project' ? (p.companion || '') : tr('cron.machine')].filter(Boolean).join('  ·  ');
    // type="button", or each press ALSO submits prefsForm (a Material button defaults to submit),
    // and that form holds the required name field — measured: a real click on edit filled every
    // field and the constraint check then blanked the name it had just filled, so the press read
    // as doing nothing. The static buttons in this dialog all carry the attribute (see
    // themeToggle); these two are built here, where it was forgotten.
    const edit = label(document.createElement('md-text-button'), tr('action.edit'));
    edit.type = 'button';
    edit.onclick = () => {
      profName.value = p.name; profBase.value = p.baseUrl || ''; profModel.value = p.model || ''; profKey.value = '';
      editOrigin = {name: p.name, home: p.tier === 'project' ? (p.socket || '') : ''};
      if (profName.focus) profName.focus();
    };
    const del = label(withMark(document.createElement('md-text-button'), '#i-sl-trash-can'), tr('action.remove'));
    del.type = 'button';
    del.onclick = () => whileItRuns(del, async () => {
      const why = await profWrite(new URLSearchParams({name: p.name, delete: '1'}),
        p.tier === 'project' ? (p.socket || '') : '');
      if (!why) { loadProfiles(); loadAutocomplete(); }
      else says(why.slice(0, 80));
    });
    row.append(name, meta, edit, del);
    profList.append(row);
  }
  if (!(list || []).length) { const e = cell('profempty'); e.textContent = tr('prof.none'); profList.append(e); }
}
async function loadProfiles() {
  if (!may('configure') || !profList) return;
  renderProfiles(await fetchList('/profiles' + acQ()) || []);
  loadProviders();
}

// The provider picker: two dropdowns over what the shims themselves reported. A pick fills the
// free-text fields below — it does not save by itself, so the name stays editable and the save
// button stays the one way a profile comes to exist.
const provRow = document.getElementById('provRow');
const provWhy = document.getElementById('provWhy');
const provSel = document.getElementById('provSel');
const provModelSel = document.getElementById('provModelSel');
let provList = [];
const fillSelect = (sel, items, placeholder) => {
  if (!sel) return;
  sel.replaceChildren();
  const opt = (value, label) => {
    const o = document.createElement('md-select-option');
    o.value = value;
    const h = document.createElement('div');
    h.slot = 'headline';
    h.textContent = label;
    o.append(h);
    sel.append(o);
  };
  opt('', placeholder);
  for (const it of items) opt(it, it);
};
async function loadProviders() {
  if (!provRow) return;
  provList = await fetchList('/providers') || [];
  const have = provList.length > 0;
  // Hidden entirely while nothing serves: a picker with no providers teaches people not to open it.
  provRow.hidden = !have;
  if (provWhy) provWhy.hidden = !have;
  if (!have) return;
  fillSelect(provSel, provList.map(p => p.name), tr('prof.provider'));
  fillSelect(provModelSel, [], tr('prof.provider_model'));
}
if (provSel) provSel.addEventListener('change', () => {
  const p = provList.find(x => x.name === provSel.value);
  fillSelect(provModelSel, p ? p.models : [], tr('prof.provider_model'));
});
if (provModelSel) provModelSel.addEventListener('change', () => {
  const p = provList.find(x => x.name === provSel.value);
  if (!p || !provModelSel.value) return;
  // Fill, don't save: the person still names it (a suggestion is offered) and presses save, so
  // nothing lands in config that nobody asked for.
  profBase.value = p.base;
  profModel.value = provModelSel.value;
  if (!profName.value.trim()) profName.value = p.name;
  if (profName.focus) profName.focus();
});
if (profSave) profSave.onclick = () => whileItRuns(profSave, async () => {
  // Same shape as the MCP form above: an error goes to the field it is about — the component puts
  // it in the label with the alert role once error-text is set — and the status line is only for a
  // refusal that names no field. A refusal that went ONLY to the status line left the field
  // looking fine and the focus wherever it was.
  const profFields = [profName, profBase, profModel, profKey];
  for (const f of profFields) { f.removeAttribute('error'); f.removeAttribute('error-text'); }
  const nm = (profName.value || '').trim();
  if (!nm) {
    profName.setAttribute('error', '');
    profName.setAttribute('error-text', tr('prof.need_name'));
    if (profName.focus) profName.focus();
    return;
  }
  const body = new URLSearchParams({name: nm, baseUrl: profBase.value || '', model: profModel.value || ''});
  if ((profKey.value || '').trim()) body.set('apiKey', profKey.value);
  const home = (editOrigin && editOrigin.name === nm) ? editOrigin.home : undefined;
  const why = await profWrite(body, home);
  if (why) {
    const at = why.includes('name') ? profName :
      /url|base/i.test(why) ? profBase : /model/i.test(why) ? profModel : /key/i.test(why) ? profKey : null;
    if (at) { at.setAttribute('error', ''); at.setAttribute('error-text', why.slice(0, 120)); if (at.focus) at.focus(); }
    else says(why.slice(0, 80));
    return;
  }
  profName.value = profBase.value = profModel.value = profKey.value = '';
  editOrigin = null;
  loadProfiles();       // the list
  loadAutocomplete();   // and the pickers above, so a new profile is immediately assignable
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
function grip(el, prop, key, lead, name) {
  const root = document.documentElement;
  const rem = parseFloat(getComputedStyle(root).fontSize) || 16;
  // Named, and with a range, before anybody touches it. The attribute list was id, class, role,
  // aria-orientation, tabindex — a separator with no name and no position, and aria-valuenow only
  // appeared after the first arrow press. The handles beside these were given names for exactly
  // this reason; the grips were missed.
  if (name) el.setAttribute('aria-label', name);
  el.setAttribute('aria-valuemin', '12');
  el.setAttribute('aria-valuemax', '40');
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
  el.setAttribute('aria-valuenow', String(Math.round(widthNow())));
  el.addEventListener('pointerdown', ev => {
    ev.preventDefault();
    el.setPointerCapture(ev.pointerId);
    el.classList.add('gripping');
    const from = ev.clientX, was = widthNow();
    // A drag survives the window narrowing past the breakpoint that hides the handle — pointer
    // capture keeps the events coming — and it kept writing, and persisting, a width for a column
    // that no longer existed. No handle on screen, no drag: offsetParent is null exactly while
    // display:none takes it out of the layout.
    const move = m => {
      if (!el.offsetParent) return done();
      setW(was + (lead ? (from - m.clientX) : (m.clientX - from)) / rem);
    };
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
grip(document.getElementById('filesGrip'), '--magi-comp-files-w', 'files.w', false,
  tr('action.pane_width', { panel: tr('panel.files') }));
grip(document.getElementById('sideGrip'), '--magi-comp-side-w', 'side.w', true,
  tr('action.pane_width', { panel: tr('panel.plan') }));

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
// The door to the settings DESTINATION — the same sliders icon that opened the dialog, opening
// a place now. The loads happen in the view switch, where arriving by address gets them too.
prefsEl.onclick = () => { history.pushState({}, '', at(HREF.settings)); render(); };
// Settings is where the way to the people screen lives; see the markup for why it is not in the
// navigation. Both are destinations now, so this is an ordinary navigation.
document.getElementById('accessGo').onclick = () => {
  history.pushState({}, '', at(HREF.access));
  render();
};
// The dialog painted its slotted content on 'opened'; a screen's content is always in the DOM,
// so the labels only need repainting when the screen is first shown after a language change —
// paint() already covers that. What survives from the dialog era is the notify row's state, told
// when the screen arrives (the view switch calls loadAutocomplete/loadProfiles; paintNotify rides
// the same arrival).

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

addEventListener('popstate', e => {
  // The shared destination's two phone halves are history steps the same way the companion's
  // panels are. No state means the entry from before the first switch: the experience half.
  sharedShows = (e.state && e.state.shared) || (view() === 'skills' ? 'skills' : sharedShows);
  // A panel entry first: Back inside a companion's four phone screens moves between them. The
  // state is the panel the entry was PUSHED from; no state on a companion page means the entry
  // under the first move, which is the conversation.
  const wasPanel = ['talk', 'facts', 'files', 'plan'].indexOf(panel);
  const p = (e.state && e.state.panel) || 'talk';
  if (!ptabs.hidden && sock() && p !== panel) {
    setPanel(p);
    if (panel === 'talk') unread = 0;
    paintUnread();
    drawPanels();
    revealPanel(wasPanel);
    freshen();
    measureDock();
    return;
  }
  render();
  // Arriving at a companion from OUTSIDE it (Back from another destination), the entry may still
  // name the panel it was made on; the strip only exists after render, so it is honoured here.
  if (!ptabs.hidden && sock() && p !== panel) {
    setPanel(p);
    drawPanels();
    revealPanel();
    freshen();
    measureDock();
  }
  landOnScreen();
});

async function post(path, body, socket, peer, quiet) {
  // Either half can stand alone: a companion is named by its socket, a console by its peer name,
  // and a global rule on another console has only the second. With neither, the action is about
  // whatever the page is already looking at.
  const parts = [];
  if (socket) parts.push('d=' + encodeURIComponent(socket));
  if (peer) parts.push('p=' + encodeURIComponent(peer));
  // '' means "whatever the page is looking at" — the fallback below. null means NOBODY, said on
  // purpose: a global-tier write from a companion's page must NOT inherit that companion's ?d=,
  // or the server aims it at the project config and a delete of a global profile removes nothing,
  // silently. There was no way to say this before, and the profile rows needed to.
  const target = socket === null ? '' : (parts.length ? '?' + parts.join('&') : q());
  // A dropped connection IS a disconnection — the daemon went away mid-action. fetch only rejects
  // when the request never completed, so this is the same outage the GET path catches, and it must
  // say so rather than throw uncaught into a caller (send/permission/git-do) that has no .catch and
  // would surface nothing at all.
  let r;
  try { r = await fetch(path + target, {method:'POST', body}); }
  catch { reach(false); if (!quiet) says(tr('error.unreachable')); return tr('error.unreachable'); }
  if (r.ok) return '';
  // A refusal is not a disconnection. The daemon answered — it answered NO — and painting the
  // connection dot red for that says the console cannot hear a machine it is talking to.
  const why = (await r.text()).trim();
  // The whole reason goes to the console, where a line fits — the status line shows only its head,
  // and a long refusal ("this operator may not approve …") was cut mid-word with the tail lost. Now
  // the tail is recoverable, the same way the GET path keeps its refused body.
  if (why.length > 80) console.warn('magi-web', r.status, path, why);
  // Returned whole so the caller can put it where it belongs. Said out loud only when nobody takes
  // it: a form has a field to hang this on and a fleet action does not.
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
// The field grows with the text — up to six rows, then it scrolls.
//
// The old comment here claimed the component grows itself. It does not: rows stayed 1, and 300
// typed characters were a 24px window onto ten lines — measured, the visible text was the last
// eleven characters. The real textarea lives in the component's shadow root, so the size is read
// there (rows=1 first so the scrollHeight is the text's, not the box's), and only the ROWS
// property is written back — the component keeps owning its own box. Fails soft: no shadow root
// (the render tests' fake) means rows stays 1, which is where it started.
const grow = () => {
  const inner = t.shadowRoot && t.shadowRoot.querySelector && t.shadowRoot.querySelector('textarea');
  if (inner) {
    t.rows = 1;
    const line = parseFloat(getComputedStyle(inner).lineHeight) || 24;
    t.rows = Math.max(1, Math.min(6, Math.round(inner.scrollHeight / line)));
  }
  measureDock();
};

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
// The two bars are measured, not assumed.
//
// Their heights were written as constants — 64 for the one at the top, 64+16+1 for the one at the
// foot — and both are only true while every word fits on one line. At twice the text size the app
// bar is 88px and the strip stuck "under" it was half behind it; at 320px "Companions" wraps and
// the navigation bar is 93px against an 81px reservation, and at 2x it is 185 against 97. A bar
// that grows for scaled text is what the guide asks for; the page has to know how far it grew.
function measureBars() {
  const set = (k, v) => document.documentElement.style.setProperty(k, v + 'px');
  const bar = document.getElementById('masthead');
  // Where the bar ENDS, not how tall it is. Everything that sticks under it — the panel strip, the
  // meeting's head — offsets by this, and the bar is sticky at `top:0` on the console and at the
  // height of a notice on the demo. Measured as a height, the strip on the demo stuck 69px too
  // high and spent every scrolled moment behind the bar.
  //
  // getBoundingClientRect, not offsetHeight: a ResizeObserver watches the CONTENT box and the
  // scrolled bar grows a 1px border, so the one change this value has at runtime was the one
  // offsetHeight-from-an-observer could not see.
  if (bar) {
    const top = parseFloat(getComputedStyle(bar).top) || 0;
    const box = bar.getBoundingClientRect ? bar.getBoundingClientRect().height : bar.offsetHeight;
    set('--magi-comp-appbar-h', Math.round((box || 0) + top) || 64);
  }
  // Only while it IS a bar at the foot: above the breakpoint the rail is a column and the page
  // reserves nothing for it.
  const atFoot = typeof matchMedia === 'function' && matchMedia('(max-width:37.4375em)').matches;
  const bay = atFoot && railEl && railEl.getBoundingClientRect
    ? railEl.getBoundingClientRect().height : (railEl && railEl.offsetHeight) || 0;
  set('--magi-comp-navbar-h', atFoot ? Math.round(bay) || 81 : 0);
}
if (typeof ResizeObserver === 'function') {
  new ResizeObserver(measureDock).observe(dock);
  const bars = new ResizeObserver(measureBars);
  const bar = document.getElementById('masthead');
  // The border box, because that is what the things below it have to clear.
  if (bar) bars.observe(bar, {box: 'border-box'});
  if (railEl) bars.observe(railEl, {box: 'border-box'});
}
addEventListener('resize', measureBars);
measureBars();
// The app bar fills when the page moves under it, which is the guide's own description of this
// component and the only way it can be the page's colour at rest and still separate when there is
// something behind it. An attribute rather than a class: the stylesheet reads body[scrolled] the
// way it reads body[at] and body[panel].
{
  let was = false;
  const mark = () => {
    const now = (globalThis.scrollY || 0) > 0;
    if (now === was) return;
    was = now;
    document.body.toggleAttribute('scrolled', now);
    // The fill brings a hairline with it, so the bar is a pixel taller scrolled than at rest.
    measureBars();
  };
  addEventListener('scroll', mark, {passive: true});
  mark();
}
t.addEventListener('input', grow);
// Composer suggestion — how the model thinks this person will finish the instruction, learned from
// their own past prompts. The composer is an md component whose real textarea lives in its shadow
// root, so there is no plain field to splice inline ghost text into the way the file editor has; the
// suggestion shows dimmed under the box and Tab takes it. On by default and remembered; the server
// self-disables when no composer profile is routed, so a console with none never shows a hint.
// sugOn is module-scope (Preferences flips it live), declared with the other helper flags above.
let sugAt = 0, sugText = '';
const sugHint = document.createElement('div');
sugHint.className = 'sughint';
sugHint.hidden = true;
sugHint.setAttribute('aria-hidden', 'true');
// BELOW the composer row, not inside it: .composer is a nowrap flex row (field + buttons), so a hint
// placed between them cannot take its own line on a phone — it would squeeze the field. Placed after
// the composer box, it is a full-width line under it at every width.
{
  const composerBox = (t.closest && t.closest('.composer')) || t.parentNode;
  if (composerBox && composerBox.insertAdjacentElement) composerBox.insertAdjacentElement('afterend', sugHint);
  else if (t.insertAdjacentElement) t.insertAdjacentElement('afterend', sugHint);
  else if (t.parentNode) t.parentNode.appendChild(sugHint);
}
const sugClear = () => { if (sugText || !sugHint.hidden) { sugText = ''; sugHint.hidden = true; sugHint.textContent = ''; } };
// Take it: append to what they typed, since the suggestion continues from where they stopped.
const sugAccept = () => {
  if (!sugText) return false;
  t.value = String(t.value || '') + sugText;
  sugClear();
  grow();
  if (t.focus) t.focus();
  return true;
};
const suggest = async () => {
  if (!sugOn || !may('prompt')) return;
  const v = String(t.value || '');
  if (!v.trim()) { sugClear(); return; }   // an empty box is not a place to guess; it is annoying
  const mine = ++sugAt;
  const out = await postText('/suggest' + qFor(lastDrawnFor || {socket: ''}),
                             new URLSearchParams({prefix: v}));
  if (mine !== sugAt) return;               // superseded by a newer request
  if (String(t.value || '') !== v) return;  // they kept typing
  sugText = (out || '').trim();
  if (sugText) { sugHint.textContent = sugText; sugHint.hidden = false; }
  else sugClear();
};
t.addEventListener('input', () => {
  sugClear();                               // what they typed changed; the standing hint is stale
  const mine = ++sugAt;
  setTimeout(() => { if (mine === sugAt) { sugAt = mine - 1; suggest(); } }, 400);
});
t.addEventListener('keydown', (e) => {
  if (e.key === 'Tab' && sugText && !composing(e)) { e.preventDefault(); sugAccept(); return; }
  if (e.key === 'Escape') sugClear();
});
t.addEventListener('blur', sugClear);
f.onsubmit = e => {
  e.preventDefault();
  const v = t.value.trim(); if (!v) return;
  if (answering) {
    const a = answering;
    t.value = ''; grow(); sugClear();
    post('/answer', new URLSearchParams({call: a.askId, kind: a.askKind, text: v}), a.socket, a.peer)
      .then(why => {
        // An answer that did not land is worse than a message that did not: the companion is still
        // stopped, waiting for it. Put it back so it can be sent again rather than retyped.
        if (why && !t.value.trim()) { t.value = v; grow(); }
        loadFleet();
      });
    return;
  }
  // A leading bang runs the rest as a command, where the daemon is. The terminal has read this
  // prefix since it existed, and a console that sent "!ls" to the model as a prompt was quietly
  // doing something else with the same keystrokes.
  if (v.startsWith('!')) {
    const cmd = v.slice(1).trim();
    if (!cmd) return;
    t.value = ''; grow(); sugClear();
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
        t.value = ''; grow(); sugClear();
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
  //
  // Emptied now and filled again if it was refused. Cleared-and-forgotten is what this did, and
  // against a companion whose daemon has gone — measured live, a 502 — the words a person had just
  // typed vanished from the box with the reason in the masthead and no way to get them back. The
  // move-and-send path above has always put them back; this one, which is the ordinary way to send
  // anything, did not.
  t.value = ''; grow(); sugClear();
  post('/submit', new URLSearchParams({text: v})).then(why => {
    if (!why) return;
    if (!t.value.trim()) { t.value = v; grow(); }
  });
};
// Enter sends on a keyboard and inserts a newline on a phone: a soft keyboard's return key is the
// only way to break a line there, and hijacking it leaves no way to write a second paragraph.
const touch = matchMedia('(hover: none)').matches;
t.onkeydown = e => { if (e.key === 'Enter' && !e.shiftKey && !touch && !composing(e)) { e.preventDefault(); f.requestSubmit(); } };
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