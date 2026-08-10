// magi-web attaches a browser to a running magi daemon.
//
// A separate binary on purpose. It could have been a flag on magi, but then every daemon would
// carry a web server it never starts — and it could have been a plugin, except a plugin's HTTP
// handler runs under the same lock as its tool calls, so the page would freeze for the whole of a
// thirty-second build. Exactly when you want to watch.
//
// It holds nothing. The transcript comes from the session log, which is append-only and already
// shared; the five things that touch the running turn go to the daemon over its socket. So this
// process can start late, be killed, and start again, and the daemon never notices — which is the
// property a viewer should have.
//
// Loopback only, and no authentication of its own. Reach it from elsewhere with
// `ssh -L 7777:localhost:7777 host`, the same answer `magi --attach` gives: OpenSSH already solves
// the part of this that is easy to get subtly wrong.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/version"
)

func main() { os.Exit(run()) }

func run() int {
	var (
		addr      = flag.String("addr", "127.0.0.1:7777", "address to serve on; loopback by default and it should stay that way")
		cfgDir    = flag.String("config-dir", "", "magi config directory (default: the platform one, honouring MAGI_CONFIG_DIR)")
		workdir   = flag.String("workdir", "", "the daemon's workspace (default: the current directory)")
		showVer   = flag.Bool("version", false, "print version and exit")
		peerSpecs multiFlag
	)
	// Repeatable: -peer mini=http://127.0.0.1:7778 -peer laptop=http://127.0.0.1:7779
	//
	// Each is another magi-web, usually a loopback port an ssh tunnel ends at. They come from the
	// operator and nowhere else — see peer.go for why that is the rule this file must not bend.
	flag.Var(&peerSpecs, "peer", "another magi-web to federate, as name=url; repeatable")
	// Writing the page out as a static site, answered by a mock in the browser. Here rather than in
	// its own command because it must emit the string THIS binary serves — a generator with its own
	// copy of the page is a demo that drifts, which is worse than none.
	emit := flag.String("emit-demo", "", "write the console as a static demo into this directory, then exit")
	flag.Parse()

	peers, perr := parsePeers(peerSpecs)
	if perr != nil {
		fmt.Fprintln(os.Stderr, "magi-web:", perr)
		return 1
	}
	if *showVer {
		fmt.Println("magi-web " + version.String())
		return 0
	}

	wd := *workdir
	if wd == "" {
		var err error
		if wd, err = os.Getwd(); err != nil {
			fmt.Fprintln(os.Stderr, "magi-web: getwd:", err)
			return 1
		}
	}
	// The SAME platform the daemon uses, not a second guess at where things live. The socket is
	// under the config directory and the store is under the data directory, and those are not the
	// same place — reimplementing either here is how a viewer ends up watching an empty session
	// while the daemon writes to another.
	plat := platform.New()
	cd := *cfgDir
	if cd == "" {
		cd = plat.ConfigDir()
	}

	// The daemon in THIS directory, if there is one, is what the viewer opens on. There need not
	// be: a dashboard over other people's daemons is a legitimate thing to want from a directory
	if *emit != "" {
		if err := emitDemo(*emit); err != nil {
			fmt.Fprintln(os.Stderr, "magi-web:", err)
			return 1
		}
		fmt.Println("wrote the demo to", *emit)
		return 0
	}

	// that has none of its own.
	here := daemon.SocketPath(cd, wd)

	// The reading half: this process's own store over the same directory. No LLM and no tools —
	// it never runs a turn, and handing it a real provider would be an invitation to.
	store, err := jsonl.New(plat.DataDir())
	if err != nil {
		fmt.Fprintln(os.Stderr, "magi-web: store:", err)
		return 1
	}
	reader := app.New(store, nil, builtin.NewRegistry(), bus.New(), nil, app.Config{})

	srv := &server{
		reader: reader, cfgDir: cd, here: here, clients: map[string]*daemon.Client{},
		peers: peers,
		// Two clients on purpose. The short one bounds every call that has an answer; the streaming
		// one must not, because an event stream is supposed to stay open and a timeout there would
		// cut the transcript every few seconds.
		http:       &http.Client{Timeout: peerTimeout},
		stream:     &http.Client{},
		embedModel: os.Getenv("MAGI_EMBED_MODEL"),
	}
	defer srv.closeAll()
	if p, err := newPush(cd); err != nil {
		fmt.Fprintln(os.Stderr, "magi-web: notifications are off:", err)
	} else {
		srv.pushes = p
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		// Three seconds, which is the fleet page's own refresh. Anything slower would let somebody
		// watching the page see a block before their phone did, and the poll reads the same cache
		// the page does — most ticks are a map read.
		go srv.watch(ctx, 3*time.Second)
	}
	mux := http.NewServeMux()
	for path, h := range srv.routes() {
		mux.HandleFunc(path, h)
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "magi-web: listen:", err)
		return 1
	}
	if !isLoopback(ln.Addr()) {
		// Refuse rather than warn. This serves a control surface with no authentication of its
		// own; bound to a routable address it hands the workspace to whoever finds the port.
		ln.Close()
		fmt.Fprintf(os.Stderr, "magi-web: %s is not loopback and this server has no authentication — "+
			"use ssh -L to reach it from elsewhere\n", *addr)
		return 1
	}
	fmt.Fprintf(os.Stderr, "magi-web: http://%s — %d companion(s) under %s", ln.Addr(), countDaemons(cd), cd)
	if len(peers) > 0 {
		names := make([]string, len(peers))
		for i, p := range peers {
			names[i] = p.Name
		}
		fmt.Fprintf(os.Stderr, ", federating %s", strings.Join(names, ", "))
	}
	fmt.Fprintln(os.Stderr)
	if err := (&http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}).Serve(ln); err != nil {
		fmt.Fprintln(os.Stderr, "magi-web:", err)
		return 1
	}
	return 0
}

