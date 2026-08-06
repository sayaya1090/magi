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
	"html"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
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

	sock := daemon.SocketPath(cd, wd)
	cl, err := daemon.Dial(sock)
	if err != nil {
		fmt.Fprintln(os.Stderr, "magi-web:", err)
		return 1
	}
	defer cl.Close()
	sid, err := daemon.PublishedSession(sock)
	if err != nil {
		fmt.Fprintln(os.Stderr, "magi-web:", err)
		return 1
	}

	// The reading half: this process's own store over the same directory. No LLM and no tools —
	// it never runs a turn, and handing it a real provider would be an invitation to.
	store, err := jsonl.New(plat.DataDir())
	if err != nil {
		fmt.Fprintln(os.Stderr, "magi-web: store:", err)
		return 1
	}
	reader := app.New(store, nil, builtin.NewRegistry(), bus.New(), nil, app.Config{})

	srv := &server{reader: reader, cl: cl, sid: session.SessionID(sid)}
	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.page)
	mux.HandleFunc("/events", srv.events)
	mux.HandleFunc("/submit", srv.submit)
	mux.HandleFunc("/interrupt", srv.interrupt)

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
	fmt.Fprintf(os.Stderr, "magi-web: http://%s — watching session %s\n", ln.Addr(), sid)
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

type server struct {
	reader *app.App
	cl     *daemon.Client
	sid    session.SessionID
}

// page is the whole front end. Server-rendered with an inline script and no build step: magi ships
// one static binary with no toolchain behind it, and a bundler for a transcript and a text box
// would be a second thing to keep working.
func (s *server) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, indexHTML, html.EscapeString(string(s.sid)))
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

	var lastCount int
	var lastSeq int64 = -1
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	for {
		msgs, seq, err := s.reader.SessionState(r.Context(), s.sid)
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
					out = append(out, line{Who: "tool", Text: p.ToolCall.Name + " " + clip(string(p.ToolCall.Args), 300)})
				}
			case session.PartToolResult:
				if p.ToolResult != nil {
					who := "result"
					if p.ToolResult.IsError {
						who = "failed"
					}
					out = append(out, line{Who: who, Text: clip(string(p.ToolResult.Content), 600)})
				}
			}
		}
	}
	return out
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
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
	err := s.cl.Steer(context.Background(), command.SubmitPrompt{
		SessionID: s.sid, Parts: []session.Part{{Kind: session.PartText, Text: text}}})
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
	if err := s.cl.Interrupt(context.Background(), command.Interrupt{SessionID: s.sid}); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
