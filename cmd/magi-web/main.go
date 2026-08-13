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
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/auth"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/change"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/core/text"
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
	// Said by the operator, because nothing here can find it out: a request that arrived through a
	// reverse proxy is a loopback connection like any other.
	exposed := flag.Bool("exposed", false,
		"more people than the operator can reach this console (through a proxy or gateway that "+
			"authenticates); refuses the routes that run something chosen by the caller")
	// The certificate this console presents. Its own, not a proxy's: the operator chose to put the
	// authentication in magi rather than in front of it, and a login token crossing plaintext is a
	// login token on the wire — so -exposed refuses to start without these.
	tlsCert := flag.String("tls-cert", "", "certificate to serve TLS with (PEM); required with -exposed")
	tlsKey := flag.String("tls-key", "", "private key for -tls-cert (PEM)")
	// Named rather than sniffed. Every gateway spells it differently (X-Forwarded-User,
	// X-Auth-Request-Email, Cf-Access-Authenticated-User-Email, Tailscale-User-Login), and a list
	// of headers this trusts by default is a list of headers anybody can set.
	groupsHeader := flag.String("groups-header", "",
		"header the authenticating proxy puts the person's groups in (e.g. X-Forwarded-Groups); "+
			"maps to [groups.*] in auth.toml, so joiners and leavers are handled in the directory")
	userHeader := flag.String("user-header", "",
		"header the authenticating proxy in front puts the person's name in, recorded with each "+
			"change (e.g. X-Forwarded-User); unset records where a request came from and not who")
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
	if err := exposedAllows(*exposed, peers); err != nil {
		fmt.Fprintln(os.Stderr, "magi-web:", err)
		return 1
	}
	if err := exposedHasTLS(*exposed, *tlsCert, *tlsKey); err != nil {
		fmt.Fprintln(os.Stderr, "magi-web:", err)
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

	if *emit != "" {
		if err := emitDemo(*emit); err != nil {
			fmt.Fprintln(os.Stderr, "magi-web:", err)
			return 1
		}
		fmt.Println("wrote the demo to", *emit)
		return 0
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

	// Read before anything is served, and a broken one stops the process. A permission file that
	// is present and wrong ends somewhere worse either way — nobody able to do anything, or
	// somebody believing they granted something the build does not have — and this is the moment
	// the operator is looking at what they just wrote.
	policy, autherr := config.LoadAuth(cd)
	if autherr != nil {
		fmt.Fprintln(os.Stderr, "magi-web:", autherr)
		return 1
	}

	if err := policyCanName(policy.Configured(), *userHeader); err != nil {
		fmt.Fprintln(os.Stderr, "magi-web:", err)
		return 1
	}

	// Beside the store, which is where records of what happened live.
	audit, aerr := newAudit(plat.DataDir())
	if aerr != nil {
		fmt.Fprintln(os.Stderr, "magi-web: changes are not being recorded:", aerr)
	}

	srv := &server{
		reader: reader, cfgDir: cd, here: here, clients: map[string]*daemon.Client{},
		peers: peers, exposed: *exposed, userHeader: *userHeader, groupsHeader: *groupsHeader,
		audit: audit, policy: policy,
		// Two clients on purpose. The short one bounds every call that has an answer; the streaming
		// one must not, because an event stream is supposed to stay open and a timeout there would
		// cut the transcript every few seconds.
		http:       &http.Client{Timeout: peerTimeout},
		stream:     &http.Client{},
		embedModel: os.Getenv("MAGI_EMBED_MODEL"),
	}
	defer srv.closeAll()
	defer audit.Close()
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

	ln, err := listenLoopback(*addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "magi-web:", err)
		return 1
	}
	scheme := "http"
	if *tlsCert != "" {
		scheme = "https"
	}
	fmt.Fprintf(os.Stderr, "magi-web: %s://%s — %d companion(s) under %s",
		scheme, ln.Addr(), countDaemons(cd), cd)
	if len(peers) > 0 {
		names := make([]string, len(peers))
		for i, p := range peers {
			names[i] = p.Name
		}
		fmt.Fprintf(os.Stderr, ", federating %s", strings.Join(names, ", "))
	}
	// Said at startup, every time, because an operator who meant to share this console and did not
	// pass the flag has no other way to notice: the page looks identical either way.
	if *exposed {
		fmt.Fprint(os.Stderr, " — shared: no shell, no MCP writes")
		// Worth saying once, at the moment somebody is setting this up: a shared console whose
		// record cannot name anybody is a record of a door with no faces in it.
		if *userHeader == "" {
			fmt.Fprint(os.Stderr, ", changes recorded without a name (-user-header)")
		}
	}
	if policy.Configured() {
		fmt.Fprintf(os.Stderr, " — %d people and %d group(s), by role", len(policy.People), len(policy.Groups))
	} else if *userHeader != "" {
		// Nobody configured and a gateway in front: this console answers everybody it can name,
		// which is the state somebody setting one up passes through and does not always notice.
		// The name is not known until a request arrives — see claimHint, which says it once.
		fmt.Fprint(os.Stderr, " — nobody configured: everybody who reaches it is the operator")
	}
	fmt.Fprintln(os.Stderr)
	srvr := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	var serr error
	if *tlsCert != "" {
		serr = srvr.ServeTLS(ln, *tlsCert, *tlsKey)
	} else {
		serr = srvr.Serve(ln)
	}
	if serr != nil {
		fmt.Fprintln(os.Stderr, "magi-web:", serr)
		return 1
	}
	return 0
}