// isLoopback reports whether an address is one only this machine can reach.
func isLoopback(a net.Addr) bool {
	host, _, err := net.SplitHostPort(a.String())
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// multiFlag collects a repeatable string flag.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// countDaemons is for the startup line only — a viewer that says "0 daemons" is telling you the
// config directory is wrong before you go looking in the browser.
func countDaemons(cfgDir string) int {
	l, _ := daemon.List(cfgDir)
	return len(l)
}

type server struct {
	reader *app.App
	cfgDir string
	here   string // the socket for the directory magi-web was started in, if any

	// Other consoles this one reads, from the operator's flags. Empty is the ordinary case: one
	// machine, no federation, nothing on the network.
	peers  []peer
	http   *http.Client // bounded: list and act
	stream *http.Client // unbounded: an event stream is meant to stay open

	// One client per LOCAL daemon, opened on first use. A dashboard that dialled on every request
	// would reconnect several times a second; one that dialled all of them at startup would fail on
	// the first dead socket and refuse to show the rest.
	mu      sync.Mutex
	clients map[string]*daemon.Client

	// What a dashboard refresh would otherwise re-derive every three seconds for every idle agent:
	// the last thing it said, which by definition is not changing while it is idle.
	fleetCache fleet.Cache

	// Notifications, or nil when the key could not be read. Nil is a working console without them
	// rather than a console that refuses to start: being unable to buzz a phone is not a reason to
	// withhold the page from somebody sitting in front of it.
	pushes *pushState

	// What searches here embed with, for the shared-knowledge screen to show. Read once at startup:
	// changing it means restarting anyway, since the vectors already cached are the old model's.
	embedModel string
}

func (s *server) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.clients {
		c.Close()
	}
}

// clientFor returns the connection to one daemon, dialling once.
func (s *server) clientFor(sock string) (*daemon.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clients[sock]; ok {
		return c, nil
	}
	c, err := daemon.Dial(sock)
	if err != nil {
		return nil, err
	}
	s.clients[sock] = c
	return c, nil
}

// target resolves the ?d= socket a request is about, defaulting to the directory this viewer was
// started in. Only sockets List already found are accepted: the parameter comes from a page and a
// path from a page must not become a path this process will dial.
func (s *server) target(r *http.Request) (daemon.Info, error) {
	want := r.URL.Query().Get("d")
	if want == "" {
		if s.here == "" {
			return daemon.Info{}, fmt.Errorf("no daemon in this directory — pick one from the dashboard")
		}
		want = s.here
	}
	// Find, not List: resolving one target must not dial every daemon on the machine. It used to,
	// so a steer typed into one agent waited on an unrelated wedged neighbour before it was sent.
	in, err := daemon.Find(s.cfgDir, want)
	if err != nil {
		if want == s.here {
			return daemon.Info{}, fmt.Errorf("no daemon in this directory — pick one from the dashboard")
		}
		return daemon.Info{}, err
	}
	return in, nil
}

// forget drops a cached connection so the next use dials again.
func (s *server) forget(sock string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clients[sock]; ok {
		c.Close()
		delete(s.clients, sock)
	}
}

