package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The console as a static page, answered by a mock magi in the browser.
//
// # Why this exists
//
// Nobody can look at this screen without running a daemon, a console and at least one companion.
// That is a lot of setup to answer "what does it look like", and it means a change to the front end
// is reviewed as a diff of a Go string. A published demo turns the review into looking at it.
//
// # Why the mock is in the browser
//
// GitHub Pages serves files, not processes, so there is no /fleet to fetch. The demo replaces the
// page's `fetch` with one that answers from a fixture — the same trick the render tests use under
// node, for the same reason: the page's REAL javascript runs, against data shaped like the real
// handlers' output.
//
// The page itself is not copied or edited. This writes indexHTML verbatim and appends one script,
// so a demo can never drift from what the binary serves: there is only one page.
//
// # What it proves, and what it does not
//
// It proves the markup, the styling and the page's own logic — the states, the filters, the tabs,
// the forms, how it behaves on a phone. It does not prove a single handler: every answer is a
// fixture. The Go tests are what check the handlers, and the workflow runs those first, so a demo
// that deploys is a demo whose server side passed its own tests.
const demoScript = `
<script>
// A mock magi, in the page. Every answer below is shaped like the handler it stands in for; the
// page's own code is untouched and does not know the difference.
(() => {
  // The language packs are NOT inlined here. They were, as a Go string literal holding a copy of
  // language.en.json, and a copy is a thing that drifts — this demo would have kept showing the
  // old wording of a label the day somebody changed the real pack. The emitted directory carries
  // the real files and the fetch below hands their requests to the network untouched, so the demo
  // switches language exactly the way the console does, Korean included.
  // RFC3339 without the fractional seconds, because that is what the handlers emit
  // (time.RFC3339) — a fixture that carries milliseconds shows a timestamp shape the real console
  // never produces, and the demo is supposed to be evidence about the real one.
  const now = new Date().toISOString().replace(/\.\d+Z$/, 'Z');
  const fleet = [
    {socket: '/demo/design.sock', name: 'design', role: 'the design system: component specs and visual review',
     team: 'frontend', hub: true, workdir: '/Users/you/work/design-system', session: 'd1',
     state: 'working', live: true, task: 'spec the empty state for the fleet table, and name the exact tokens',
     steps: 7, planDone: 2, planTotal: 5, idle: 12, host: 'studio', addr: '10.0.0.4', pid: 4127},
    {socket: '/demo/api.sock', name: 'api', role: 'the billing API and its contracts',
     team: 'backend', workdir: '/Users/you/work/billing', session: 'a1',
     state: 'waiting', live: true, asking: 'run: psql -c "drop table staging_invoices"',
     askId: 'call_42', askKind: 'permission', task: 'add the idempotency key', steps: 3,
     planDone: 1, planTotal: 4, idle: 4, host: 'studio', addr: '10.0.0.4', pid: 4128},
    // A question rather than a permission, so the demo shows the other prompt and the report a
    // person is meant to decide on. Its sections are the default contract's — a console whose
    // operator has written their own decision-report skill would show theirs.
    {socket: '/demo/design.sock2', name: 'palette', role: 'colour and type', team: 'frontend',
     workdir: '/Users/you/work/design-system', session: 'p1', state: 'waiting', live: true,
     asking: 'which surface should the empty state sit on?',
     askId: 'call_51', askKind: 'question',
     report: [
       {key: 'tried', text: 'drew it on surface and on surface-container-low, both themes; measured 4.7:1 and 6.1:1 against the muted label'},
       {key: 'stakes', text: 'surface matches the table around it but the empty state stops reading as a panel; the container reads as a panel and is one more layer to keep in step with the cards'},
       {key: 'lean', text: 'surface-container-low — the contrast is the one with headroom, and light is already the tighter theme'},
     ],
     task: 'spec the empty state', steps: 5, idle: 22, host: 'studio', addr: '10.0.0.4', pid: 4131},
    {socket: '/demo/buttons.sock', name: 'buttons', role: 'components', team: 'frontend',
     workdir: '/Users/you/work/ui-kit', session: 'b1', state: 'idle', live: true,
     task: 'the toggle now reads its state from the store rather than a prop', idle: 640,
     host: 'studio', addr: '10.0.0.4', pid: 4129},
    {socket: '/demo/ops.sock', name: 'ops', role: 'deploys and alerting', workdir: '/Users/you/work/infra',
     session: 'o1', state: 'stopped', live: false, task: 'rotated the staging certificates', idle: 90000,
     host: 'mini', addr: '10.0.0.9'},
  ];
  const answers = {
    '/fleet': fleet,
    // Which machine this console is. A demo that left it blank would be showing the drawer with a
    // hole in it, and the hole is the part that answers "am I looking at the right one".
    '/console': {host: 'studio', configDir: '/Users/you/.config/magi',
                 peers: ['mini', 'laptop']},
    '/interventions': [
      {companion: 'design', socket: '/demo/design.sock', kind: 'steer', afterSec: 8,
       text: 'use the tokens from the scale, not hand-written spacing', at: now},
      {companion: 'buttons', socket: '/demo/buttons.sock', kind: 'steer', afterSec: 1140,
       text: 'use the tokens from the scale, not hand-written spacing', at: now},
      {companion: 'api', socket: '/demo/api.sock', kind: 'denied', afterSec: 95,
       text: 'call_31', at: now},
    ],
    '/skills': [
      {name: 'skill-tests-before-done', kind: 'skill', tier: 'global', observed: 6,
       firstSeen: '2026-06-30', lastSeen: '2026-08-07',
       description: 'run the tests before saying it is done',
       body: 'Run the project\'s own test command and read the output before reporting a task '
           + 'finished. A build that compiles is not a test that passed, and "it should work" is '
           + 'the sentence that precedes every regression in this repository.\n\n'
           + 'If the tests cannot be run, say so and say why, rather than landing the work quietly.'
           + '\n\n(source: agent)'},
      {name: 'skill-tokens', kind: 'skill', tier: 'project', companion: 'design',
       socket: '/demo/design.sock', observed: 3, firstSeen: '2026-07-14', lastSeen: '2026-08-06',
       description: 'spacing comes from the scale, never hand-written',
       body: 'Every margin and padding is a token from the spacing scale. A hand-written value is '
           + 'one more thing to keep in step with the rest, and it will not be.\n\n(source: agent)'},
      {name: 'mem-staging', kind: 'memory', tier: 'project', companion: 'api',
       socket: '/demo/api.sock', observed: 1, lastSeen: '2026-08-05', tags: ['ops'],
       description: 'the staging database is restored from prod every Monday'},
    ],
    '/mcp': [
      {name: 'docs', tier: 'global', url: 'http://localhost:3000/mcp', file: '~/.config/magi/config.toml'},
      {name: 'figma', tier: 'project', companion: 'design', socket: '/demo/design.sock',
       command: 'npx', args: ['-y', 'figma-mcp'], envNames: ['FIGMA_TOKEN'],
       file: '/Users/you/work/design-system/.magi/config.toml'},
    ],
    '/context': {model: 'qwen3-coder-next', window: 128000, used: 104300, estimated: false,
      messages: 61, compactions: 2, shed: 39000, lastBefore: 48000, lastAfter: 9000,
      lastAt: now, topics: ['internal/adapter/fleet/fleet.go', 'cmd/magi-web/page.go', 'discussion']},
    '/plan': [
      {content: 'read what the empty states do now', status: 'completed'},
      {content: 'write the spec', status: 'completed'},
      {content: 'name the tokens it uses', status: 'in_progress'},
      {content: 'get it reviewed by buttons', status: 'pending'},
      {content: 'fold it into the component docs', status: 'pending'},
    ],
    // What this companion has done before now — the store's own session list, newest first.
    '/history': [
      {id: 'd1', title: 'spec the empty state for the fleet table, and name the exact tokens',
       ago: 0, current: true},
      {id: 'c9', title: 'audit the button emphasis against the M3 scale and fix the inversions',
       ago: 5400},
      {id: 'c4', title: 'the filter chips are not reachable with a keyboard on the corrections page',
       ago: 93000},
      {id: 'b7', title: 'move the palette into styles.go so the two surfaces cannot drift',
       ago: 260000},
    ],
    '/handoffs': [
      {from: 'design', to: 'buttons', socket: '/demo/buttons.sock', state: 'idle',
       request: 'make the toggle read its state from the store',
       answer: 'the toggle now reads its state from the store rather than a prop'},
      {from: 'design', to: 'api', socket: '/demo/api.sock', state: 'waiting',
       request: 'confirm the invoice endpoint is idempotent'},
    ],
  };
  const banner = document.createElement('div');
  banner.className = 'demo-banner';
  banner.textContent = 'demo — the real page, answered by a mock. Nothing here is a running agent, ' +
                       'and every action reports what it would have sent.';
  addEventListener('DOMContentLoaded', () => document.body.prepend(banner));

  // Kept before the mock takes over: the language packs are real files sitting beside this page,
  // and they are the one thing here that must NOT be answered by a fixture.
  const realFetch = globalThis.fetch.bind(globalThis);
  globalThis.fetch = async (path, init) => {
    const url = String(path).split('?')[0];
    if (/i18n\/language\.[a-z]{2}\.json$/.test(url)) return realFetch(path);
    if (init && init.method === 'POST') {
      // Actions say what they would have done rather than pretending they did it: a demo that
      // silently accepts a delete teaches the wrong thing about the real console.
      const body = init.body ? ' ' + init.body.toString() : '';
      banner.textContent = 'demo — would have sent: POST ' + url + body;
      return {ok: true, status: 204, text: async () => ''};
    }
    const body = answers[url];
    if (body === undefined) return {ok: false, status: 404, json: async () => [], text: async () => ''};
    return {ok: true, status: 200, json: async () => body, text: async () => JSON.stringify(body)};
  };
  // The transcript arrives over SSE, which a static host cannot serve either.
  globalThis.EventSource = class {
    constructor() {
      setTimeout(() => this.onmessage && this.onmessage({data: JSON.stringify([
        {who: 'user', text: 'spec the empty state for the fleet table, and name the exact tokens'},
        {who: 'assistant', text: 'Reading what the empty states do today.'},
        {who: 'tool', text: 'grep "empty" cmd/magi-web/page.go'},
        {who: 'result', text: 'page.go:612  e.innerHTML = \'Nothing learned yet.<br>\''},
        {who: 'assistant', text: 'Three of them, and none says what would be there. Writing the spec.'},
      ])}), 60);
    }
    close() {}
  };
})();
</script>
<style>
  .demo-banner {
    position:sticky; top:0; z-index:50; padding:.55rem 1.2rem;
    background:var(--primaryContainer); color:var(--fg);
    font:600 11px/1.5 var(--mono); letter-spacing:.06em; border-bottom:1px solid var(--outlineVariant);
  }
</style>
`

