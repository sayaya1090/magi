// A DOM small enough to run the page against, and strict enough to fail when the page is wrong.
//
// The front end is a Go string that no Go test can execute, so everything below the syntax check
// was unverified: whether a card gets the right class, whether a blocked agent grows the buttons
// that answer it, whether the empty state says anything. This is the smallest object graph the
// page's own code will accept — createElement, textContent, className, append, replaceChildren —
// and a page that reaches for anything else throws here rather than in somebody's browser.
//
// Deliberately not jsdom: a dependency, a lockfile and a second toolchain for a page that is one
// file. Anything this fake cannot express is a sign the page is doing more than it should.

const RENDERED = [];

function element(tag) {
  const node = {
    tag,
    children: [],
    attrs: {},
    style: {},
    _class: '',
    _text: '',
    set className(v) { this._class = String(v); },
    get className() { return this._class; },
    // Setting textContent REPLACES the node's contents, children included. The fake kept only its
    // own string and left the children standing, so a readout rebuilt from "5 agents" plus a button
    // and then reset to "cannot reach magi-web" read as both at once here and correctly in a
    // browser — a fake disagreeing with the DOM in the direction that hides a bug.
    set textContent(v) { this._text = String(v); this.children = []; },
    get textContent() { return this._text; },
    set innerHTML(v) { this._text = String(v).replace(/<[^>]*>/g, ' '); },
    set href(v) { this.attrs.href = v; },
    set placeholder(v) { this.attrs.placeholder = v; },
    set hidden(v) { this.attrs.hidden = !!v; },
    set disabled(v) { this.attrs.disabled = !!v; },
    get disabled() { return !!this.attrs.disabled; },
    set type(v) { this.attrs.type = v; },
    set title(v) { this.attrs.title = v; },
    get hidden() { return !!this.attrs.hidden; },
    append(...kids) { this.children.push(...kids); },
    replaceChildren(...kids) { this.children = kids; },
    addEventListener() {},
    requestSubmit() {},
    focus() {},
    // md-primary-tab keeps its selection in a property, not a class.
    // Written to either by md-tabs (below) or by the page. Both leave the same value behind, and
    // only one of them animates, so which one did it is recorded rather than enforced: a fake that
    // threw where the real one shrugs would be lying about the DOM to make a point.
    set active(v) { this.attrs.active = !!v; if (!byTabs) this.setDirectly = true; },
    get active() { return !!this.attrs.active; },
    // md-tabs owns which of its tabs is active, and the page asks it by index rather than writing
    // to the tabs — that is what makes the indicator slide. Mirrored here (Tabs.activateTab sets
    // active on every tab and clears the rest) so the page's routing is still testable with the
    // components stubbed out. The animation itself needs a real browser and is not modelled.
    // md-select owns which of its options is chosen, the same way md-tabs owns its tabs: the page
    // sets a value and the component resolves it to an option, which is where the field's display
    // text comes from. Mirrored so the page's routing of a preference is testable with the
    // components stubbed — marking an option selected on the way in is exactly the bug this
    // mirrors away from.
    set value(v) {
      this.attrs.value = v == null ? '' : String(v);
      if (this.tag.endsWith('-select')) {
        this.children.forEach((o) => { o.selected = o.value === this.attrs.value; });
      }
    },
    get value() { return this.attrs.value ?? ''; },
    set activeTabIndex(i) {
      this.attrs.activeTabIndex = i;
      byTabs = true;
      this.children.forEach((k, n) => { k.active = n === i; });
      byTabs = false;
    },
    get activeTabIndex() { return this.children.findIndex((k) => k.active); },
    set name(v) { this.attrs.name = v; },
    get name() { return this.attrs.name ?? ''; },
    set autocomplete(v) { this.attrs.autocomplete = v; },
    set onsubmit(f) { this._onsubmit = f; },
    get onsubmit() { return this._onsubmit; },
    // Attributes the console's controls set: a pressed state on the filter tiles, a disabled tile
    // for a count of zero, a title on the stop button.
    setAttribute(k, v) { this.attrs[k] = String(v); },
    getAttribute(k) { return this.attrs[k]; },
    removeAttribute(k) { delete this.attrs[k]; },
    hasAttribute(k) { return k in this.attrs; },
    toggleAttribute(k, on) { if (on) this.attrs[k] = ''; else delete this.attrs[k]; return !!on; },
    // text is everything this node and its descendants would show.
    get text() {
      return [this._text, ...this.children.map((k) => k.text)].join(' ').replace(/\s+/g, ' ').trim();
    },
    // find collects descendants (and self) of a tag — an array, so .find(fn) on the result is
    // Array.prototype.find and a test can pick a node by class.
    //
    // A predicate is allowed as well as a tag, because a control's tag is now the library's choice:
    // the same button is `md-text-button` here and `md-filled-button` there, and a test that asks
    // for `button` would report an empty card rather than a restyled one.
    // querySelector, for the one shape the page needs: a class among this node's descendants. The
    // markup holds elements the module does not create — the rail's icons and labels — and the
    // page reaches into them by class rather than giving each of eight an id of its own.
    querySelector(sel) {
      const want = String(sel).replace(/^\./, '');
      const hit = (n) => String(n.className || '').split(' ').includes(want);
      const walk = (n) => {
        for (const k of n.children) {
          if (hit(k)) return k;
          const deeper = walk(k);
          if (deeper) return deeper;
        }
        return null;
      };
      return walk(this);
    },
    find(t) {
      const hit = typeof t === 'function' ? t(this) : this.tag === t;
      const out = hit ? [this] : [];
      for (const k of this.children) out.push(...k.find(t));
      return out;
    },
  };
  return node;
}