// routeToPeer reports whether a request names a companion on another console, and which.
//
// The peer comes from the query, but it is only ever LOOKED UP in the operator's list — a name that
// is not configured routes nowhere. The socket travels verbatim because it is that machine's path
// and means nothing here; the peer is what this process resolves.
func (s *server) routeToPeer(r *http.Request) (p peer, socket string, remote bool, err error) {
	name := r.URL.Query().Get("p")
	if name == "" {
		return peer{}, "", false, nil
	}
	p, ok := s.peerNamed(name)
	if !ok {
		// Named and unknown is an error of its own, not a fall-through to the local lookup — that
		// path then fails for a different reason ("no daemon at …"), which sends whoever is reading
		// to look for a companion when the actual answer is that this console federates nobody by
		// that name.
		return peer{}, "", false, fmt.Errorf("no console named %q is federated here; this one knows: %s",
			name, s.peerNames())
	}
	return p, r.URL.Query().Get("d"), true, nil
}

// forwarded sends a request to the console that owns the companion it names, and reports whether
// the caller is done.
//
// Six handlers began with the same twelve lines — resolve the peer, answer 404 if the name is not
// one this operator configured, otherwise forward and return — and three of them carried the same
// comment word for word. The shape is one decision ("is this mine to answer?"), so it reads better
// as one call, and a seventh handler cannot now forget half of it.
//
// via is how it travels: s.proxy for a request with an answer, s.proxyStream for one that stays
// open. That is the only thing that ever differed between the copies.
func (s *server) forwarded(w http.ResponseWriter, r *http.Request,
	via func(http.ResponseWriter, *http.Request, peer, string)) bool {
	p, socket, remote, err := s.routeToPeer(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return true
	}
	if !remote {
		return false
	}
	via(w, r, p, socket)
	return true
}

// writeJSON answers with a value, or logs why it could not.
//
// Seven handlers had the same three lines. The log line is the part worth keeping identical: an
// encode that fails halfway has already written a partial body and the status is long gone, so the
// only thing left to do is say which answer broke — and a handler that quietly dropped that would
// leave a truncated response and no trace of it anywhere.
func writeJSON(w http.ResponseWriter, what string, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("magi-web: writing the %s: %v", what, err)
	}
}

// session is the companion a request names, or a 404 saying why not.
//
// The pair is always the same: resolve the target, answer 404 with the resolver's own words when
// there is none, and carry on with its session id. Returning the id rather than the record is what
// the callers actually wanted — five of them reached straight for .Session — and it keeps the
// published record from leaking into places that have no business holding a workspace path.
func (s *server) session(w http.ResponseWriter, r *http.Request) (session.SessionID, bool) {
	in, err := s.target(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return "", false
	}
	return session.SessionID(in.Session), true
}