// exposedAllows refuses the combination of a shared console and federation.
//
// A peer is another machine's console, reached through a tunnel the OPERATOR set up with their own
// keys — magi-web has no credential of its own to present, so every request this console forwards
// travels on the operator's authority. Shared, that turns "somebody the gateway let in" into
// somebody acting as the operator on a second machine, and the audit record on the far side would
// say the request came from here. There is no way to narrow it from this end, so the two are
// refused together rather than half-supported.
// exposedAllows used to refuse -exposed together with -peer, and no longer does.
//
// The reason it refused was real: a crossing to a peer carries no identity, so on a shared console
// anybody admitted here could act as the operator over there, unattributed. But the ban also
// removed the arrangement people actually want — one console, several machines, several people
// looking — to prevent a class of request that can simply be refused instead. It is: see proxy,
// where a change aimed at a peer is turned away and a read is not.
//
// Kept as a function because the shape of the check is worth having when identity does cross the
// hop, and because deleting a rule is a decision that should leave something behind to read.
func exposedAllows(exposed bool, peers []peer) error { return nil }

// exposedHasTLS refuses a shared console with nothing to encrypt it.
//
// The operator chose authentication in magi rather than a proxy in front, and that decision brings
// this one with it: whatever identifies a person — a session cookie today, a bearer token
// tomorrow — crosses this connection, and a token over plaintext is a token on the wire. The
// gateway that authenticates them is the thing on the other end of it.
//
// Loopback does not save it. The port is reached through something forwarding to it, and the hop
// this process can see is the only one it can do anything about.
//
// Not required WITHOUT -exposed: one operator on their own machine, over a unix-domain-shaped
// tunnel they made themselves, gains nothing from a certificate they would have to invent.
func exposedHasTLS(exposed bool, cert, key string) error {
	if !exposed {
		if (cert == "") != (key == "") {
			return errors.New("-tls-cert and -tls-key are a pair; one without the other serves nothing")
		}
		return nil
	}
	if cert == "" || key == "" {
		return errors.New("-exposed needs -tls-cert and -tls-key: this console authenticates people " +
			"itself, so what identifies them crosses this connection — in plaintext it is on the " +
			"wire. Put a certificate here, or drop -exposed and reach it through your own tunnel")
	}
	return nil
}

// policyCanName refuses a permission file nothing can apply.
//
// A policy answers "what may this person do", and the person is whoever the header names. With no
// -user-header there is no name, an unnamed caller is refused by design, and the console starts,
// prints how many people it has, and turns down every request including the ones that would let an
// operator fix it. Nothing about that state works, so it is not a state to start in — the refusal
// belongs here, where somebody is looking at the flags they just typed, and not on a page four
// minutes later telling them to ask an operator.
func policyCanName(configured bool, userHeader string) error {
	if !configured || userHeader != "" {
		return nil
	}
	return errors.New("this console has an " + config.AuthFile + " and no -user-header, so it " +
		"cannot tell who anybody is: every request would be refused, including the ones that " +
		"would change that. Name the header your gateway sets (e.g. -user-header X-Forwarded-User), " +
		"or move the file away to go back to one operator")
}

