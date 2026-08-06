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
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
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
		addr    = flag.String("addr", "127.0.0.1:7777", "address to serve on; loopback by default and it should stay that way")
		cfgDir  = flag.String("config-dir", "", "magi config directory (default: the platform one, honouring MAGI_CONFIG_DIR)")
		workdir = flag.String("workdir", "", "the daemon's workspace (default: the current directory)")
		showVer = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()
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

	srv := &server{reader: reader, cfgDir: cd, here: here, clients: map[string]*daemon.Client{}}
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
	fmt.Fprintf(os.Stderr, "magi-web: http://%s — %d daemon(s) under %s\n", ln.Addr(), countDaemons(cd), cd)
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

	// One client per daemon, opened on first use. A dashboard that dialled on every request would
	// reconnect several times a second; one that dialled all of them at startup would fail on the
	// first dead socket and refuse to show the rest.
	mu      sync.Mutex
	clients map[string]*daemon.Client
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
	list, err := daemon.List(s.cfgDir)
	if err != nil {
		return daemon.Info{}, err
	}
	for _, in := range list {
		if in.Socket == want || (want == "" && in.Socket == s.here) {
			return in, nil
		}
	}
	if want == "" {
		return daemon.Info{}, fmt.Errorf("no daemon in this directory — pick one from the dashboard")
	}
	return daemon.Info{}, fmt.Errorf("no daemon at %s", want)
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
		"/manifest.webmanifest": s.manifest,
		"/icon.svg":             s.icon,
	}
}

// manifest makes the page installable: added to a phone's home screen it opens without the browser
// chrome, which is the difference between "a website about my agents" and something you reach for.
//
// Served rather than inlined as a data: URI because iOS ignores a manifest it cannot fetch, and it
// is small enough that a route costs less than the explanation of the workaround would.
func (s *server) manifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	// display:standalone and a dark theme that matches the page's surface, so the status bar does
	// not flash white on launch. start_url is the fleet: the phone is where you check on things.
	const m = `{
  "name": "magi",
  "short_name": "magi",
  "start_url": "/",
  "scope": "/",
  "display": "standalone",
  "background_color": "#14110d",
  "theme_color": "#211B14",
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
	list, err := fleet.List(r.Context(), s.reader, s.cfgDir, s.here)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
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

	var lastCount int
	var lastSeq int64 = -1
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	for {
		msgs, seq, err := s.reader.SessionState(r.Context(), sid)
		if err == nil && (seq != lastSeq || len(msgs) != lastCount) {
			lastSeq, lastCount = seq, len(msgs)
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
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
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

func (s *server) interrupt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
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
