// The new console as a static page, answered by a mock in the browser — the old console's
// -emit-demo, translated. GitHub Pages serves files, not processes, so /fleet and /events are
// answered by a shim this emitter appends: fetch is patched for the BFF paths and EventSource is
// replaced with a fixture stream. The compiled page is otherwise byte-for-byte what the build
// produced — except the root-relative asset prefixes, which become relative the same way (and for
// the same reason) demo.go rewrites /vendor/ in the old console: Pages serves a subpath.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const demoShim = `
<script>
// A mock magi, in the page. Shapes mirror the real handlers; the page's own code cannot tell.
(() => {
  const now = new Date().toISOString().replace(/\.\d+Z$/, 'Z');
  const fleet = [
    {socket: '/demo/build.sock', name: 'build', role: 'keeps the build green', team: 'core',
     workdir: '/Users/you/work/app', session: 's_demo1', pid: 4242, host: 'devbox', addr: '10.0.0.7',
     version: 'dev', live: true, state: 'waiting', asking: 'may I run the migration?',
     askId: 'call_demo', askKind: 'permission', askOptions: ['yes', 'no'], task: 'run the migration',
     steps: 3, permission: 'ask', idle: 12, here: true},
    {socket: '/demo/docs.sock', name: 'docs', role: 'writes the manuals', team: 'core',
     workdir: '/Users/you/work/docs', session: 's_demo2', pid: 4243, host: 'devbox', addr: '10.0.0.7',
     version: 'dev', live: true, state: 'idle', asking: '', askId: '', askKind: '',
     task: 'proofread the release notes', steps: 0, permission: 'allow', idle: 340, here: true},
  ];
  const rows = [
    {who: 'user', text: 'run the migration and tell me how it went', at: now},
    {who: 'thinking', text: 'read the migration first\nthen ask before running it', at: now},
    {who: 'tool', tool: 'bash', args: '{"command":"cat migrations/0421.sql"}',
     out: '"alter table sessions add column title text;"', ok: true, at: now},
    {who: 'assistant', text: 'one statement, additive — asking before running it', at: now},
  ];
  const skills = [
    {name: 'rule-cache-window', description: 'reuse the prompt cache window when retrying', tier: 'global',
     kind: 'skill', observed: 4, firstSeen: '2026-08-01', lastSeen: '2026-08-25',
     body: 'When a retry rebuilds a prompt, keep the shared prefix byte-identical.\nsource: retry-storm postmortem'},
    {name: 'mem-staging-db', description: 'the staging database is restored from prod every Monday', tier: 'team',
     team: 'core', kind: 'memory', body: ''},
  ];
  const wiki = [
    {title: 'release trains', tier: 'global', updated: '2026-08-20T09:00:00Z', editor: 'docs',
     summary: 'how a web-v* tag ships the console alone', body: 'The core releases on v*; the console on web-v*.'},
    {title: 'old deploy runbook', tier: 'global', stale: true, body: 'superseded'},
  ];
  const mcp = [
    {name: 'github', tier: 'global', url: 'https://api.githubcopilot.com/mcp/', file: '~/.config/magi/config.toml'},
    {name: 'repo-grep', tier: 'project', companion: 'build', socket: '/demo/build.sock',
     command: 'rg-mcp', args: ['--root', '.'], envNames: ['RG_TOKEN'], file: '.magi/config.toml'},
  ];
  const histories = {
    '/demo/build.sock': [
      {id: 's1', title: 'run the migration', started: now.slice(0,10)+'T09:00:00Z',
       ended: now.slice(0,10)+'T09:40:00Z', model: 'gpt-oss:120b', labels: ['migration']},
      {id: 's2', title: 'still at it', started: now.slice(0,10)+'T11:00:00Z', ended: '', current: true},
    ],
    '/demo/docs.sock': [
      {id: 's3', title: 'proofread the notes', started: now.slice(0,10)+'T08:20:00Z',
       ended: now.slice(0,10)+'T08:50:00Z'},
    ],
  };
  const realFetch = window.fetch.bind(window);
  window.fetch = (input, init) => {
    const url = typeof input === 'string' ? input : input.url;
    if (url.startsWith('/i18n/')) return realFetch('.' + url, init);
    if (url.startsWith('/fleet')) return Promise.resolve(new Response(JSON.stringify(fleet)));
    if (url.startsWith('/skills')) return Promise.resolve(new Response(JSON.stringify(skills)));
    if (url.startsWith('/wiki')) return Promise.resolve(new Response(JSON.stringify(wiki)));
    if (url.startsWith('/mcp')) return Promise.resolve(new Response(JSON.stringify(mcp)));
    if (url.startsWith('/history')) {
      const d = new URLSearchParams(url.split('?')[1] || '').get('d');
      return Promise.resolve(new Response(JSON.stringify(histories[d] || [])));
    }
    if (init && init.method === 'POST') return Promise.resolve(new Response(''));
    return realFetch(input, init);
  };
  window.EventSource = class {
    constructor(url) {
      this.handlers = {};
      const q = new URLSearchParams(String(url).split('?')[1] || '');
      setTimeout(() => {
        this.emit('open', {});
        this.emit('fleet', {data: JSON.stringify(fleet)});
        if (q.get('d')) {
          this.emit('message', {data: JSON.stringify(rows)});
          this.emit('turn', {data: '{"open":false,"forSec":0}'});
        }
      }, 30);
    }
    addEventListener(kind, fn) { (this.handlers[kind] = this.handlers[kind] || []).push(fn); }
    // GWT registers EventListener OBJECTS (handleEvent), not bare functions — both are the DOM
    // contract, and the first fixture only honoured half of it (measured: "fn is not a function").
    emit(kind, evt) {
      for (const fn of this.handlers[kind] || []) {
        if (typeof fn === 'function') fn(evt);
        else if (fn && typeof fn.handleEvent === 'function') fn.handleEvent(evt);
      }
    }
    close() {}
  };
})();
</script>
`

// emitDemo writes the assembled console as a self-answering static site into dir.
func emitDemo(dir, ui, oldConsole string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// The compiled modules and stylesheets, laid out under ui/ the way the server serves them.
	if err := copyTree(ui, filepath.Join(dir, "ui")); err != nil {
		return fmt.Errorf("ui assets: %w", err)
	}
	// The single-source assets the BFF serves in life: the material bundle and the language packs.
	if err := copyTree(filepath.Join(oldConsole, "i18n"), filepath.Join(dir, "i18n")); err != nil {
		return fmt.Errorf("i18n: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "vendor"), 0o755); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(oldConsole, "vendor", "material.js"),
		filepath.Join(dir, "vendor", "material.js")); err != nil {
		return fmt.Errorf("vendor: %w", err)
	}
	// The page: root-relative prefixes become relative, and the shim goes in ahead of the shell
	// so fetch and EventSource are already the mock's when the first module asks.
	pageBytes, err := os.ReadFile(filepath.Join(ui, "console.html"))
	if err != nil {
		return err
	}
	page := string(pageBytes)
	for _, prefix := range []string{"/ui/", "/vendor/"} {
		page = strings.ReplaceAll(page, `"`+prefix, `"./`+strings.TrimPrefix(prefix, "/"))
		page = strings.ReplaceAll(page, "'"+prefix, "'./"+strings.TrimPrefix(prefix, "/"))
	}
	page = strings.Replace(page, "<script src=", demoShim+"<script src=", 1)
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(page), 0o644); err != nil {
		return err
	}
	// The compiled shell builds module paths as '/ui/'+name+… (ScriptModuleLoader) — absolute,
	// because in life relative paths leak into the proxy. Here there is no proxy and there IS a
	// subpath, so the demo's copy says it relatively. Same move as the old console's /vendor/.
	// GWT's obfuscated output writes string literals with SINGLE quotes (measured), and the
	// path shows up two ways: as the loader's own '/ui/' piece and as whole literals like
	// '/ui/companion.css'. So the rewrite is by PREFIX — quote + /ui/ — in both quote styles,
	// the same shape the old console's demo uses for /vendor/.
	for _, q := range []string{`'`, `"`} {
		if err := rewriteTree(filepath.Join(dir, "ui"), q+`/ui/`, q+`./ui/`); err != nil {
			return err
		}
	}
	return nil
}

func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		dest := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		return copyFile(p, dest)
	})
}

func copyFile(from, to string) error {
	b, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return os.WriteFile(to, b, 0o644)
}

func rewriteTree(root, from, to string) error {
	return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".js") {
			return err
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if !strings.Contains(string(b), from) {
			return nil
		}
		return os.WriteFile(p, []byte(strings.ReplaceAll(string(b), from, to)), 0o644)
	})
}