// listenLoopback binds, and hands back nothing the network can reach.
//
// Refuse rather than warn. This serves a control surface with no authentication of its own: a
// prompt, a shell command, the approval mode, the model, a skill file, a cron job. Bound to a
// routable address it hands the workspace to whoever finds the port, and a warning on a terminal
// nobody is looking at is not a defence.
//
// It is a function rather than four lines in run() so there is ONE place that opens a port and it
// is the place that checks — a second net.Listen elsewhere in this binary would be a second door
// with no lock, which is why a test counts them.
//
// The listener is closed on refusal. Returned open, the port would stay bound for the life of the
// process while the caller reports the failure and exits, and the operator's retry on the same
// address would fail for a reason that has nothing to do with what they typed.
func listenLoopback(addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	if !isLoopback(ln.Addr()) {
		ln.Close()
		return nil, fmt.Errorf("%s is not loopback and this server has no authentication — "+
			"use ssh -L to reach it from elsewhere", addr)
	}
	return ln, nil
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

	// exposed says somebody other than the operator can reach this console — a proxy, an SSO
	// gateway, a tunnel the team shares. The bind guard still holds (this process serves loopback
	// and nothing else); what changes is what a request is allowed to ask for once something in
	// front has let it through.
	//
	// It is the operator's declaration, not a discovery. Nothing this process can measure tells it
	// who is on the other end of a loopback connection that a reverse proxy made, so guessing
	// would be a guess that gets the safe answer wrong in both directions.
	//
	// What it turns off is the two routes that make this MACHINE run something chosen by whoever
	// sent the request, outside the permission policy the agent's own tools go through — see
	// shell() and mcpWrite(). Everything else stays: submitting a prompt, answering a permission,
	// scheduling a job all go through the agent, and refusing them would leave a console that
	// cannot be used for the thing it is for.
	exposed bool

	// policy is who may do what. The zero value — nobody configured — is a console with one
	// operator, which is what every console is until somebody puts a gateway in front of it.
	policy auth.Policy
	// audit is the record of what was asked of this console, or nil when it could not be opened.
	// Nil records nothing rather than refusing to serve: being unable to write the record is not a
	// reason to withhold the console from the person in front of it, and it is said at startup.
	audit *auditLog
	// userHeader is the header a gateway in front puts the authenticated person in, named by the
	// operator because only they know what is in front. Empty means the record says where a
	// request came from and not who sent it. See audit.go.
	userHeader string
	// groupsHeader is where the gateway puts membership, when it reports it. Empty = names only.
	groupsHeader string
	// claimOnce keeps the "nobody has claimed this console" note to one line per process.
	claimOnce sync.Once

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

// refuseWhenShared turns down what only the operator should have, and says which console this is.
//
// The two callers are the routes that make this machine run something the CALLER chose, outside
// the permission policy every tool call goes through: a shell command, and an MCP server's command
// line written into config (a daemon spawns those at startup — the same-site guard exists because
// a page on another origin once wrote one). Everything else the console can do goes through the
// agent, which is the thing the console is for.
//
// The refusal names the flag. Somebody hitting a wall they did not put up needs to know it was put
// up on purpose and by whom, or the next step is to go looking for the bug.
//
// ⚠ A console with an auth.toml is shared whether or not the flag was passed. It binds loopback
// either way, and a gateway on the same machine in front of it is a perfectly ordinary way to run
// this — one that never touches -exposed and, until this read the policy too, kept both of these
// routes open to anybody the gateway let in. The policy is the honest signal: it exists precisely
// because more than one person reaches this console.
func (s *server) shared() bool { return s.exposed || s.policy.Configured() }

func (s *server) refuseWhenShared(w http.ResponseWriter, what string) bool {
	if !s.shared() {
		return false
	}
	why := "it was started with -exposed"
	if !s.exposed {
		why = "it has an " + config.AuthFile + ", so more than one person reaches it"
	}
	http.Error(w, what+" is off on this console: "+why+", which means more "+
		"people than the operator can reach it. Do it from a terminal on that machine.",
		http.StatusForbidden)
	return true
}

// daemonSaysNo turns what came back from a companion into a status.
//
// Everything from a daemon was 502, which says "the machine in the middle could not be reached" —
// and most of these are the opposite: it was reached, it understood, and it said no. A companion
// mid-turn cannot be moved, and a conversation belonging to another workspace is not its to open.
// Reported as a gateway error, a refusal reads as a broken console, and the page's own retry logic
// treats it as one.
//
// 409, because the refusals are all of one kind: what was asked conflicts with the state the
// companion is in. The client's typed Refused is the seam — it exists so a caller can tell "it
// answered no" from "it never answered", and this is the caller that had not been using it.
func daemonSaysNo(err error) int {
	var refused daemon.Refused
	if errors.As(err, &refused) {
		return http.StatusConflict
	}
	return http.StatusBadGateway
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
		return notRunning(in, err)
	}
	if err = do(cl, sid); err == nil {
		return nil
	}
	// A refusal is not a stale connection. The reconnect below exists for a socket the daemon has
	// since replaced — the first call fails on a dead pipe and the second one works — and a
	// companion that answered "no, I am mid-turn" would have that reason asked for twice and
	// answered the same way, which is a second round trip to learn nothing.
	var refused daemon.Refused
	if errors.As(err, &refused) {
		return err
	}
	s.forget(in.Socket)
	cl, derr := s.clientFor(in.Socket)
	if derr != nil {
		// The reconnect can know something the first failure could not: a call that died on a
		// broken pipe says the connection went, and a redial that finds nothing there says the
		// companion did. That is the more useful of the two, and the only one of them that tells
		// somebody their conversation is still on disk.
		if said := notRunning(in, derr); said != derr {
			return said
		}
		return err // otherwise the original failure, not "could not reconnect" — that hides it
	}
	return do(cl, sid)
}

