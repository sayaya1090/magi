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
    _on: {},
    attrs: {},
    style: {},
    _class: '',
    _text: '',
    set className(v) { this._class = String(v); },
    get className() { return this._class; },
    // classList over the same string the page reads back through className, so a test asserting on
    // one sees what the other did. Only the methods this page uses; a fuller fake would be
    // inventing a DOM nobody is calling.
    classList: {
      add(...c) { const n = this._n; n._class = [...new Set(n._class.split(/\s+/).filter(Boolean).concat(c))].join(' '); },
      remove(...c) { const n = this._n; n._class = n._class.split(/\s+/).filter(x => x && !c.includes(x)).join(' '); },
      contains(c) { return this._n._class.split(/\s+/).includes(c); },
      // The fourth, now that the page needs it: a class that says one thing and is set from a
      // condition. Written against add/remove so the three stay the single definition of what a
      // class list does here, and taking the second argument, because the caller that wanted this
      // passes one and a toggle that ignored it would flip on every render.
      toggle(c, on) {
        const want = on === undefined ? !this.contains(c) : !!on;
        if (want) this.add(c); else this.remove(c);
        return want;
      },
    },
    // Reading it is what forces a reflow in a browser; here it only has to exist.
    offsetWidth: 0,
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
    // A height, because there is no layout here and code that measures one has to be exercisable.
    // A constant rather than 0: zero is a number the page treats as "nothing to stand in for", so
    // a fake reporting it would quietly turn every height calculation into a no-op and pass.
    // Writable, because the body's is set to a page-sized number where this fake is assembled.
    offsetHeight: 24,
    // parentNode is set on append, because the page asks a CHILD which box it is in. A fake that
    // answered undefined would let a guard pass that a browser fails — and this fake already did
    // the mirror of that: children is an array here and an HTMLCollection there, so a page written
    // against it used indexOf, which threw in a browser and silently dropped a whole panel.
    append(...kids) {
      for (const k of kids) { if (k && typeof k === 'object') k.parentNode = this; }
      this.adopt(kids);
      this.children.push(...kids);
    },
    // Removing one child, which is how the transcript drops the tail it is about to rebuild.
    // Absent here, a page that stopped replacing everything every frame threw instead — the fake
    // has to answer the DOM calls the page makes, not the ones it used to make.
    removeChild(kid) {
      const i = this.children.indexOf(kid);
      if (i >= 0) this.children.splice(i, 1);
      if (kid && typeof kid === 'object') kid.parentNode = null;
      return kid;
    },
    // Taking ITSELF out, which is what a node does when the thing that owns it is not in reach.
    // The report-format editor's row-delete has always called this, and the composer's button asks
    // for it when its mark is exchanged for another; the fake had only removeChild, so both threw
    // — the tenth place this stand-in has been narrower than the DOM it stands in for.
    remove() {
      const p = this.parentNode;
      if (p && typeof p.removeChild === 'function') p.removeChild(this);
      else this.parentNode = null;
    },
    // Taken out of wherever it was, first. The DOM MOVES a node: appending one that already has a
    // parent removes it from that parent, and nothing about the tree can be in two places. This
    // fake kept it in both — so a node could end up inside its own descendant, and a walk over the
    // tree recursed until the stack ran out. Found by the attribute selector added for data-may;
    // it had been true for every append before that and only ever cost duplicates nobody counted.
    adopt(kids) {
      for (const k of kids) {
        if (k && typeof k === 'object' && k.parentNode && k.parentNode !== this &&
            typeof k.parentNode.removeChild === 'function') {
          k.parentNode.removeChild(k);
        }
      }
    },
    // Inserting at the FRONT, which is how the transcript puts the stand-in for what is above it.
    prepend(...kids) {
      this.adopt(kids);
      for (const k of kids) { if (k && typeof k === 'object') k.parentNode = this; }
      this.children.unshift(...kids);
    },
    // Inserting BEFORE a child, which is how the report-format editor keeps its add control at the
    // bottom while rows arrive above it. A missing node appends, as the DOM does.
    insertBefore(kid, before) {
      this.adopt([kid]);
      if (kid && typeof kid === 'object') kid.parentNode = this;
      const i = this.children.indexOf(before);
      if (i < 0) this.children.push(kid);
      else this.children.splice(i, 0, kid);
      return kid;
    },
    // Standing aside for what takes its place, which is how a control that has been used becomes
    // the thing it opened. The DOM puts the new nodes AT THIS INDEX and then takes this one out,
    // so order is preserved — appending them and removing itself would move them to the end, and a
    // "write a different answer" button would open its field somewhere other than where it stood.
    // The eleventh gap between this stand-in and the DOM it stands in for.
    replaceWith(...kids) {
      const p = this.parentNode;
      if (!p) return;
      p.adopt(kids);
      for (const k of kids) { if (k && typeof k === 'object') k.parentNode = p; }
      const i = p.children.indexOf(this);
      if (i < 0) p.children.push(...kids);
      else p.children.splice(i, 1, ...kids);
      this.parentNode = null;
    },
    replaceChildren(...kids) {
      this.adopt(kids);
      for (const k of kids) { if (k && typeof k === 'object') k.parentNode = this; }
      this.children = kids;
    },
    // Listeners are kept and dispatched, not swallowed. A no-op here made the fake disagree with
    // the DOM in the direction that hides bugs: md-tabs reports a switch by firing 'change' and
    // nothing else, so a page that listened for it looked correct while doing nothing at all.
    addEventListener(type, fn) { (this._on[type] || (this._on[type] = [])).push(fn); },
    removeEventListener(type, fn) {
      const l = this._on[type];
      if (l) this._on[type] = l.filter(x => x !== fn);
    },
    dispatchEvent(e) {
      for (const fn of this._on[e && e.type] || []) fn.call(this, e);
      return true;
    },
    // The namespaced setter, which an <svg><use> needs for the xlink spelling Safari wanted for
    // years. Same store as setAttribute: nothing here reads a namespace back, and a second map
    // would be a fake being more of a DOM than the page can tell.
    setAttributeNS(_ns, k, v) { this.attrs[k] = String(v); },
    requestSubmit() {},
    focus() {},
    // Recorded rather than ignored: where the page decided to move the view is a decision worth
    // asserting on, and a row is REBUILT by the poll that follows an answer — so a test cannot
    // patch the element it clicked and expect to still be holding it.
    scrollIntoView() { SCROLLED.push(this.attrs.href || this.className || ''); },
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
    // dataset, over the same attribute bag a browser keeps it in, so a page that writes
    // el.dataset.kind and a test that reads getAttribute('data-kind') see one value. A plain object
    // would have been two stores agreeing by luck, which is the shape this fake keeps being wrong
    // in — and having no dataset at all is how a page that used one threw here and nowhere else.
    get dataset() {
      const attrs = this.attrs;
      const key = (p) => 'data-' + String(p).replace(/[A-Z]/g, (c) => '-' + c.toLowerCase());
      return new Proxy({}, {
        get: (_, p) => attrs[key(p)],
        set: (_, p, v) => { attrs[key(p)] = String(v); return true; },
        has: (_, p) => key(p) in attrs,
        deleteProperty: (_, p) => { delete attrs[key(p)]; return true; },
      });
    },
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
    // The plural, for the same one shape — and for a compound class like ".card.waiting", which is
    // how the page finds "a row that is a card AND is waiting". Returns an array, which is a NodeList
    // in a browser; the page only ever iterates it, and a test guards against it doing more.
    // ⚠ And a bare TAG, which is the other shape the page uses and the one this did not have. It
    // answered a tag selector by splitting on "." into a single class name, matching nothing, and
    // returning an empty array — so a form that reads its own fields with querySelectorAll found
    // none and posted an empty body, silently. A selector engine this small has to say which
    // shapes it knows.
    querySelectorAll(sel) {
      const s = String(sel).trim();
      // [attribute], the third shape: it is how the page finds the drawings that name a symbol they
      // would rather be. Same answer as the document-wide one next to byId, and the same reason for
      // being this small — a selector engine that guesses is a fake that flatters.
      const attr = /^\[([a-z-]+)\]$/.exec(s);
      if (attr) {
        const out = [];
        const walk = n => {
          if (!n || typeof n !== 'object') return;
          if (n.attrs && n.attrs[attr[1]] !== undefined) out.push(n);
          for (const k of n.children || []) walk(k);
        };
        walk(this);
        return out;
      }
      if (!s.startsWith('.')) {
        if (/[.#\[\s>]/.test(s)) throw new Error('the fake only does a bare tag or a class chain, not ' + s);
        const out = [];
        const walk = (n) => { for (const k of n.children) { if (k.tag === s) out.push(k); walk(k); } };
        walk(this);
        return out;
      }
      const want = s.split('.').filter(Boolean);
      const hit = (n) => {
        const have = String(n.className || '').split(/\s+/);
        return want.every((w) => have.includes(w));
      };
      const out = [];
      const walk = (n) => {
        for (const k of n.children) {
          if (hit(k)) out.push(k);
          walk(k);
        }
      };
      walk(this);
      return out;
    },
    find(t) {
      const hit = typeof t === 'function' ? t(this) : this.tag === t;
      const out = hit ? [this] : [];
      for (const k of this.children) out.push(...k.find(t));
      return out;
    },
  };
  node.classList = Object.create(node.classList);
  node.classList._n = node;
  return node;
}

