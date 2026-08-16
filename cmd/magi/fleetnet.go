package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
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
	"github.com/sayaya1090/magi/internal/adapter/identity"
)

// The fleet door over the network, for machines that do not want ssh between them.
//
// # Same door, different pipe
//
// door.go is the rule — four methods, and only companions this account published. This file is
// one way of getting bytes to it: a TLS listener instead of an ssh forced command. Everything the
// door refuses is refused here too, because it is the same two functions being called.
//
// # Who the other side is
//
// mTLS, and both ends check the same thing: the public key that arrived, against the list of
// fingerprints somebody admitted by hand. No certificate authority — see the identity package for
// why, and for what replaces it.
//
// The check runs in the TLS handshake, which is the point. A party nobody admitted never reaches a
// route, a parser, or a version string: what it learns from trying is that the handshake failed.
//
// # One route, and the request says the rest
//
// The console is dozens of routes and a person's session; this is one route and a machine's key.
// Keeping them apart is deliberate — the console's identity comes from a gateway in front of it
// and can be shaped by a header, and nothing shaped by a header should decide whether another
// machine may hand this one work.
const fleetPath = "/fleet"

// fleetJoinPath is where an invitation is spent. Its own route because it is the one thing a
// machine that has NOT been admitted may reach, and only while somebody is inviting.
const fleetJoinPath = "/fleet/join"

// fleetCrossTimeout bounds one crossing, the way the ssh path is bounded by the context that kills
// its process.
const fleetCrossTimeout = 30 * time.Second

// fleetServe answers admitted machines until ctx ends.
func fleetServe(ctx context.Context, addr, configDir, host string, errOut io.Writer) int {
	id, err := identity.Load(configDir, host)
	if err != nil {
		fmt.Fprintln(errOut, "magi: no fleet identity:", err)
		return 1
	}
	mux := http.NewServeMux()
	mux.HandleFunc(fleetPath, func(w http.ResponseWriter, r *http.Request) {
		// Re-checked here, because the handshake may have let a stranger through: a join window
		// accepts any certificate so the one route below can ask for the secret. Everything else
		// is for parties already on the list.
		if _, ok := identity.PeerOf(configDir, peerCerts(r)); !ok {
			answerFleet(w, daemon.Response{Err: "this machine has not admitted you"})
			return
		}
		fleetHandle(w, r, configDir)
	})
	mux.HandleFunc(fleetJoinPath, func(w http.ResponseWriter, r *http.Request) {
		fleetJoinHandle(w, r, configDir, id)
	})
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{id.Cert},
			MinVersion:   tls.VersionTLS13,
			// Any certificate, and then the fingerprint decides. RequireAndVerifyClientCert would
			// send Go looking for a chain to a root, which is exactly the authority this design
			// does without — so the requirement is "present one" and the verification is ours.
			ClientAuth:            tls.RequireAnyClientCert,
			VerifyPeerCertificate: identity.VerifyAdmittedOrInviting(configDir),
		},
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintln(errOut, "magi:", err)
		return 1
	}
	go func() {
		<-ctx.Done()
		// Close rather than Shutdown: a crossing is one short request, and a door that waits for
		// in-flight work to drain is a door that stays open while somebody is trying to shut it.
		if cerr := srv.Close(); cerr != nil {
			fmt.Fprintln(errOut, "magi: closing the fleet door:", cerr)
		}
	}()
	fmt.Fprintf(errOut, "magi: fleet door on %s — %s\n", ln.Addr(), id.Fingerprint())
	if err := srv.ServeTLS(ln, "", ""); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(errOut, "magi:", err)
		return 1
	}
	return 0
}