// notRunning says what a companion that is not there means, rather than what the socket did.
//
// A daemon that is off leaves its record behind on purpose: the board still shows what it did, and
// its conversations are files. So the console offers everything about it and only fails at the
// moment somebody sends — with "dial unix …: connect: no such file or directory", which is true
// and answers none of what the person is now wondering. Chiefly: whether the conversation they
// were reading is gone. It is not, and that is the sentence.
func notRunning(in daemon.Info, err error) error {
	// Asked of the daemon package rather than of the operating system. The three errnos below are
	// the same fact seen from three angles — a socket taken away with its daemon (ENOENT), one left
	// behind with nothing accepting (ECONNREFUSED), a path that is not a socket at all (ENOTSOCK) —
	// and which of them a machine produces differs by platform: this check keyed on errno alone and
	// so worked on macOS and failed on Linux, where the dial replaces the syscall with a sentence.
	// ErrGone is the daemon package saying the thing itself.
	if !errors.Is(err, daemon.ErrGone) && !errors.Is(err, fs.ErrNotExist) &&
		!errors.Is(err, syscall.ECONNREFUSED) && !errors.Is(err, syscall.ENOTSOCK) {
		return err
	}
	where := in.Workdir
	if where == "" {
		where = "its workspace"
	}
	return fmt.Errorf("%s is not running, so there is nothing to send to. Its conversations are on "+
		"disk and will still be there — start it again in %s and this one opens where it was left",
		nameOfSocket(in.Socket), where)
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
	// Cached and revalidated, like the assets under it. The document carries the whole front end,
	// so a browser holding yesterday's copy is a person looking at yesterday's console — reporting
	// bugs that were fixed and not seeing controls that exist. With no Cache-Control at all a
	// browser applies its own heuristic, which is the worst of both: sometimes stale, never
	// predictably. The ETag makes the check a round trip with no body.
	sum := sha256.Sum256([]byte(indexHTML))
	etag := "\"" + hex.EncodeToString(sum[:8]) + "\""
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
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
	// Scoped for the same reason the fleet is, and more so: this carries what a person SAID to each
	// companion, verbatim. A list of names is an inventory; this is the correspondence.
	list = onlySeen(s, r, list, func(m fleet.Moment) (string, string) { return m.Companion, m.Peer })
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
		// Three wrappers, and the order is the argument. Audit outermost, because a cross-site
		// POST that never reaches a handler is the line in that record somebody would actually
		// want. Then the cross-site guard, which is about the BROWSER and applies to everybody
		// including the operator. Then may-do, which is about the person — asked last, so a
		// forged cross-site request is turned away before anybody's permissions are consulted.
		out[path] = s.audited(sameSiteOnly(s.claiming(s.mayDo(path, h))))
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
		"/resume":               s.resume,
		"/shell":                s.shell,
		"/cron":                 s.cron,
		"/search":               s.search,
		"/answer":               s.answer,
		"/manifest.webmanifest": s.manifest,
		"/icon.svg":             s.icon,
		"/icon-maskable.svg":    s.iconMaskable,
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
		"/report-format": s.reportFormat,
		"/remember":      s.remember,
		"/context":       s.context,
		"/dispatch":      s.dispatch,
		"/mcp":           s.mcp,
		"/handoffs":      s.handoffs,
		"/subagents":     s.subagents,
		"/jobs":          s.jobs,
		"/tools":         s.tools,
		"/model":         s.models,
		"/loop":          s.loop,
		"/transcript":    s.transcript,
		"/council":       s.council,
		"/plan":          s.plan,
		"/compact":       s.compact,
		"/permission":    s.permission,
		"/console":       s.console,
		"/me":            s.me,
		"/access":        s.access,
		"/files":         s.files,
		"/file":          s.file,
		"/find":          s.find,
		"/save":          s.save,
		"/git":           s.git,
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
	// Cached and REVALIDATED, which are not the same thing. These were served immutable for a day
	// on the reasoning that they change with a release — and the consequence is that they change
	// with a release and nobody sees it: an upgraded console served its new pack to a browser that
	// went on using yesterday's for up to a day, so a label added in the same build rendered as its
	// own dotted key. Observed twice while working on this page, both times read as a bug in the
	// page rather than as a stale file.
	//
	// no-cache is "ask first", not "do not store". The browser keeps the bytes and gets a 304 back
	// on every check, which costs a round trip with no body and is always right.
	sum := sha256.Sum256(b)
	etag := "\"" + hex.EncodeToString(sum[:8]) + "\""
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
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
  "icons": [
    {"src": "/icon.svg", "sizes": "any", "type": "image/svg+xml", "purpose": "any"},
    {"src": "/icon-maskable.svg", "sizes": "any", "type": "image/svg+xml", "purpose": "maskable"}
  ]
}`

const iconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 192 192">
  <circle cx="96" cy="70" r="21" fill="#FFB454"/>
  <circle cx="70" cy="115" r="21" fill="#5CD8E6"/>
  <circle cx="122" cy="115" r="21" fill="#FF8A8A"/>
  <circle cx="96" cy="97" r="43" fill="none" stroke="#FF7A1A" stroke-width="4" opacity=".55"/>
</svg>`