// sameSiteOnly rejects a state-changing request that another site sent.
//
// Binding to loopback keeps the network out and does nothing about the browser. Any page the
// operator visits can POST to 127.0.0.1: a form-urlencoded body is a CORS "simple request", so it
// goes without a preflight, and the attacker never needs to read the reply — the side effect has
// already happened. Measured before this existed: a page on an unrelated origin wrote
// [mcp.pwned] command = "/bin/sh" into the global config, and a daemon runs its configured MCP
// servers at startup. Visiting a web page was arbitrary code execution.
//
// Two headers settle it, and script cannot forge either. Sec-Fetch-Site says where the request
// came from; Origin is set by the browser on every POST and is on the forbidden-header list. A
// request with NEITHER is not from a browser at all — curl, a script, the operator's own shell —
// and that is allowed, because this server is loopback-only and those are the operator.
//
// Applied by wrapping the mux rather than by calling it in each handler: a route added later is
// covered by existing, which is the difference between a rule and a list somebody maintains.
func sameSiteOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			h(w, r) // a cross-origin read cannot see the reply; the browser blocks that itself
			return
		}
		switch r.Header.Get("Sec-Fetch-Site") {
		case "", "same-origin", "none":
			// same-origin, or a client that sends no fetch metadata
		default:
			http.Error(w, "cross-site requests are refused", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" &&
			o != "http://"+r.Host && o != "https://"+r.Host {
			http.Error(w, "cross-origin requests are refused", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

// postOnly rejects a read method on a handler that changes something.
func postOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodPost {
		return false
	}
	http.Error(w, "POST only", http.StatusMethodNotAllowed)
	return true
}

// peerNames is for the message above: a list told "unknown" and not told what IS known cannot tell
// a typo from a console that was never configured.
func (s *server) peerNames() string {
	if len(s.peers) == 0 {
		return "(none — this console federates no others)"
	}
	names := make([]string, len(s.peers))
	for i, p := range s.peers {
		names[i] = p.Name
	}
	return strings.Join(names, ", ")
}

// withClient runs one write against the request's target, retrying once on a fresh connection.
//
// The retry is not defensive padding: a daemon restarted between two page actions leaves this
// process holding a socket nobody reads, and the first write after that fails on a connection
// problem rather than on anything the user did. Redialling tells that apart from a real refusal —
// and if the second attempt fails too, its error is the one worth showing.
func (s *server) withClient(r *http.Request, do func(*daemon.Client, session.SessionID) error) error {
	in, err := s.target(r)
	if err != nil {
		return err
	}
	sid := session.SessionID(in.Session)
	cl, err := s.clientFor(in.Socket)
	if err != nil {
		return err
	}
	if err = do(cl, sid); err == nil {
		return nil
	}
	s.forget(in.Socket)
	cl, derr := s.clientFor(in.Socket)
	if derr != nil {
		return err // the original failure, not "could not reconnect" — that hides the reason
	}
	return do(cl, sid)
}

// page is the whole front end. Server-rendered with an inline script and no build step: magi ships
// one static binary with no toolchain behind it, and a bundler for a transcript and a text box
// would be a second thing to keep working.
//
// One page for both views. `/` is the dashboard, `/?d=<socket>` is one agent — the same document,
// which is why entering an agent and coming back costs no reload and why the two cannot drift into
// two different ideas of what magi looks like.
func (s *server) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The English pack, inlined ahead of the page's own script. The page fetches the reader's
	// locale after it loads, but the FIRST paint happens before any fetch answers — without a seed
	// every label would show its dotted key for a moment, which is debug output on somebody's
	// dashboard. One source: the same file the /i18n route serves.
	if pack, err := assetFS.ReadFile("i18n/language.en.json"); err == nil {
		if _, werr := w.Write([]byte("<script>window.__LANG=" + string(pack) + "</script>\n")); werr != nil {
			log.Printf("magi-web: writing the language seed: %v", werr)
			return
		}
	}
	// A write that fails means the browser hung up mid-page. There is nobody left to tell and no
	// second attempt worth making, but the reason is worth having in the log of a process whose
	// whole job is to be watched.
	if _, err := io.WriteString(w, indexHTML); err != nil {
		log.Printf("magi-web: writing the page: %v", err)
	}
}

// interventions is the supervisor's evening question: what did I have to step in and say?
//
// Gathered across every companion, including the federated ones, because the whole value is seeing
// that the same correction went to three of them — that is what makes it a rule rather than a
// remark, and one companion at a time cannot show it.
func (s *server) interventions(w http.ResponseWriter, r *http.Request) {
	since := 7 * 24 * time.Hour
	if v := r.URL.Query().Get("days"); v != "" {
		if d, err := strconv.Atoi(v); err == nil && d > 0 && d <= 90 {
			since = time.Duration(d) * 24 * time.Hour
		}
	}
	list, err := fleet.Interventions(r.Context(), s.reader, s.cfgDir, since)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	list = append(list, s.peerInterventions(r.Context(), r.URL.RawQuery)...)
	sort.Slice(list, func(i, j int) bool { return list[i].At > list[j].At })
	writeJSON(w, "interventions", list)
}

// routes is every path this server answers, in one place.
//
// Wrapped where the table is built, not where the server is started: a guard applied at the call
// site is one a later route can be added beside, and a test that calls the wrapper directly passes
// either way — measured, by removing the wrapping and watching the check stay green.
//
// A list rather than a run of mux.HandleFunc calls because the page links to some of these, and a
// test checks that everything the page references is a path this binary serves — which is the real
// meaning of "self-contained", and cannot be checked against a list that exists only as statements.
func (s *server) routes() map[string]http.HandlerFunc {
	out := map[string]http.HandlerFunc{}
	for path, h := range s.handlers() {
		out[path] = sameSiteOnly(h)
	}
	return out
}

// handlers is the table itself. routes() is what anything outside gets, and it is the wrapped one.
func (s *server) handlers() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/":                     s.page,
		"/fleet":                s.fleet,
		"/events":               s.events,
		"/submit":               s.submit,
		"/interrupt":            s.interrupt,
		"/shell":                s.shell,
		"/cron":                 s.cron,
		"/search":               s.search,
		"/answer":               s.answer,
		"/manifest.webmanifest": s.manifest,
		"/icon.svg":             s.icon,
		"/font/":                s.font,
		// The page's own two subtrees. Missing, `import '/vendor/material.js'` answered 404, which
		// fails the whole ES module — so on a real console NOTHING ran: no components, no script, no
		// language beyond the seed inlined above. The demo hid it for as long as it existed, because
		// a static export writes these files to disk beside the page.
		"/vendor/":       s.asset,
		"/i18n/":         s.asset,
		"/interventions": s.interventions,
		"/skills":        s.skills,
		"/forget":        s.forgetSkill,
		"/remember":      s.remember,
		"/context":       s.context,
		"/dispatch":      s.dispatch,
		"/mcp":           s.mcp,
		"/handoffs":      s.handoffs,
		"/plan":          s.plan,
		"/compact":       s.compact,
		"/console":       s.console,
		"/history":       s.history,
		"/push":          s.push,
		"/sw.js":         s.serviceWorker,
	}
}