// fleetHandle carries one request to one companion and answers with its reply.
//
// Request and reply for three of the four methods: ask what a companion is, hand it work, ask how
// that work went. `watch` streams — the daemon pushes a frame per change and closes when there is
// nothing more coming — so for it the reply is the response BODY held open, one JSON line per
// frame, flushed as each arrives. Same rule as the ssh door's relay: the connection is the
// stream's, and it ends with it.
func fleetHandle(w http.ResponseWriter, r *http.Request, configDir string) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		http.Error(w, "unreadable request", http.StatusBadRequest)
		return
	}
	var ask struct {
		Socket string `json:"socket"`
		daemon.Request
	}
	if json.Unmarshal(body, &ask) != nil {
		answerFleet(w, daemon.Response{Err: "malformed request"})
		return
	}
	if !doorAllows(ask.Method) {
		answerFleet(w, daemon.Response{Err: "a fleet door carries " + doorMethods +
			" and nothing else; " + ask.Method + " has to be asked for on that machine"})
		return
	}
	socket, ok := publishedHere(configDir, ask.Socket)
	if !ok {
		// Named and refused, and no more: a caller who may not reach a companion may not learn
		// which ones are here by asking for names until one works.
		answerFleet(w, daemon.Response{Err: "no companion of this account answers there"})
		return
	}
	conn, err := net.Dial("unix", socket)
	if err != nil {
		answerFleet(w, daemon.Response{Err: "that companion is not running"})
		return
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(ask.Request); err != nil {
		answerFleet(w, daemon.Response{Err: "could not pass it on"})
		return
	}
	if ask.Method == "watch" {
		fleetWatchRelay(w, r, conn)
		return
	}
	line, err := bufioReadLine(conn)
	if err != nil && len(line) == 0 {
		answerFleet(w, daemon.Response{Err: "that companion stopped answering"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, werr := w.Write(line); werr != nil {
		log.Printf("magi: passing a companion's answer back: %v", werr)
	}
}

// fleetWatchRelay streams a watch's frames into the response body until the daemon closes the
// stream or the caller leaves.
//
// The caller leaving is noticed through the request context, and what it has to unblock is the
// READ on the unix socket — a watch nothing is happening in writes nothing, so without the close
// below a watcher whose link died would hold this goroutine (and the daemon's) until the daemon
// stopped. Closing the connection is also how the daemon learns its peer hung up, the same signal
// an ssh relay's death sends.
func fleetWatchRelay(w http.ResponseWriter, r *http.Request, conn net.Conn) {
	fl, ok := w.(http.Flusher)
	if !ok {
		answerFleet(w, daemon.Response{Err: "this door cannot stream"})
		return
	}
	go func() {
		<-r.Context().Done()
		conn.Close() // double-close with the handler's defer is fine; unblocking the read is the point
	}()
	w.Header().Set("Content-Type", "application/json")
	for {
		line, err := bufioReadLine(conn)
		if len(line) > 0 {
			if _, werr := w.Write(line); werr != nil {
				return // the caller is gone; the deferred close tells the daemon
			}
			fl.Flush()
		}
		if err != nil {
			return // the daemon closed the stream: nothing more is coming
		}
	}
}

// answerFleet writes one refusal or reply.
//
// A failure here is the caller having gone, which nothing can be done about — so it is logged, the
// way the console logs an answer it could not finish writing, rather than returned to a handler
// that has no move left either.
// fleetJoinHandle takes an invitation and records the two machines in each other's lists.
//
// Both directions in one exchange, which is the whole point of provisioning: the caller's key is
// admitted here from the certificate it presented — not from anything it typed, so it cannot claim
// somebody else's — and this machine's own fingerprint goes back so the caller can pin it.
func fleetJoinHandle(w http.ResponseWriter, r *http.Request, configDir string, id *identity.Identity) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST", http.StatusMethodNotAllowed)
		return
	}
	certs := peerCerts(r)
	if len(certs) == 0 {
		answerFleet(w, daemon.Response{Err: "a join needs a certificate to admit"})
		return
	}
	var ask struct {
		Token string `json:"token"`
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<10))
	if json.Unmarshal(body, &ask) != nil || strings.TrimSpace(ask.Token) == "" {
		answerFleet(w, daemon.Response{Err: "a join needs an invitation"})
		return
	}
	label, ok := identity.Redeem(configDir, ask.Token)
	if !ok {
		// One sentence for wrong, spent and expired alike: which of the three it was is not the
		// caller's business, and saying would turn this into an oracle.
		answerFleet(w, daemon.Response{Err: "that invitation is not open"})
		return
	}
	fp := identity.FingerprintOf(certs[0])
	if _, err := identity.Admit(configDir, identity.Peer{Fingerprint: fp, Label: label}); err != nil {
		answerFleet(w, daemon.Response{Err: "could not record you: " + err.Error()})
		return
	}
	log.Printf("magi: admitted %s as %s by invitation", fp, label)
	answerFleet(w, daemon.Response{OK: true, Out: id.Fingerprint()})
}