// clicky matches a control by role rather than by tag. The same button is `md-text-button` in one
// place and `md-filled-button` in another — both are Material Web's, both are the thing a person
// presses — and a test that asked for `button` would report an empty card rather than a restyled
// one. Global so every snippet gets it without threading a helper through.
// True while md-tabs is the one changing the selection. See the active setter.
let byTabs = false;

globalThis.clicky = (n) => n.tag === 'button' || n.tag.endsWith('-button');

const byId = {};
for (const id of ['fleet', 'log', 'state', 'sid', 'back', 'f', 't', 'stop', 'prompt', 'dock', 'summary', 'detail', 'crumbSep', 'crumbHere', 'tabs', 'tabFleet', 'skills', 'tabSkills', 'to', 'roles', 'mcp', 'tabMcp', 'board', 'tabBoard', 'railBoard', 'handoffs', 'history', 'intervened', 'agentview', 'stream', 'side', 'plan', 'send',
                 'rail', 'railNav', 'scrim', 'theme', 'lang', 'prefsK',
                 'consoleK', 'console', 'prefs', 'prefsDialog', 'prefsClose', 'prefsForm', 'railMenu', 'themeToggle', 'railBadge', 'tabBadge', 'railMenu', 'railFleet', 'railSkills', 'railMcp',
                ]) byId[id] = element('div');
// The four tabs are children of #tabs in the markup, and md-tabs works through that relationship:
// it activates by index into its own children. A flat bag of ids would let the page set an index
// nothing answers to, and every tab would read as unselected.
// The two preference selects are md-outlined-select, and the fake mirrors a select's value→option
// resolution by tag. Created as divs, they were silently not selects, and the check that the
// toggle and the select are one setting passed against a stub that could not disagree.
byId.lang = element('md-outlined-select');
// The dialog holds the controls it holds; a test asks the form what is in it.
byId.prefsForm.append(byId.lang);
for (const id of ['tabFleet', 'tabSkills', 'tabBoard', 'tabMcp']) byId.tabs.append(byId[id]);
// The companions tab holds a label element beside its badge, so the word can be rewritten without
// taking the badge with it. Mirrored here for the same reason the rail's labels are.
{ const wrap = element('span'); wrap.className = 'tablbl'; const l = element('span'); l.className = 'lbl'; wrap.append(l); wrap.append(byId.tabBadge); byId.tabFleet.append(wrap); }
for (const id of ['railFleet', 'railSkills', 'railBoard', 'railMcp']) {
  byId.railNav.append(byId[id]);
  // The label is markup, not something the module creates: paint() writes into it by class.
  const lbl = element('span');
  lbl.className = 'lbl';
  byId[id].append(lbl);
}

