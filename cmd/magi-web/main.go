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
		http:   &http.Client{Timeout: peerTimeout},
		stream: &http.Client{},
	}
	defer srv.closeAll()
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
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(list); err != nil {
		log.Printf("magi-web: writing the interventions: %v", err)
	}
}

// routes is every path this server answers, in one place.
//
// A list rather than a run of mux.HandleFunc calls because the page links to some of these, and a
// test checks that everything the page references is a path this binary serves — which is the real
// meaning of "self-contained", and cannot be checked against a list that exists only as statements.
func (s *server) routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/":                     s.page,
		"/fleet":                s.fleet,
		"/events":               s.events,
		"/submit":               s.submit,
		"/interrupt":            s.interrupt,
		"/answer":               s.answer,
		"/manifest.webmanifest": s.manifest,
		"/icon.svg":             s.icon,
		"/font/":                s.font,
		"/interventions":        s.interventions,
		"/promote":              s.promote,
		"/skills":               s.skills,
		"/forget":               s.forgetSkill,
		"/context":              s.context,
		"/dispatch":             s.dispatch,
		"/mcp":                  s.mcp,
		"/handoffs":             s.handoffs,
		"/plan":                 s.plan,
		"/compact":              s.compact,
	}
}

//go:embed fonts/*.woff2
var fontFS embed.FS

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
func (s *server) manifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	// display:standalone and a theme colour that matches the page's BACKGROUND — the masthead sits
	// on it directly now, so a surface colour here would draw a band the page does not have. start_url is the fleet: the phone is where you check on things.
	const m = `{
  "name": "magi",
  "short_name": "magi",
  "start_url": "/",
  "scope": "/",
  "display": "standalone",
  "background_color": "#14110d",
  "theme_color": "#14110d",
  "icons": [{"src": "/icon.svg", "sizes": "any", "type": "image/svg+xml", "purpose": "any maskable"}]
}`
	if _, err := io.WriteString(w, m); err != nil {
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
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 192 192">
  <rect width="192" height="192" fill="#211B14"/>
  <circle cx="96" cy="70" r="21" fill="#FFB454"/>
  <circle cx="70" cy="115" r="21" fill="#5CD8E6"/>
  <circle cx="122" cy="115" r="21" fill="#FF8A8A"/>
  <circle cx="96" cy="97" r="43" fill="none" stroke="#FF7A1A" stroke-width="4" opacity=".55"/>
</svg>`
	if _, err := io.WriteString(w, svg); err != nil {
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
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(list); err != nil {
		log.Printf("magi-web: writing the fleet: %v", err)
	}
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
			b, _ := json.Marshal(renderMessages(msgs))
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
type line struct {
	Who  string `json:"who"`
	Text string `json:"text"`
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
					out = append(out, line{Who: string(m.Role), Text: t})
				}
			case session.PartReasoning:
				if t := strings.TrimSpace(p.Text); t != "" {
					out = append(out, line{Who: "thinking", Text: t})
				}
			case session.PartToolCall:
				if p.ToolCall != nil {
					out = append(out, line{Who: "tool", Text: p.ToolCall.Name + " " + fleet.Clip(string(p.ToolCall.Args), 300)})
				}
			case session.PartToolResult:
				if p.ToolResult != nil {
					who := "result"
					if p.ToolResult.IsError {
						who = "failed"
					}
					out = append(out, line{Who: who, Text: fleet.Clip(string(p.ToolResult.Content), 600)})
				}
			}
		}
	}
	return out
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