//go:embed fonts/*.woff2
var fontFS embed.FS

//go:embed vendor/*.js i18n/*.json
var assetFS embed.FS

// asset serves the vendored javascript and the language packs.
//
// Both are in the binary for the same reason the typeface is: a page that fetched them from
// somewhere else would depend on that machine being up, tell it when you look at your agents, and
// behave differently on a laptop with no route out. See vendor/README.md for how the bundle is
// built — once, by hand, from a pinned version, with its hash written down.
func (s *server) asset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	b, err := assetFS.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch {
	case strings.HasSuffix(name, ".js"):
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case strings.HasSuffix(name, ".json"):
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	// Immutable for a day: the bundle is pinned and the language packs change with a release, so a
	// browser re-fetching them on every poll is bytes for nothing. Not forever — a build that does
	// change them should reach a tab somebody left open overnight.
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if _, err := w.Write(b); err != nil {
		log.Printf("magi-web: serving %s: %v", name, err)
	}
}

// font serves the display face the page sets its headlines in.
//
// Embedded and served from here rather than fetched from a font CDN. A viewer that reached out for
// its typeface would make this page's appearance depend on a machine that is not yours, tell that
// machine when you look at your agents, and fall back to something else entirely on a laptop with
// no route out — and this binary exists to hold everything it serves. See fonts/README.md for how
// the files were built, and fonts/OFL.txt for the licence that travels with them.
func (s *server) font(w http.ResponseWriter, r *http.Request) {
	name := path.Base(r.URL.Path)
	b, err := fontFS.ReadFile("fonts/" + name)
	if err != nil || !strings.HasSuffix(name, ".woff2") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "font/woff2")
	// The bytes are baked into the binary, so they cannot change without the binary changing —
	// and a page that re-fetches its typeface every poll is a page that flashes.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if _, err := w.Write(b); err != nil {
		log.Printf("magi-web: writing %s: %v", name, err)
	}
}

// manifest makes the page installable: added to a phone's home screen it opens without the browser
// chrome, which is the difference between "a website about my agents" and something you reach for.
//
// Served rather than inlined as a data: URI because iOS ignores a manifest it cannot fetch, and it
// is small enough that a route costs less than the explanation of the workaround would.
// manifestJSON and iconSVG are package-level so the static demo can write the same bytes this
// server answers with. They were consts inside their handlers, and the demo shipped without either
// — found by a check that walks every path the page references.
const manifestJSON = `{
  "name": "magi",
  "short_name": "magi",
  "start_url": "/",
  "scope": "/",
  "display": "standalone",
  "background_color": "#14110d",
  "theme_color": "#14110d",
  "icons": [{"src": "/icon.svg", "sizes": "any", "type": "image/svg+xml", "purpose": "any maskable"}]
}`

const iconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 192 192">
  <rect width="192" height="192" fill="#211B14"/>
  <circle cx="96" cy="70" r="21" fill="#FFB454"/>
  <circle cx="70" cy="115" r="21" fill="#5CD8E6"/>
  <circle cx="122" cy="115" r="21" fill="#FF8A8A"/>
  <circle cx="96" cy="97" r="43" fill="none" stroke="#FF7A1A" stroke-width="4" opacity=".55"/>