// peerCerts is what the other end presented, or nothing when the connection is not TLS.
func peerCerts(r *http.Request) []*x509.Certificate {
	if r.TLS == nil {
		return nil
	}
	return r.TLS.PeerCertificates
}

func answerFleet(w http.ResponseWriter, resp daemon.Response) {
	w.Header().Set("Content-Type", "application/json")
	b, err := json.Marshal(resp)
	if err != nil {
		log.Printf("magi: a fleet answer would not encode: %v", err)
		return
	}
	if _, werr := w.Write(append(b, '\n')); werr != nil {
		log.Printf("magi: answering a fleet crossing: %v", werr)
	}
}

func bufioReadLine(r io.Reader) ([]byte, error) {
	var out []byte
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			out = append(out, buf[0])
			if buf[0] == '\n' {
				return out, nil
			}
			if len(out) > 4<<20 {
				return out, fmt.Errorf("answer too long")
			}
		}
		if err != nil {
			return out, err
		}
	}
}

// fleetDial is the other end: a ReadWriteCloser that looks like a pipe to daemon.Over and is one
// HTTPS request per line underneath.
//
// Shaped this way so nothing above it changes. The daemon client writes a request and reads a
// reply; that is exactly one POST for the three alternating methods, and for `watch` — the one
// that streams — the POST's response body IS the stream: it stays open, and Read hands frames
// over as the far side flushes them. A transport that buffered a stream into one reply would be
// a watch that says nothing until it is over, which is the poll it exists to replace.
type fleetDial struct {
	client *http.Client
	url    string
	socket string
	// ctx bounds the crossing's LIFE, from whoever opened it (reachCompanion's caller). It is what
	// unwinds a watch blocked on a quiet stream — a pipe crossing dies with its ssh process on the
	// same cancel, and this is that lever for a connection that has no process to kill.
	ctx context.Context

	mu     sync.Mutex
	next   bytes.Buffer  // the reply waiting to be read (the alternating methods)
	stream io.ReadCloser // a watch's live body; non-nil while one is open
}

func (f *fleetDial) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var req daemon.Request
	if err := json.Unmarshal(p, &req); err != nil {
		return 0, fmt.Errorf("fleet: %w", err)
	}
	body, err := json.Marshal(struct {
		Socket string `json:"socket"`
		daemon.Request
	}{Socket: f.socket, Request: req})
	if err != nil {
		return 0, err
	}
	send := func(rctx context.Context) (*http.Response, error) {
		httpReq, err := http.NewRequestWithContext(rctx, http.MethodPost, f.url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := f.client.Do(httpReq)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("fleet: %s answered %s", f.url, resp.Status)
		}
		return resp, nil
	}
	if req.Method == "watch" {
		// No timeout on a watch: it is meant to stay open for as long as the work takes, ended by
		// the far side closing the stream or by f.ctx — the crossing's own life. The body is the
		// stream; Read serves from it until then. An old stream — there should be none, a client
		// watches once — is closed rather than leaked.
		resp, err := send(f.ctx)
		if err != nil {
			return 0, err
		}
		if f.stream != nil {
			f.stream.Close()
		}
		f.stream = resp.Body
		return len(p), nil
	}
	// The bound is per request, not on the client: a client-level timeout would cut a watch's
	// stream at the crossing bound. This is the same bound at the right grain.
	rctx, cancel := context.WithTimeout(f.ctx, fleetCrossTimeout)
	defer cancel()
	resp, err := send(rctx)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return 0, err
	}
	f.next.Write(answer)
	if !bytes.HasSuffix(answer, []byte("\n")) {
		f.next.WriteByte('\n')
	}
	return len(p), nil
}

func (f *fleetDial) Read(p []byte) (int, error) {
	f.mu.Lock()
	if f.next.Len() > 0 {
		defer f.mu.Unlock()
		return f.next.Read(p)
	}
	stream := f.stream
	f.mu.Unlock()
	if stream == nil {
		return 0, io.EOF
	}
	// Read OUTSIDE the lock: a stream blocks until the far side has something to say, and holding
	// the mutex across that would make Close — the one way out of a wait whose link died — wait
	// with it.
	return stream.Read(p)
}

func (f *fleetDial) Close() error {
	f.mu.Lock()
	if f.stream != nil {
		f.stream.Close()
		f.stream = nil
	}
	f.mu.Unlock()
	f.client.CloseIdleConnections()
	return nil
}

