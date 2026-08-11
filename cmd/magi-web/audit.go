package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/core/text"
)

// What was asked of this console, and by whom it can tell.
//
// # Why there was nothing
//
// One person, one machine, one browser: "who interrupted that turn" had one answer and nobody had
// to look it up. A second console makes the question real — and it is already real without any
// authentication being added, because a phone on the same tunnel is a second console.
//
// # What is recorded, and what deliberately is not
//
// Every request that CHANGES something: the method, the path, which companion it named, where it
// came from, and what this console answered. Not the body. A prompt is already in the session log
// verbatim, with a timestamp and an actor, and copying it here would make a second copy of
// everything anybody typed, in a file with different permissions and no compaction — the log is
// the record of the work, and this is the record of the door.
//
// Refusals are recorded with the rest, which is most of the value: a cross-site POST that the
// same-site guard turned down is the one line in this file somebody would actually want, and it
// never reaches a handler. So the wrapping is audit OUTSIDE the guard.
//
// # Who
//
// This process cannot tell. It has no users and no login (console.go says so), and a request
// forwarded by a reverse proxy arrives as a loopback connection like every other. So identity
// comes from the gateway in front, through a header the operator NAMES with -user-header: their
// SSO proxy knows who it let in, and this writes down what it said. Unset, the record says where a
// request came from and not who sent it, which is the truth rather than a blank to be filled in
// later by somebody reading it as a name.
//
// It is not verified and cannot be. The header is trustworthy exactly as far as the thing in front
// is the only way in — which is the same assumption the whole arrangement rests on, and why the
// bind guard stays.
type auditLog struct {
	mu sync.Mutex
	f  *os.File
	// once keeps a broken disk from writing a line per request to a terminal nobody is watching.
	once sync.Once
}

// auditName is beside the session store rather than in the config directory: it is a record of
// what happened, not a setting, and the store's directory is the one that already holds those.
const auditName = "console-audit.jsonl"

// newAudit opens the record, creating it. 0600 because the lines name workspaces and, when a
// gateway supplies one, people.
func newAudit(dir string) (*auditLog, error) {
	f, err := os.OpenFile(filepath.Join(dir, auditName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &auditLog{f: f}, nil
}

func (a *auditLog) Close() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.f.Close()
}

// entry is one line. Everything is omitted when empty, so a single-operator console's record stays
// readable rather than a column of nulls.
type entry struct {
	At     string `json:"at"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Status int    `json:"status"`
	// Agent is the companion the request named — the socket, which is what the page sends and what
	// resolves to a workspace. Absent means "the daemon in this console's own directory".
	Agent string `json:"agent,omitempty"`
	Who   string `json:"who,omitempty"`
	From  string `json:"from,omitempty"`
	// Via is what a proxy said the original client was, when there is one. Kept apart from From,
	// which is the connection this process actually accepted: the two disagreeing is worth seeing,
	// and merging them would make a forwarded header look like an observation.
	Via string `json:"via,omitempty"`
}

// note appends one line. A nil log records nothing, which is what a test's bare server does.
func (a *auditLog) note(e entry) {
	if a == nil {
		return
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.f.Write(append(b, '\n')); err != nil {
		a.once.Do(func() {
			fmt.Fprintln(os.Stderr, "magi-web: the audit record is not being written:", err)
		})
	}
}

// audited wraps a route so what it did is written down.
//
// Reads are not recorded. They are the page polling — a fleet refresh every three seconds, an
// event stream held open — and a record that is nine parts noise is one nobody reads. What changes
// something is what a person answers for.
func (s *server) audited(h http.HandlerFunc) http.HandlerFunc {
	if s.audit == nil {
		return h
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			h(w, r)
			return
		}
		sw := &statusWriter{ResponseWriter: w}
		h(sw, r)
		s.audit.note(entry{
			At:     time.Now().Format(time.RFC3339),
			Method: r.Method,
			Path:   r.URL.Path,
			Status: sw.code(),
			Agent:  text.Clip(r.URL.Query().Get("d"), 200),
			Who:    s.whoFrom(r),
			From:   hostOf(r.RemoteAddr),
			Via:    text.Clip(r.Header.Get("X-Forwarded-For"), 100),
		})
	}
}

// whoFrom reads the identity the gateway in front claims, or nothing.
func (s *server) whoFrom(r *http.Request) string {
	if s.userHeader == "" {
		return ""
	}
	return text.Clip(r.Header.Get(s.userHeader), 100)
}

// hostOf drops the ephemeral port, which is noise: from a proxy on the same machine every line
// would carry a different one and none of them would mean anything.
func hostOf(remote string) string {
	if h, _, err := net.SplitHostPort(remote); err == nil {
		return h
	}
	return remote
}

// statusWriter remembers what was answered.
//
// Nothing else in this server needs the status after the fact, which is why it did not exist: a
// handler writes and returns. The record needs it, because "refused" and "done" are the same line
// otherwise — and a refusal is the line worth having.
type statusWriter struct {
	http.ResponseWriter
	wrote int
}

func (s *statusWriter) WriteHeader(c int) {
	if s.wrote == 0 {
		s.wrote = c
	}
	s.ResponseWriter.WriteHeader(c)
}

// Write is the implicit 200 a handler takes by writing a body without a header.
func (s *statusWriter) Write(b []byte) (int, error) {
	if s.wrote == 0 {
		s.wrote = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// code is what was answered; a handler that wrote nothing at all answered 200, which is what net/http
// sends for it.
func (s *statusWriter) code() int {
	if s.wrote == 0 {
		return http.StatusOK
	}
	return s.wrote
}

// Flush keeps a streaming handler streaming. Only reads stream today and reads are not wrapped,
// but a wrapper that silently disables Flusher is the kind of thing that is found months later in
// a transcript that stopped updating.
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