</svg>`

func (s *server) manifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	// display:standalone and a theme colour that matches the page's BACKGROUND — the masthead sits
	// on it directly now, so a surface colour here would draw a band the page does not have. start_url is the fleet: the phone is where you check on things.
	if _, err := io.WriteString(w, manifestJSON); err != nil {
		log.Printf("magi-web: writing the manifest: %v", err)
	}
}

// icon is the home-screen icon: the three councillors, in their own hues.
//
// SVG so there is one file for every size, and drawn here rather than shipped as a PNG because a
// binary asset in a source tree is a thing nobody can review. The maskable safe zone is the middle
// 80%, so nothing meaningful goes near the edge.
func (s *server) icon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	if _, err := io.WriteString(w, iconSVG); err != nil {
		log.Printf("magi-web: writing the icon: %v", err)
	}
}

// fleet is the dashboard's data: every daemon this config directory knows about.
//
// The states and their derivation live in internal/adapter/fleet, shared with `magi --agents`. Two
// surfaces answering "what is that agent doing?" from two copies of the same reasoning is a pair
// that disagrees later, when only one of them is updated.
func (s *server) fleet(w http.ResponseWriter, r *http.Request) {
	list, err := fleet.ListCached(r.Context(), s.reader, s.cfgDir, s.here, &s.fleetCache)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// The local companions are answered first and the peers are added to them, so a console with an
	// unreachable peer still shows this machine rather than an error page.
	list = s.federated(r.Context(), list)
	writeJSON(w, "fleet", list)
}

// events streams the transcript as server-sent events, re-reading the log as it grows.
//
// The daemon's bus is in the daemon's memory, so this polls the shared log — the same answer the
// terminal attach reached, for the same reason: the log is already the record, and a second stream
// of the same facts is the first thing to disagree after a reconnect.
func (s *server) events(w http.ResponseWriter, r *http.Request) {
	// A remote companion's transcript is streamed through, so opening one is not a different page
	// from opening a local one.
	if s.forwarded(w, r, s.proxyStream) {
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	in, err := s.target(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	sid := session.SessionID(in.Session)

	var lastSeq int64 = -1
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	for {
		// Ask before rebuilding. Reconstructing the transcript from the log costs 12ms on a
		// four-thousand-event session and asking whether anything arrived costs 2µs, and on almost
		// every tick the answer is nothing — this is the same poll running two and a half times a
		// second for as long as the page is open, once per viewer.
		//
		// The REBUILD is not cached, deliberately. The rendered transcript is not append-only even
		// though the log is: a resurfaced interjection removes an earlier prompt when a later event
		// lands, and a compaction rewrites the log outright. A kept prefix would need an
		// invalidation signal that does not exist, and a wrong one shows a transcript the log
		// denies. Cheap question, whole answer.
		// lastSeq starts at -1, so the first pass asks for everything and the first frame is the
		// backlog — a viewer that connects to a session hours after the work is the ordinary case.
		seq, changed, err := s.reader.NewSince(r.Context(), sid, lastSeq)
		if err == nil && changed {
			lastSeq = seq
			msgs, _, serr := s.reader.SessionState(r.Context(), sid)
			if serr != nil {
				select {
				case <-r.Context().Done():
					return
				case <-tick.C:
				}
				continue
			}
			rows := renderMessages(msgs)
			// The council, put back where it happened. Its marks name the message they followed,
			// so this is a splice and not a guess about ordering — and a mark whose anchor is not
			// in the transcript (a compaction dropped it) goes at the end rather than nowhere.
			if marks, cerr := s.reader.CouncilMarks(r.Context(), sid); cerr == nil {
				rows = spliceCouncil(rows, marks)
			}
			b, _ := json.Marshal(rows)
			// One SSE frame, one whole transcript. A diff protocol would be smaller and would also
			// be a second thing that can drift from the log; at these sizes it is not worth it.
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
	}
}

// line is one row of the transcript as the page draws it.
//
// Tool and Args are carried apart from Text so the page can fold a call behind a summary that names
// what was run without having to take the name back out of a string this file just put it into.
type line struct {
	Who  string `json:"who"`
	Text string `json:"text"`
	Tool string `json:"tool,omitempty"`
	Args string `json:"args,omitempty"`
	// Round and Tally belong to a council row: which round it was, and how the round came out.
	Round int    `json:"round,omitempty"`
	Tally string `json:"tally,omitempty"`
	// Decision is the council vocabulary as the log spells it, so the page can colour by it.
	Decision string `json:"decision,omitempty"`
	// msg is the message this row came out of, and never crosses the wire. It is how council marks
	// find the place they belong: they carry the same id, out of the same log.
	msg string `json:"-"`
}

// renderMessages flattens the session into rows. Tool calls keep their arguments: what a tool was
// asked to do is most of what a watcher wants to know, and the name alone is the progress line
// that was not enough on the pane strip either.
func renderMessages(msgs []session.Message) []line {
	var out []line
	for _, m := range msgs {
		for _, p := range m.Parts {
			switch p.Kind {
			case session.PartText:
				if t := strings.TrimSpace(p.Text); t != "" {
					out = append(out, line{Who: string(m.Role), Text: t, msg: m.ID})
				}
			case session.PartReasoning:
				if t := strings.TrimSpace(p.Text); t != "" {
					out = append(out, line{Who: "thinking", Text: t, msg: m.ID})
				}
			case session.PartToolCall:
				if p.ToolCall != nil {
					// Arguments are no longer clipped at 300 bytes. They were, because the row was
					// always open and a long call pushed the conversation off the screen; the row
					// folds now, and what a tool was asked is most of what a watcher wants when
					// they open it.
					out = append(out, line{Who: "tool", Text: p.ToolCall.Name,
						Tool: p.ToolCall.Name, Args: string(p.ToolCall.Args), msg: m.ID})
				}
			case session.PartToolResult:
				if p.ToolResult != nil {
					who := "result"
					if p.ToolResult.IsError {
						who = "failed"
					}
					// Still clipped, and by much more than before. A result can be a whole build
					// log, and the transcript is rebuilt and re-sent on every change — this bound
					// is about what crosses the wire two and a half times a second, not about what
					// fits on the screen.
					out = append(out, line{Who: who, Text: fleet.Clip(string(p.ToolResult.Content), 8000), msg: m.ID})
				}
			// The two that were being dropped on the floor. A part kind this switch does not name
			// is not an empty row — it is a row that never existed, with nothing anywhere saying
			// so. An image and an error both reached the log and neither reached the page.
			case session.PartImage:
				if p.Image != nil {
					out = append(out, line{Who: "image", Text: p.Image.Path, msg: m.ID})
				}
			case session.PartError:
				if t := strings.TrimSpace(p.Err); t != "" {
					out = append(out, line{Who: "error", Text: t, msg: m.ID})
				}
			}
		}
	}
	return out
}

// spliceCouncil puts each council mark after the last row of the message it followed.
//
// Anchored rather than appended. Appending is right until a session has a second turn, which is
// every session anybody keeps — and then round one's votes appear after round two's work, saying
// the members approved something they never saw.
func spliceCouncil(rows []line, marks []app.CouncilMark) []line {
	if len(marks) == 0 {
		return rows
	}
	// Where each message's rows end, so several marks on one message keep their own order.
	after := map[string]int{}
	for i, r := range rows {
		if r.msg != "" {
			after[r.msg] = i
		}
	}
	pending := map[int][]line{}
	var orphans []line
	for _, m := range marks {
		row := line{Who: "council", Text: councilText(m), Round: m.Round, Tally: m.Tally,
			Decision: m.Decision}
		at, ok := after[m.After]
		if !ok {
			orphans = append(orphans, row)
			continue
		}
		pending[at] = append(pending[at], row)
	}
	out := make([]line, 0, len(rows)+len(marks))
	for i, r := range rows {
		out = append(out, r)
		out = append(out, pending[i]...)
	}
	return append(out, orphans...)
}

// councilWord and councilIcon are the council's vocabulary as a reader should see it.
//
// "continue" is the important one: from this council it is a REJECTION — the gate on ending the
// turn, which the work cannot proceed past — and the page showed the raw word in a neutral colour,
// which reads as progress. The terminal has said "reject" since it drew its first verdict.
//
// The same mapping is in internal/adapter/tui/render.go. Two spellings of one vocabulary, in two
// languages; noted here rather than left for somebody to find when they diverge.
func councilWord(decision string) string {
	switch decision {
	case "continue":
		return "reject"
	case "done", "abstain":
		return decision
	}
	return decision
}

func councilIcon(decision string) string {
	switch decision {
	case "done":
		return "✓"
	case "continue":
		return "✗"
	case "abstain":
		return "∅"
	}
	return "·"
}

// councilText is what a council row says: the vote, then the reasoning behind it.
//
// One string rather than fields, because the page folds it the same way it folds reasoning — a
// summary line and the rest behind it — and the summary is the first line of this.
func councilText(m app.CouncilMark) string {
	if m.IsOutcome() {
		head := "the council says " + councilWord(m.Decision)
		if m.Tally != "" {
			head += " — " + m.Tally
		}
		if m.Why != "" {
			return head + "\n\n" + m.Why
		}
		return head
	}
	head := councilIcon(m.Decision) + " " + m.Member + ": " + councilWord(m.Decision)
	if m.Lens != "" {
		head += " (" + m.Lens + ")"
	}
	var body []string
	if m.Why != "" {
		body = append(body, m.Why)
	}
	// The fragment a vote rests on, named. An empty one on a "done" is itself worth seeing, which
	// is why the absence is written out rather than left as a missing line.
	if m.Cite != "" {
		body = append(body, "rests on: "+m.Cite)
	} else if m.Decision == "done" {
		body = append(body, "rests on: nothing cited")
	}
	if len(body) == 0 {
		return head
	}
	return head + "\n\n" + strings.Join(body, "\n\n")
}

func (s *server) submit(w http.ResponseWriter, r *http.Request) {
	// A companion on another console is acted on THERE. Nothing about this one can reach it — the
	// socket path is that machine's, and its daemon is not ours to dial.
	if s.forwarded(w, r, s.proxy) || postOnly(w, r) {
		return
	}
	text := strings.TrimSpace(r.FormValue("text"))
	if text == "" {
		http.Error(w, "empty", http.StatusBadRequest)
		return
	}
	// Steer, not Submit: the daemon may already be working, and a second Submit would queue a
	// whole new turn behind it rather than reaching the one in flight. The engine decides which
	// it is — that decision is already made there, and making it twice is how the two disagree.
	err := s.withClient(r, func(cl *daemon.Client, sid session.SessionID) error {
		return cl.Steer(context.Background(), command.SubmitPrompt{
			SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: text}}})
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// answer resolves a prompt the daemon is blocked on.
//
// The prompt itself never reaches this process as an event — it is transient, and it belongs to the
// daemon's bus — so the page learns of it from /fleet and answers by call id. Which is why the id
// travels with the status: a viewer that can see a pending permission and not grant it stops in a
// worse place than one that never showed it.
func (s *server) answer(w http.ResponseWriter, r *http.Request) {
	// A companion on another console is acted on THERE. Nothing about this one can reach it — the
	// socket path is that machine's, and its daemon is not ours to dial.
	if s.forwarded(w, r, s.proxy) || postOnly(w, r) {
		return
	}
	callID := strings.TrimSpace(r.FormValue("call"))
	if callID == "" {
		http.Error(w, "no call id — this answer would have nowhere to go", http.StatusBadRequest)
		return
	}
	kind, text := r.FormValue("kind"), strings.TrimSpace(r.FormValue("text"))
	// Named, not defaulted. The two answers travel the same shape and mean different things, and
	// defaulting to permission turns a question's answer into a decision string the core does not
	// recognise — which reads as "not allow", so the tool is DENIED and the page reports success.
	// A wrong answer delivered silently is worse than one refused.
	if kind != "permission" && kind != "question" {
		http.Error(w, "kind must be permission or question", http.StatusBadRequest)
		return
	}
	if kind == "permission" && text != "allow" && text != "deny" && text != "always" && text != "persist" {
		// The decision vocabulary is the core's, carried through unchanged: a second spelling here
		// would be a place for the two to disagree, and an unrecognised one denies by falling
		// through rather than by saying so.
		http.Error(w, "decision must be allow, deny, always or persist", http.StatusBadRequest)
		return
	}
	err := s.withClient(r, func(cl *daemon.Client, sid session.SessionID) error {
		if kind == "question" {
			return cl.RespondQuestion(context.Background(), command.RespondQuestion{
				SessionID: sid, CallID: callID, Answer: text})
		}
		return cl.RespondPermission(context.Background(), command.RespondPermission{
			SessionID: sid, CallID: callID, Decision: text})
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// shell runs a command in the daemon's workspace and answers with what it wrote.
//
// # What this is, said plainly
//
// It runs as the user who started the daemon, in that daemon's workspace, and it does NOT go
// through the permission policy the bash TOOL goes through. That gate exists to put a human in
// front of what a model wants to run; a person typing a command at a console is already that human,
// and asking them to approve their own keystroke would be a dialog that only ever gets one answer.
//
// So the protection is where it has always been for this console: it is bound to loopback, it
// refuses to start anywhere else, and every state-changing route including this one is behind the
// same-site check. Whoever can reach this page can already reach the agent that runs commands for a
// living.
//
// # Not recorded, like the terminal's
//
// The terminal's own bang-commands are display-only — a block in that process's transcript and
// nothing in the log — and this matches, because the two should not disagree about what a shell run
// is. The output comes back to the caller and is drawn by the page; nothing is appended to the
// session. A second console watching does not see it, which is a real limitation and the same one
// the terminal has.
func (s *server) shell(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) || postOnly(w, r) {
		return
	}
	cmd := strings.TrimSpace(r.FormValue("cmd"))
	if cmd == "" {
		http.Error(w, "no command", http.StatusBadRequest)
		return
	}
	var out string
	var exit int
	err := s.withClient(r, func(cl *daemon.Client, _ session.SessionID) error {
		var e error
		out, exit, e = cl.Shell(cmd)
		return e
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, "shell", map[string]any{"out": out, "exit": exit})
}

func (s *server) interrupt(w http.ResponseWriter, r *http.Request) {
	// A companion on another console is acted on THERE. Nothing about this one can reach it — the
	// socket path is that machine's, and its daemon is not ours to dial.
	if s.forwarded(w, r, s.proxy) || postOnly(w, r) {
		return
	}
	err := s.withClient(r, func(cl *daemon.Client, sid session.SessionID) error {
		return cl.Interrupt(context.Background(), command.Interrupt{SessionID: sid})
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