// fleetTo opens a crossing to an admitted machine over TLS.
//
// The peer's fingerprint is checked the same way ours is checked over there: the connection is to
// an address, and an address is not an identity — whoever answers it has to be the party this
// machine admitted, or the handshake ends.
func fleetTo(ctx context.Context, configDir, host string, peer identity.Peer, socket string) (*fleetDial, error) {
	id, err := identity.Load(configDir, host)
	if err != nil {
		return nil, err
	}
	want := peer.Fingerprint
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{id.Cert},
		MinVersion:   tls.VersionTLS13,
		// The hostname is not what is being verified — a self-signed certificate has no name worth
		// checking — so Go's own verification is turned off and replaced, not dropped. The peer's
		// public key is compared against the one that was admitted.
		InsecureSkipVerify: true, //nolint:gosec // pinned by fingerprint below
		VerifyPeerCertificate: identity.VerifyAdmitted(configDir, func(p identity.Peer) bool {
			return p.Fingerprint == want
		}),
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return &fleetDial{
		// No client-level Timeout: it would bound a watch stream too. The alternating methods get
		// their fleetCrossTimeout per request in Write, which is the same bound at the right grain.
		client: &http.Client{
			Transport: &http.Transport{TLSClientConfig: tlsCfg, ForceAttemptHTTP2: true},
		},
		url:    "https://" + peer.Addr + fleetPath,
		socket: socket,
		ctx:    ctx,
	}, nil
}

// peerFor finds the admitted machine a hostname belongs to, if this magi reaches out to it.
//
// By label first and then by the host part of the address, because both are things a person writes
// and either is a reasonable way to have written it. A machine with no address on its line is one
// that reaches US, and there is nothing here to dial.
func peerFor(configDir, host string) (identity.Peer, bool) {
	host = strings.TrimSpace(host)
	if host == "" {
		return identity.Peer{}, false
	}
	for _, p := range identity.Admitted(configDir) {
		if p.Addr == "" {
			continue
		}
		if strings.EqualFold(p.Label, host) {
			return p, true
		}
		if h, _, err := net.SplitHostPort(p.Addr); err == nil && strings.EqualFold(h, host) {
			return p, true
		}
	}
	return identity.Peer{}, false
}

// fleetOpts is the small family of commands that are about identity rather than about work.
type fleetOpts struct {
	whoami        bool
	admit, refuse string
	as, at        string
	listen        string
	provision     string
	join          string
	token, pin    string
	configDir     string
	out, errOut   io.Writer
}

// runFleetCmd answers the identity commands and returns a process exit code.
func runFleetCmd(o fleetOpts) int {
	host, _ := os.Hostname()
	switch {
	case o.whoami:
		id, err := identity.Load(o.configDir, host)
		if err != nil {
			fmt.Fprintln(o.errOut, "magi:", err)
			return 1
		}
		// Whole, on its own line, with the word another machine needs to type. This is meant to be
		// read out loud or pasted into a message, and then compared by the person on the far side.
		fmt.Fprintf(o.out, "%s\n", id.Fingerprint())
		fmt.Fprintf(o.out, "\nthis is %s. On the machine that should reach it:\n  magi --admit %s --as %s\n",
			host, id.Fingerprint(), orWordCLI(host, "thismachine"))
		return 0
	case o.admit != "":
		already, err := identity.Admit(o.configDir, identity.Peer{
			Fingerprint: o.admit, Label: o.as, Addr: o.at})
		if err != nil {
			fmt.Fprintln(o.errOut, "magi:", err)
			return 1
		}
		if already {
			fmt.Fprintln(o.out, "already admitted")
			return 0
		}
		fmt.Fprintf(o.out, "admitted %s as %s\n", o.admit, orWordCLI(o.as, "unnamed"))
		return 0
	case o.provision != "":
		return fleetProvision(o.configDir, host, o.provision, o.at, o.out)
	case o.join != "":
		return fleetJoin(o.configDir, host, o.join, o.token, o.pin, o.out)
	case o.refuse != "":
		was, err := identity.Refuse(o.configDir, o.refuse)
		if err != nil {
			fmt.Fprintln(o.errOut, "magi:", err)
			return 1
		}
		if !was {
			fmt.Fprintln(o.out, "that fingerprint was not admitted here")
			return 0
		}
		fmt.Fprintln(o.out, "refused; it cannot reach this machine from the next connection")
		return 0
	default:
		return fleetServe(context.Background(), o.listen, o.configDir, host, o.errOut)
	}
}

