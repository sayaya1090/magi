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
  const now = new Date().toISOString();
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
       description: 'run the tests before saying it is done'},
      {name: 'skill-tokens', kind: 'skill', tier: 'project', companion: 'design',
       socket: '/demo/design.sock', observed: 3, firstSeen: '2026-07-14', lastSeen: '2026-08-06',
       description: 'spacing comes from the scale, never hand-written'},
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

  globalThis.fetch = async (path, init) => {
    const url = String(path).split('?')[0];
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
	page := indexHTML + demoScript
	if strings.Contains(indexHTML, demoScript) {
		return fmt.Errorf("the page already carries the demo script — it must only ever be appended here")
	}
	for name, body := range map[string]string{
		"index.html": page,
		".nojekyll":  "",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
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