globalThis.document = {
  title: "",
  // Text nodes are elements with only text here — the page appends them beside <br> to stack a
  // host name over its address, and the fake only has to make .text come out right.
  createTextNode(t) { const n = element('#text'); n.textContent = t; return n; },
  // The page measures its dock and writes the height into a custom property; a fake that cannot be
  // written to would throw where the real one shrugs.
  // The root carries the theme the reader chose, as an attribute the stylesheet's override blocks
  // read. Written to and removed from, because 'follow the system' is the ABSENCE of it.
  documentElement: {
    style: { setProperty(k, v) { this[k] = v; } },
    attrs: {},
    setAttribute(k, v) { this.attrs[k] = String(v); },
    getAttribute(k) { return this.attrs[k] ?? null; },
    removeAttribute(k) { delete this.attrs[k]; },
  },
  body: Object.assign(element('body'), { offsetHeight: 400, scrollHeight: 400 }),
  createElement: element,
  getElementById(id) {
    if (!(id in byId)) throw new Error('the page looked up #' + id + ', which the markup does not have');
    return byId[id];
  },
};
globalThis.window = { innerHeight: 800, scrollY: 0, scrollTo() {} };
// The address bar, enough of it. pushState WRITES the search string, because the page navigates
// by pushing a url and re-reading it — a no-op stub made every tab click look like it did nothing,
// which is a fake that answers "broken" for a page that works.
// A browser has these and the page reads both to pick a language; without them it throws before
// drawing anything, which is a fake missing a fact rather than a page doing something wrong.
// The server inlines the English pack ahead of the page's script; the harness does the same, so a
// test sees the words a browser sees on the first paint.
globalThis.__LANG = JSON.parse(process.env.LANG_PACK ?? '{}');

globalThis.localStorage = {
  store: new Map(),
  getItem(k) { return this.store.has(k) ? this.store.get(k) : null; },
  setItem(k, v) { this.store.set(k, String(v)); },
  removeItem(k) { this.store.delete(k); },
};
// navigator already exists in node and is read-only, so the languages are defined onto it rather
// than replacing it.
//
// Both, because the page reads the LIST. A browser set to Korean with English after it reports
// navigator.language = 'ko-KR' AND navigator.languages = ['ko-KR', 'en-US'], and a fake that
// carried only the first could not tell a page that reads one from a page that reads the other.
const tags = (process.env.LANG_TAGS ?? process.env.LANG_TAG ?? 'en-US').split(',');
Object.defineProperty(globalThis.navigator, 'language', {value: tags[0], configurable: true});
Object.defineProperty(globalThis.navigator, 'languages', {value: tags, configurable: true});

globalThis.location = { search: process.env.QUERY ?? '', pathname: process.env.BASE ?? '/' };
globalThis.history = {
  pushState(_state, _title, url) {
    const q = String(url).indexOf('?');
    globalThis.location.search = q < 0 ? '' : String(url).slice(q);
  },
};
globalThis.addEventListener = () => {};
// Two queries are asked now and they mean different things: hover:none is a touch screen, and the
// width one is the layout breakpoint. A stub answering both with the same flag made a phone-sized
// test claim a desktop layout.
globalThis.matchMedia = (q) => ({
  matches: String(q).includes('hover') ? process.env.TOUCH === '1'
                                       : process.env.NARROW === '1',
});
// Timers are recorded rather than run: a test needs to know that the page ARMED a poll — the one
// on an agent's page is how the prompt it is blocked on ever reaches the browser — without the
// suite then waiting three seconds for it.
let timerID = 0;
globalThis.setInterval = (fn, ms) => { RENDERED.push({ interval: ms }); return ++timerID; };
globalThis.clearInterval = () => {};
globalThis.setTimeout = () => 0;
globalThis.EventSource = class {
  constructor(url) { RENDERED.push({ subscribed: url }); this.close = () => {}; }
};
// Every fetch is answered from the environment, so a scenario is one JSON blob and no server.
globalThis.fetch = async (path, init) => {
  RENDERED.push({ fetched: path, method: init?.method ?? 'GET', body: init?.body?.toString() ?? '' });
  // The language pack is the one route with a shape of its own: an object, not the scenario's list.
  // Answered with the REAL English pack, so a test asserting on a label is asserting on the words
  // this binary ships rather than on a fixture that can drift from them.
  //
  // Matched on the tail, not on a leading '/i18n/'. The page builds this url from where it is
  // mounted, so under a base path it asks for /magi/i18n/… — a prefix match would have handed that
  // request the fleet's LIST, which the page rejects for not being an object, and every label would
  // have quietly stayed at its seeded value with nothing failing.
  if (/i18n\/language\.[a-z]{2}\.json$/.test(String(path))) {
    const pack = JSON.parse(process.env.LANG_PACK ?? '{}');
    return { ok: true, status: 200, json: async () => pack, text: async () => JSON.stringify(pack) };
  }
  return { ok: true, status: 200, json: async () => JSON.parse(process.env.FLEET_JSON ?? '[]'), text: async () => '' };
};

export { byId, RENDERED, element };