// iconMaskableSVG is the same three councillors with the plate back under them.
//
// A maskable icon is not a picture with rounded corners applied — the platform crops it to
// whatever shape it likes, circle on one launcher and squircle on the next, and a transparent one
// is cropped to nothing with the launcher filling the rest in a colour this file did not choose.
// So this one keeps the ground, and it is the only place that needs it.
const iconMaskableSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 192 192">
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

// icon is the mark: the three councillors, in their own hues, on nothing.
//
// SVG so there is one file for every size, and drawn here rather than shipped as a PNG because a
// binary asset in a source tree is a thing nobody can review. The maskable safe zone is the middle
// 80%, so nothing meaningful goes near the edge.
//
// No ground under it. This is the favicon, and a tab strip is whatever colour the browser and its
// theme make it — a dark brown square there is a sticker on the tab rather than a mark in it. The
// same file is the notification icon, where a launcher tints what it is given and a plate is a
// plate. Where a ground IS required, there is a second file that has one; see iconMaskableSVG.
func (s *server) icon(w http.ResponseWriter, r *http.Request) {
	s.svg(w, iconSVG)
}

// iconMaskable is the same mark for a home screen, which crops it.
func (s *server) iconMaskable(w http.ResponseWriter, r *http.Request) {
	s.svg(w, iconMaskableSVG)
}