// clicky matches a control by role rather than by tag. The same button is `md-text-button` in one
// place and `md-filled-button` in another — both are Material Web's, both are the thing a person
// presses — and a test that asked for `button` would report an empty card rather than a restyled
// one. Global so every snippet gets it without threading a helper through.
// True while md-tabs is the one changing the selection. See the active setter.
let byTabs = false;

globalThis.clicky = (n) => n.tag === 'button' || n.tag.endsWith('-button');

// Every id the MARKUP carries, scraped from it by the harness and written in beside this file.
//
// It used to be a list kept here by hand, and keeping it was the tax on adding any element at all:
// the way it told you it was short was an error in an unrelated test, naming the lookup rather
// than the change. The markup's ids are exactly the set the page can ask for.
import { MARKUP_IDS, MARKUP_MAY } from './ids.mjs';
const byId = {};
for (const id of MARKUP_IDS) byId[id] = element('div');
// The one attribute the markup writes and the page reads back. Everything else a stub carries was
// put there by the page itself; this one arrives from the HTML, and without it the permission gate
// looked as though it covered nothing at all.
for (const [id, need] of Object.entries(MARKUP_MAY || {})) {
  if (byId[id]) byId[id].attrs['data-may'] = need;
}
// A dialog opens, closes, and remembers which button closed it. The page reads returnValue to tell
// a cancel from a confirm, and a fake without it makes every cancel look like a confirm — which is
// the one thing a dialog must never get wrong.
for (const id of MARKUP_IDS.filter(i => i.toLowerCase().endsWith('dialog'))) {
  Object.assign(byId[id], {
    open: false, returnValue: '',
    show() { this.open = true; },
    close(v) { this.open = false; if (v !== undefined) this.returnValue = String(v); },
  });
}
// The four tabs are children of #tabs in the markup, and md-tabs works through that relationship:
// it activates by index into its own children. A flat bag of ids would let the page set an index
// nothing answers to, and every tab would read as unselected.
// The two preference selects are md-outlined-select, and the fake mirrors a select's value→option
// resolution by tag. Created as divs, they were silently not selects, and the check that the
// toggle and the select are one setting passed against a stub that could not disagree.
byId.lang = element('md-outlined-select');
// The dialog holds the controls it holds; a test asks the form what is in it.
byId.prefsForm.append(byId.lang);
for (const id of ['tabFleet', 'tabSkills']) byId.tabs.append(byId[id]);
// md-tabs answers activeTabIndex from its CHILDREN, so a strip whose tabs were never appended
// reports -1 for every selection and the page reads that as "the first one".
for (const id of ['ptabTalk', 'ptabState']) byId.ptabs.append(byId[id]);
// The companions tab holds a label element beside its badge, so the word can be rewritten without
// taking the badge with it. Mirrored here for the same reason the rail's labels are.
{ const wrap = element('span'); wrap.className = 'tablbl'; const l = element('span'); l.className = 'lbl'; wrap.append(l); wrap.append(byId.tabBadge); byId.tabFleet.append(wrap); }
for (const id of ['railFleet', 'railSkills']) {
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
  // Namespaced elements are the same fake element here. The page builds <svg> and <use> this way
  // because a browser demands it; nothing in these tests reads the namespace back.
  createElementNS(_ns, tag) { return element(tag); },
  // A sweep of the document, which here finds only what this harness actually holds.
  //
  // The fake has no tree built from the markup — it has the ids the page looks up and whatever the
  // page has since created — so a page-wide query returns the created nodes and none of the eight
  // drawings that live in page.html. That is the honest answer for this environment rather than an
  // invented one: what dressIcons() does to those eight is checked in a browser, and what it does
  // when there is no sprite (nothing) is what these tests exercise.
  //
  // Only the [attribute] form, because that is the only selector the page hands it. Anything else
  // throws rather than quietly matching nothing, which is how a fake stops flattering the page.
  querySelectorAll(sel) {
    const m = /^\[([a-z-]+)\]$/.exec(String(sel));
    if (!m) throw new Error('the fake DOM only understands [attribute] selectors, not ' + sel);
    const out = [];
    // Each node once, however it got where it is. A real tree cannot contain itself, but a TEST
    // can assign `children` directly — one does, to stand the pane up from the markup's ids — and
    // the walk then followed its own tail until the stack ran out. Every id is also a root here,
    // so a node reachable twice would be reported twice even without a cycle.
    const seen = new Set();
    const walk = n => {
      if (!n || typeof n !== 'object' || seen.has(n)) return;
      seen.add(n);
      if (n.attrs && n.attrs[m[1]] !== undefined) out.push(n);
      for (const k of n.children || []) walk(k);
    };
    for (const k of Object.values(byId)) walk(k);
    return out;
  },
  getElementById(id) {
    // The icon symbols are allowed to be missing, and are missing HERE: they come from a sprite
    // baked into the binary at build time from a licensed download, and this harness runs the page
    // against markup with no sprite in it. The page is written to ask before drawing one, so the
    // fake answers "no" the way a browser would rather than throwing — which is the whole state
    // being tested. Every other unknown id is still a typo and still throws.
    if (id.startsWith('i-')) return byId[id] ?? null;
    if (!(id in byId)) throw new Error('the page looked up #' + id + ', which the markup does not have');
    return byId[id];
  },
};
// The page asks `'Notification' in window` and reads window.isSecureContext, so what a browser
// puts on window has to be here too — a bare object made the notifications switch throw before it
// reached anything, and an async handler swallows that into a rejected promise nobody awaits.
globalThis.window = {
  innerHeight: 800, scrollY: 0, scrollTo() {},
  isSecureContext: true,
  get Notification() { return globalThis.Notification; },
  get PushManager() { return globalThis.PushManager; },
};
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
// A getter, because the worker fake is defined further down and a value read here would be
// undefined — which is how `navigator.serviceWorker.getRegistration` throws on a fake that looks
// present.
Object.defineProperty(globalThis.navigator, 'serviceWorker', {
  get() { return globalThis.__sw; }, configurable: true});