// emitDemo writes the static demo into dir.
//
// The page verbatim plus the mock, and a .nojekyll so a host that would otherwise hide files
// starting with an underscore does not eat part of the site. Nothing here is templated: the moment
// this function edits the page, the demo stops being evidence about the page.
func emitDemo(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Root-relative becomes relative, for the demo only.
	//
	// The binary serves the page AT the root, so `/vendor/rxjs.js` is right there. GitHub Pages
	// serves a project site under /<repo>/, where a leading slash escapes to the domain root and
	// 404s — the module import among them, which means the script never runs and the page is
	// blank. Shipped exactly that way, and a local check that served the directory at the root
	// said everything was fine, which is how it got past.
	//
	// Only the prefixes the page loads are rewritten. Navigation hrefs keep their absolute form:
	// they are pushState targets read by the page's own router, not fetches.
	page := indexHTML
	for _, prefix := range []string{"/vendor/", "/font/"} {
		page = strings.ReplaceAll(page, "'"+prefix, "'."+prefix)
		page = strings.ReplaceAll(page, `"`+prefix, `".`+prefix)
		page = strings.ReplaceAll(page, "url("+prefix, "url(."+prefix)
	}
	for _, file := range []string{"/icon.svg", "/manifest.webmanifest"} {
		page = strings.ReplaceAll(page, `"`+file+`"`, `".`+file+`"`)
	}
	page += demoScript
	if strings.Contains(indexHTML, demoScript) {
		return fmt.Errorf("the page already carries the demo script — it must only ever be appended here")
	}
	for name, body := range map[string]string{
		"index.html": page,
		".nojekyll":  "",
		// The two the page links for a home-screen install. Same bytes the server answers with;
		// without them the demo is a page with a broken manifest, which is what it looked like
		// until a check started walking every path the page references.
		"manifest.webmanifest": manifestJSON,
		"icon.svg":             iconSVG,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	// Everything the page IMPORTS, at the path it imports it from. A missing font degrades the
	// look; a missing module means the script never runs and the page is blank — which is exactly
	// what shipped the first time this became a module, because emitDemo copied the fonts and
	// nobody had written down that the list had grown.
	// Everything in vendor/, not a list somebody keeps in step: the page imports what it imports,
	// and a hand-written list is one more thing to forget when a second bundle arrives — which is
	// exactly what happened with the Material Web one.
	vendored, verr := assetFS.ReadDir("vendor")
	if verr != nil {
		return verr
	}
	var carry []string
	for _, f := range vendored {
		if strings.HasSuffix(f.Name(), ".js") {
			carry = append(carry, "vendor/"+f.Name())
		}
	}
	// Every language pack, for the same reason and read the same way. The demo used to answer these
	// from a copy inlined in its own script, which meant the deployed page was English whatever the
	// reader's browser asked for — the one part of the console a visitor can check against their own
	// machine, and it was the part that did not work.
	packs, perr := assetFS.ReadDir("i18n")
	if perr != nil {
		return perr
	}
	for _, f := range packs {
		if strings.HasSuffix(f.Name(), ".json") {
			carry = append(carry, "i18n/"+f.Name())
		}
	}
	for _, name := range carry {
		b, rerr := assetFS.ReadFile(name)
		if rerr != nil {
			return rerr
		}
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			return err
		}
	}
	// The language seed the handler inlines for a real page. Written into the file here for the
	// same reason: without it the first paint is a screen of dotted keys.
	if pack, perr := assetFS.ReadFile("i18n/language.en.json"); perr == nil {
		page = "<script>window.__LANG=" + string(pack) + "</script>\n" + page
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(page), 0o644); err != nil {
			return err
		}
	}

	// The font the page asks for, at the path it asks for it. Without this the demo falls back to
	// the system serif and stops being a fair look at the thing.
	fonts, err := fontFS.ReadDir("fonts")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "font"), 0o755); err != nil {
		return err
	}
	for _, f := range fonts {
		if !strings.HasSuffix(f.Name(), ".woff2") {
			continue
		}
		b, rerr := fontFS.ReadFile("fonts/" + f.Name())
		if rerr != nil {
			return rerr
		}
		if werr := os.WriteFile(filepath.Join(dir, "font", f.Name()), b, 0o644); werr != nil {
			return werr
		}
	}
	return nil
}