func (s *server) svg(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "image/svg+xml")
	if _, err := io.WriteString(w, body); err != nil {
		log.Printf("magi-web: writing an icon: %v", err)
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
	// And then only the ones this person may see. This is the list every other screen is read from
	// — the board, the count in the masthead, the dispatch roster — so a scope that left it whole
	// hid nothing: it carries every companion's name, workspace path, host and current task.
	writeJSON(w, "fleet", onlySeen(s, r, list, func(a fleet.Agent) (string, string) { return a.Name, a.Peer }))
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
	// This stream was addressed by COMPANION, and a companion can now be pointed at another of its
	// own conversations from any console. So the session is re-read rather than resolved once: a
	// page watching a companion that moved was left polling the conversation it left, for as long
	// as the tab stayed open, while everything it typed went to the new one — the shape where what
	// is on screen and what a control reaches are two different things.
	//
	// Every fifth tick rather than every tick: it is a small file read, the poll beside it runs two
	// and a half times a second per viewer, and two seconds is well inside the time it takes to
	// read that a conversation has moved. A screen addressed by SESSION (the past view) must not
	// follow, and does not — it never opens this stream.
	const lookEvery = 5
	look := 0
	for {
		if look++; look >= lookEvery {
			look = 0
			if now, rerr := s.target(r); rerr == nil && now.Session != "" &&
				session.SessionID(now.Session) != sid {
				// Everything from the start of the new conversation, in the next frame: the page
				// draws whatever it is sent, so a reset here IS the redraw.
				sid, lastSeq = session.SessionID(now.Session), -1
			}
		}
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
			rows := markPending(renderMessages(msgs))
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
	// Note marks a call that DID what it was asked and left something to read: a post-edit hook,
	// or a language server's complaint about the file just written. Those come back marked as
	// errors on purpose — that is what makes the agent read them instead of moving on — and Ok is
	// drawn from the same field, so a file that was written and then linted appeared as a write
	// that FAILED. Reported live, with the file on disk and the model carrying on.
	Note bool `json:"note,omitempty"`
	// Ok reports how a tool call ended, and is nil while it has not. The call and its result are
	// ONE row: the terminal has always drawn them that way, and split across two the question a
	// reader has — did that work — could only be answered by finding the row below and opening it.
	Ok *bool `json:"ok,omitempty"`
	// Out is what a failed call wrote. Its own field and not Args: what a tool was ASKED and what
	// it then said are two different things, and the first attempt at this overwrote one with the
	// other — so the row that told you a call had failed had stopped telling you what it ran.
	Out string `json:"out,omitempty"`
	// Pending marks the last user prompt when nothing has answered it yet. Whether a prompt is
	// being worked on is a fact about it, and the page had no way to show that at all.
	Pending bool `json:"pending,omitempty"`
	// Round is which round a council row belongs to. The tally is not a field: councilText writes
	// it into the row's own text, and a second copy on the wire was one the page never read.
	Round int `json:"round,omitempty"`
	// Decision is the council vocabulary as the log spells it, so the page can colour by it.
	Decision string `json:"decision,omitempty"`
	// Member is which councillor said it, so the page can give each their own hue — the same three
	// the terminal uses. It is in the text too, and reading it back out of a rendered sentence is
	// how the two surfaces come to disagree about who spoke.
	Member string `json:"member,omitempty"`
	// At is when the message this row came out of began, RFC 3339, or empty when the log did not
	// record one. The terminal has stamped its blocks with it since it had a transcript; the page
	// showed a conversation with no times in it anywhere, which is the first thing somebody
	// returning to a companion after lunch wants to know about what they are reading.
	At string `json:"at,omitempty"`
	// Diff is an edit or a write drawn as the change it makes, built from the call's OWN arguments.
	//
	// The terminal has shown this since it had a transcript, and the console showed the arguments:
	// a JSON blob with the old and new text escaped into one line, which is the least readable form
	// of the most important thing an agent does. Built here rather than in the page because the
	// terminal's diff is Go and there is no second implementation worth having — the page already
	// knows how to colour one.
	Diff string `json:"diff,omitempty"`
	// Abandoned marks a prompt no turn will ever answer — cancelled, or merged into a later one.
	// Without it a cancelled request reads as a question the companion ignored.
	Abandoned bool `json:"abandoned,omitempty"`
	// By is which of magi's own parts wrote a system row — the orchestrator, the planner. The
	// terminal has named it since these notes existed; the console called all of them magi.
	By string `json:"by,omitempty"`
	// msg is the message this row came out of, and never crosses the wire. It is how council marks
	// find the place they belong: they carry the same id, out of the same log.
	msg string `json:"-"`
}

// renderMessages flattens the session into rows. Tool calls keep their arguments: what a tool was
// asked to do is most of what a watcher wants to know, and the name alone is the progress line
// that was not enough on the pane strip either.
func renderMessages(msgs []session.Message) []line {
	// Results first, by call id, so a call can be drawn as one row that already knows how it ended.
	// Parallel calls complete out of order, so the pairing is by id and not by position.
	results := map[string]*session.ToolResult{}
	for _, m := range msgs {
		for _, p := range m.Parts {
			if p.Kind == session.PartToolResult && p.ToolResult != nil {
				results[p.ToolResult.CallID] = p.ToolResult
			}
		}
	}

	var out []line
	for _, m := range msgs {
		// One stamp per message, on every row it produces: a turn's reasoning, its calls and its
		// answer all belong to the same moment, and the log records the moment once.
		at := ""
		if !m.At.IsZero() {
			at = m.At.Format(time.RFC3339)
		}
		by := m.Author
		for _, p := range m.Parts {
			switch p.Kind {
			case session.PartText:
				if t := strings.TrimSpace(p.Text); t != "" {
					out = append(out, line{Who: string(m.Role), Text: t, msg: m.ID, At: at, By: by,
						Abandoned: m.Abandoned})
				}
			case session.PartReasoning:
				if t := strings.TrimSpace(p.Text); t != "" {
					out = append(out, line{Who: "thinking", Text: t, msg: m.ID, At: at})
				}
			case session.PartToolCall:
				if p.ToolCall != nil {
					// Arguments are no longer clipped at 300 bytes. They were, because the row was
					// always open and a long call pushed the conversation off the screen; the row
					// folds now, and what a tool was asked is most of what a watcher wants when
					// they open it.
					row := line{Who: "tool", Text: p.ToolCall.Name,
						Tool: p.ToolCall.Name, Args: string(p.ToolCall.Args), msg: m.ID, At: at}
					// What the edit actually does, when the call is one. Only for a call that has
					// not failed: the arguments of a rejected edit describe a change that never
					// happened, and drawing it as a diff would show the file as it is not.
					// A refused edit describes a change that never happened. One that landed and
					// then got a diagnostic did happen, and its diff is the thing to look at.
					if res, ok := results[p.ToolCall.CallID]; !ok || !res.IsError || res.Advisory {
						row.Diff = change.EditDiff(p.ToolCall.Name, string(p.ToolCall.Args))
					}
					if res, ok := results[p.ToolCall.CallID]; ok {
						good := !res.IsError
						row.Ok, row.Note = &good, res.Advisory
						// What it ANSWERED, whether or not it failed.
						//
						// Only failures used to travel, on the reasoning that a success is noise
						// and the arguments are the more useful thing to keep. That is wrong twice
						// over: the row folds, so a success costs nothing until somebody opens it —
						// and when they did open it they got the arguments they had just read in
						// the summary line, twice, with the answer nowhere. "What did the grep
						// find" is most of why anybody opens a tool call, and the terminal has
						// always shown it.
						// Head AND tail, with the omitted size named. Clipped from the front
						// alone, a two-hundred-kilobyte build log arrived as the first eight
						// kilobytes — the part where everything was still going fine — and the
						// failure at the end, which is the reason anybody opened the row, was
						// exactly what got dropped. The same treatment the tool's own output gets
						// before the model reads it, out of the same function.
						row.Out = text.HeadTail(string(res.Content), 8000)
					}
					out = append(out, row)
				}
			case session.PartToolResult:
				// Drawn on its own only when no call claimed it. A result whose call is in this
				// transcript is already that call's row.
				if p.ToolResult != nil && !claimed(msgs, p.ToolResult.CallID) {
					who := "result"
					if p.ToolResult.IsError {
						who = "failed"
					}
					// Still clipped, and by much more than before. A result can be a whole build
					// log, and the transcript is rebuilt and re-sent on every change — this bound
					// is about what crosses the wire two and a half times a second, not about what
					// fits on the screen.
					out = append(out, line{Who: who,
						Text: text.HeadTail(string(p.ToolResult.Content), 8000), msg: m.ID, At: at})
				}
			// The two that were being dropped on the floor. A part kind this switch does not name
			// is not an empty row — it is a row that never existed, with nothing anywhere saying
			// so. An image and an error both reached the log and neither reached the page.
			case session.PartImage:
				if p.Image != nil {
					out = append(out, line{Who: "image", Text: p.Image.Path, msg: m.ID, At: at})
				}
			case session.PartError:
				if t := strings.TrimSpace(p.Err); t != "" {
					out = append(out, line{Who: "error", Text: t, msg: m.ID, At: at})
				}
			}
		}
	}
	return out
}

// claimed reports whether a tool call with this id is in the transcript.
func claimed(msgs []session.Message, callID string) bool {
	for _, m := range msgs {
		for _, p := range m.Parts {
			if p.Kind == session.PartToolCall && p.ToolCall != nil && p.ToolCall.CallID == callID {
				return true
			}
		}
	}
	return false
}

// markPending flags the last user prompt when nothing in the transcript answers it.
//
// Whether a prompt is being worked on is a fact about that prompt, and the console could not show
// it at all: every row looked the same whether it had been answered an hour ago or was the one the
// companion is thinking about now. The terminal has drawn a bar on it since it existed.
//
// Only the LAST one, and only "nothing has answered it". Queued and abandoned are states the
// terminal knows because it watched them happen; they are not recoverable from the transcript
// alone, and a page claiming them from a guess would be worse than a page saying less.
func markPending(rows []line) []line {
	if len(rows) == 0 {
		return rows
	}
	last := &rows[len(rows)-1]
	switch {
	// Not an abandoned one. The log says outright that nothing will answer it, and a spinner over
	// a request that was cancelled is the console inventing work.
	case last.Who == "user" && last.Abandoned:
	case last.Who == "user":
		last.Pending = true
	// A tool call at the very end with no result yet is the call running right now. Only at the
	// end: Ok is also nil for a call whose result never came — an interrupted turn, or a
	// compaction that took the result — and a spinner on one of those would be claiming work that
	// stopped hours ago. If anything follows it, it is over however it ended.
	case last.Who == "tool" && last.Ok == nil:
		last.Pending = true
	}
	return rows
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
		row := line{Who: "council", Text: councilText(m), Round: m.Round,
			Decision: m.Decision, Member: m.Member}
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
	// The mark and the word, and NOT the member: the gutter beside this row is that councillor's
	// name already (whoWord in page.js), so putting it here read "balthasar ✗ balthasar: reject".
	// The terminal has the same fact in the same place, and neither says it twice.
	head := councilIcon(m.Decision) + " " + councilWord(m.Decision)
	if m.Lens != "" {
		head += " (" + m.Lens + ")"
	}
	// How sure, where the terminal puts it: on the line with the vote. A vote at 55% and a vote at
	// 95% are different votes and the number was on the wire the whole time.
	if m.Confidence > 0 {
		head += fmt.Sprintf(" · %d%%", int(m.Confidence*100+0.5))
	}
	var body []string
	if m.Why != "" {
		body = append(body, m.Why)
	}
	// The rest of the vote, in the words the terminal uses for it.
	//
	// This showed the rationale and the citation; the terminal showed the feedback and the keep.
	// Two surfaces reading one record and each dropping the half the other kept — so a council read
	// in the console and the same council read in the terminal disagreed about what was said.
	// Reported live, and the fix is that neither drops anything.
	if f := strings.TrimSpace(m.Feedback); f != "" && f != m.Why {
		body = append(body, "feedback: "+f)
	}
	// What must survive a revision. Not gated on the vote: an approving member's keep is exactly
	// the part a rewrite forced by another member's objection would drop, and it reaches the model
	// — so a reader who cannot see it cannot tell why the next turn protected something.
	if k := strings.TrimSpace(m.Keep); k != "" {
		body = append(body, "keep: "+k)
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
		http.Error(w, err.Error(), daemonSaysNo(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// permission changes which tool calls this companion stops for, and how long it waits when it does.
//
// The mode is per companion and it changes at runtime — the terminal has had Shift+Tab and
// /permission since it had a prompt, and this is the same call over the same socket. A console that
// can answer a prompt and cannot change what raises one leaves the person holding the wrong end:
// they can grant the fiftieth confirmation and not the thing that stops asking for it.
//
// It is a bigger lever than granting one call, which is why it is a POST like every other action
// here, same-site only, and forwarded to the console that owns the companion rather than dialled
// across. What it cannot do is exceed what the operator already allowed: the guardrail rules in
// config (allow/deny globs, allow_domains) are read on every call and are not reachable from here.
func (s *server) permission(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) || postOnly(w, r) {
		return
	}
	mode := strings.TrimSpace(r.FormValue("mode"))
	switch mode {
	case "ask", "auto", "allow", "deny":
	default:
		http.Error(w, "mode must be ask, auto, allow or deny", http.StatusBadRequest)
		return
	}
	// Loosening the approval axis is answering, in advance and for everything. `allow` approves
	// every tool call this companion makes and `auto` approves the edits, so a role holding
	// `configure` alone — "may set how it runs, may not tell it yes" — could do in one post what
	// /answer exists to govern one call at a time. Tightening it (`ask`, `deny`) needs no such
	// thing: putting a person back in the loop is not a way around the person.
	if mode == "allow" || mode == "auto" {
		if who := s.whoFrom(r); !s.policy.Allows(who, auth.Answer,
			nameOfSocket(r.URL.Query().Get("d")), r.URL.Query().Get("p")) {
			http.Error(w, refusal(who, auth.Answer)+" — "+mode+" answers every request this "+
				"companion would have made", http.StatusForbidden)
			return
		}
	}
	if err := s.withClient(r, func(cl *daemon.Client, _ session.SessionID) error {
		return cl.SetPermission(mode)
	}); err != nil {
		http.Error(w, err.Error(), daemonSaysNo(err))
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
		http.Error(w, err.Error(), daemonSaysNo(err))
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
	// First, and before the forward: a shared console must not run a command for anybody, and it
	// must not carry one to another machine either.
	if s.refuseWhenShared(w, "running a command") {
		return
	}
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
		http.Error(w, err.Error(), daemonSaysNo(err))
		return
	}
	writeJSON(w, "shell", map[string]any{"out": out, "exit": exit})
}

// resume points a companion at another of its own conversations.
//
// The session travels in the BODY and not as the target: `?d=` names the companion, the way every
// other route here does, and the body says which conversation of that companion is meant. A route
// that took a session as its address would be the console naming sessions, which is the thing this
// whole arrangement does not do — it names a companion and reads the session out of the record.
//
// Every refusal comes from the daemon: mid-turn, not-my-workspace, could-not-publish. This end
// does not pre-judge any of them, because the answer is only true at the daemon and a console
// asking a question it then answers itself is two truths.
func (s *server) resume(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) || postOnly(w, r) {
		return
	}
	want := strings.TrimSpace(r.FormValue("session"))
	if want == "" {
		http.Error(w, "no conversation named", http.StatusBadRequest)
		return
	}
	if err := s.withClient(r, func(cl *daemon.Client, _ session.SessionID) error {
		return cl.Resume(session.SessionID(want))
	}); err != nil {
		http.Error(w, err.Error(), daemonSaysNo(err))
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
		http.Error(w, err.Error(), daemonSaysNo(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