globalThis.location = { search: process.env.QUERY ?? '', pathname: process.env.BASE ?? '/' };
globalThis.history = {
  pushState(_state, _title, url) {
    const q = String(url).indexOf('?');
    globalThis.location.search = q < 0 ? '' : String(url).slice(q);
  },
};
globalThis.addEventListener = () => {};
// Enough of the notification surface to drive the switch, and no more. ORDER records which of these
// the page reached first, because the ONE thing that matters about this flow is not what it calls
// but WHEN: requestPermission needs transient user activation, and an await before it spends the
// activation — the call then resolves 'default' having shown nobody a prompt, which is what "the
// button does nothing" looks like from outside.
globalThis.ORDER = [];
globalThis.SCROLLED = [];
globalThis.isSecureContext = true;
globalThis.Notification = {
  permission: process.env.PERM || 'default',
  requestPermission() {
    ORDER.push('requestPermission');
    return Promise.resolve(process.env.PERM_ANSWER || 'denied');
  },
};
globalThis.PushManager = function () {};
const swReg = {
  pushManager: {
    getSubscription() { ORDER.push('getSubscription'); return Promise.resolve(null); },
    subscribe() { ORDER.push('subscribe'); return Promise.resolve(null); },
  },
};
// Added TO the existing navigator, not over it: the language properties are defined on it further
// down, and replacing the object took them with it.
globalThis.__sw = {
  getRegistration() { ORDER.push('getRegistration'); return Promise.resolve(swReg); },
  register() { ORDER.push('register'); return Promise.resolve(swReg); },
  get ready() { return Promise.resolve(swReg); },
};
// Two queries are asked now and they mean different things: hover:none is a touch screen, and the
// width one is the layout breakpoint. A stub answering both with the same flag made a phone-sized
// test claim a desktop layout.
globalThis.matchMedia = (q) => {
  const s = String(q);
  // A min-width query asks "is the window at least this wide", so under NARROW it is FALSE. Written
  // as `matches: NARROW` it answered yes to exactly the question a narrow screen answers no to —
  // harmless while the only width query was in CSS, and wrong the moment the page asked one.
  const matches = s.includes('hover') ? process.env.TOUCH === '1'
                : s.includes('min-width') ? process.env.NARROW !== '1'
                : process.env.NARROW === '1';
  // A media query is an EventTarget. The page listens for the breakpoint being crossed, which is
  // how a window dragged across it re-lays out; a stub without these throws on the way past.
  return {matches, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}};
};
// Timers are recorded rather than run: a test needs to know that the page ARMED a poll — the one
// on an agent's page is how the prompt it is blocked on ever reaches the browser — without the
// suite then waiting three seconds for it.
let timerID = 0;
globalThis.setInterval = (fn, ms) => { RENDERED.push({ interval: ms }); return ++timerID; };
globalThis.clearInterval = () => {};
globalThis.setTimeout = () => 0;
// Run straight away rather than at a frame boundary. The page uses it to let a redraw land before
// it scrolls, and a fake that swallowed the callback would let a test pass over a scroll that
// never happens — while one that never called it at all made the caller throw.
globalThis.requestAnimationFrame = (fn) => { fn(); return 0; };
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
  // A route a test has answered for itself. The default below hands every path the fleet's list,
  // which is right for the page's own poll and wrong for anything with a shape of its own — a
  // strip fed the fleet array finds no children in it and hides, which looks exactly like a
  // companion with nothing running.
  const route = String(path).split('?')[0];
  if (Object.prototype.hasOwnProperty.call(globalThis.ROUTES, route)) {
    const body = globalThis.ROUTES[route];
    return { ok: true, status: 200, json: async () => body, text: async () => JSON.stringify(body) };
  }
  // The fleet is parsed ONCE and handed out by reference, so a test can change what the next poll
  // will report — a companion going quiet, or leaving the list — which is a thing that happens and
  // could not be written down before.
  return { ok: true, status: 200, json: async () => globalThis.FLEET, text: async () => '' };
};
// Answers a test can set per route, by path without its query.
globalThis.ROUTES = {};
// What the page has copied, in order. The clipboard is a browser capability and there is none
// here; a fake that threw would make a copy control look broken, and one that swallowed silently
// would let a control that copies the WRONG thing pass.
globalThis.CLIPBOARD = [];
globalThis.FLEET = JSON.parse(process.env.FLEET_JSON ?? '[]');
// Defined onto the existing navigator, which node supplies and does not let you replace.
Object.defineProperty(globalThis.navigator, 'clipboard', {
  configurable: true,
  value: { writeText: async t => { globalThis.CLIPBOARD.push(String(t)); } },
});

export { byId, RENDERED, element };
