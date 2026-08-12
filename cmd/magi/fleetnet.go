package main

import (
	"bytes"
	"context"
	"crypto/tls"
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
// door.go is the rule — three methods, and only companions this account published. This file is
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
		fleetHandle(w, r, configDir)
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
			VerifyPeerCertificate: identity.VerifyAdmitted(configDir, nil),
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
// Request and reply, not a stream, which is what the three methods are: ask what a companion is,
// hand it work, ask how that work went. The one method that streams is the one the door does not
// carry, so nothing here needs to hold a connection open.
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

// answerFleet writes one refusal or reply.
//
// A failure here is the caller having gone, which nothing can be done about — so it is logged, the
// way the console logs an answer it could not finish writing, rather than returned to a handler
// that has no move left either.
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
// reply; that is exactly one POST, and the protocol is strictly alternating for every method this
// door carries. A transport that pretended to be a stream and buffered would be more code and less
// honest about what it does.
type fleetDial struct {
	client *http.Client
	url    string
	socket string

	mu   sync.Mutex
	next bytes.Buffer // the reply waiting to be read
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
	resp, err := f.client.Post(f.url, "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("fleet: %s answered %s", f.url, resp.Status)
	}
	f.next.Write(answer)
	if !bytes.HasSuffix(answer, []byte("\n")) {
		f.next.WriteByte('\n')
	}
	return len(p), nil
}

func (f *fleetDial) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.next.Len() == 0 {
		return 0, io.EOF
	}
	return f.next.Read(p)
}

func (f *fleetDial) Close() error {
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
	return &fleetDial{
		client: &http.Client{
			Timeout:   fleetCrossTimeout,
			Transport: &http.Transport{TLSClientConfig: tlsCfg, ForceAttemptHTTP2: true},
		},
		url:    "https://" + peer.Addr + fleetPath,
		socket: socket,
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
