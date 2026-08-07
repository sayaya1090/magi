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
    set textContent(v) { this._text = String(v); },
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
    set value(v) { this.attrs.value = v; },
    get value() { return this.attrs.value ?? ''; },
    append(...kids) { this.children.push(...kids); },
    replaceChildren(...kids) { this.children = kids; },
    addEventListener() {},
    requestSubmit() {},
    // Attributes the console's controls set: a pressed state on the filter tiles, a disabled tile
    // for a count of zero, a title on the stop button.
    setAttribute(k, v) { this.attrs[k] = String(v); },
    getAttribute(k) { return this.attrs[k]; },
    // text is everything this node and its descendants would show.
    get text() {
      return [this._text, ...this.children.map((k) => k.text)].join(' ').replace(/\s+/g, ' ').trim();
    },
    // find collects descendants (and self) of a tag — an array, so .find(fn) on the result is
    // Array.prototype.find and a test can pick a node by class.
    find(t) {
      const out = this.tag === t ? [this] : [];
      for (const k of this.children) out.push(...k.find(t));
      return out;
    },
  };
  return node;
}

const byId = {};
for (const id of ['fleet', 'log', 'state', 'sid', 'back', 'f', 't', 'stop', 'prompt', 'dock', 'summary', 'detail', 'crumbSep', 'crumbHere', 'ivs', 'tabs', 'tabFleet', 'tabIv', 'skills', 'tabSkills']) byId[id] = element('div');

globalThis.document = {
  title: "",
  // Text nodes are elements with only text here — the page appends them beside <br> to stack a
  // host name over its address, and the fake only has to make .text come out right.
  createTextNode(t) { const n = element('#text'); n.textContent = t; return n; },
  // The page measures its dock and writes the height into a custom property; a fake that cannot be
  // written to would throw where the real one shrugs.
  documentElement: { style: { setProperty(k, v) { this[k] = v; } } },
  body: { offsetHeight: 400, scrollHeight: 400 },
  createElement: element,
  getElementById(id) {
    if (!(id in byId)) throw new Error('the page looked up #' + id + ', which the markup does not have');
    return byId[id];
  },
};
globalThis.window = { innerHeight: 800, scrollY: 0, scrollTo() {} };
globalThis.location = { search: process.env.QUERY ?? '' };
globalThis.history = { pushState() {} };
globalThis.addEventListener = () => {};
globalThis.matchMedia = () => ({ matches: process.env.TOUCH === '1' });
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
  return { ok: true, status: 200, json: async () => JSON.parse(process.env.FLEET_JSON ?? '[]'), text: async () => '' };
};

export { byId, RENDERED, element };