func orWordCLI(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return strings.TrimSpace(s)
}

// fleetJoin spends an invitation: this machine's key is recorded over there, and that machine's
// fingerprint is recorded here, in one exchange.
//
// The pin is what makes it safe to do over a network nobody vouches for. An invitation is a bearer
// secret, so handing it to the wrong server would hand over the secret AND admit that server —
// pinning the fingerprint the inviter printed means the secret only ever reaches the machine it
// was minted on.
func fleetJoin(configDir, host, addr, token, pin string, out io.Writer) int {
	if addr == "" || token == "" || pin == "" {
		fmt.Fprintln(out, "magi: a join needs the address, the invitation and the fingerprint to "+
			"pin — the line `magi --provision` printed has all three")
		return 2
	}
	id, err := identity.Load(configDir, host)
	if err != nil {
		fmt.Fprintln(out, "magi:", err)
		return 1
	}
	// Pinned before anything is sent. The peer is not on the admitted list yet — this is the one
	// crossing where the fingerprint comes from the command line instead — so the check is written
	// here rather than borrowed from the list.
	client := &http.Client{
		Timeout: fleetCrossTimeout,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			Certificates:       []tls.Certificate{id.Cert},
			MinVersion:         tls.VersionTLS13,
			InsecureSkipVerify: true, //nolint:gosec // pinned to --pin below
			VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
				if len(raw) == 0 {
					return fmt.Errorf("that machine presented no certificate")
				}
				leaf, perr := x509.ParseCertificate(raw[0])
				if perr != nil {
					return perr
				}
				if got := identity.FingerprintOf(leaf); got != pin {
					return fmt.Errorf("that machine is %s and the invitation named %s — "+
						"the address may be somebody else's", got, pin)
				}
				return nil
			},
		}},
	}
	body, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		fmt.Fprintln(out, "magi:", err)
		return 1
	}
	resp, err := client.Post("https://"+addr+fleetJoinPath, "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintln(out, "magi: could not join:", err)
		return 1
	}
	defer resp.Body.Close()
	var got daemon.Response
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if json.Unmarshal(answer, &got) != nil || !got.OK {
		fmt.Fprintln(out, "magi: that machine did not take the invitation:", strings.TrimSpace(orWordCLI(got.Err, string(answer))))
		return 1
	}
	if got.Out != pin {
		// It answered with a different fingerprint than the one this connection was pinned to,
		// which cannot happen over a verified connection — said rather than trusted.
		fmt.Fprintln(out, "magi: that machine named a different key than it presented")
		return 1
	}
	label := labelOf(addr)
	if _, err := identity.Admit(configDir, identity.Peer{
		Fingerprint: pin, Label: label, Addr: addr}); err != nil {
		fmt.Fprintln(out, "magi:", err)
		return 1
	}
	fmt.Fprintf(out, "joined %s (%s)\nboth machines now hold each other's key; `magi --refuse` on "+
		"either side ends it\n", label, pin)
	return 0
}

// fleetProvision mints an invitation and prints the one line the other machine runs.
//
// One line, because the four steps it replaces — read a fingerprint here, carry it, admit it
// there, write down an address — are four chances to stop. The fingerprint is still in the line,
// so what the other side pins is still something this machine printed.
func fleetProvision(configDir, host, label, at string, out io.Writer) int {
	id, err := identity.Load(configDir, host)
	if err != nil {
		fmt.Fprintln(out, "magi:", err)
		return 1
	}
	token, err := identity.Mint(configDir, label)
	if err != nil {
		fmt.Fprintln(out, "magi:", err)
		return 1
	}
	where := at
	if where == "" {
		where = orWordCLI(host, "this-machine") + ":7777"
	}
	fmt.Fprintf(out, "on %s, run:\n\n  magi --join %s --token %s --pin %s\n\n",
		orWordCLI(label, "the other machine"), where, token, id.Fingerprint())
	fmt.Fprintf(out, "the invitation stands for %s and is spent by the first machine that uses it.\n"+
		"this machine must be answering: magi --fleet-listen %s\n", identity.TokenLife(), where)
	return 0
}

// labelOf is the host part of an address, which is the name a joined machine gets until somebody
// edits the line.
func labelOf(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil && h != "" {
		return h
	}
	return addr
}
