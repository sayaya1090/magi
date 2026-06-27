// Package jsonl implements port.Store as an append-only JSONL log per session,
// grouped by working directory (D6, a reference agent style):
//
//	<root>/projects/<encoded-workdir>/<sessionID>.jsonl
//
// One line = one fact event. Transient (bus-only) events are rejected. The
// store assigns a per-session monotonically increasing seq starting at 1.
package jsonl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Store is a filesystem-backed, event-sourced store. Safe for concurrent use.
type Store struct {
	root string

	mu    sync.Mutex
	seqs  map[session.SessionID]int64  // last assigned seq per session
	paths map[session.SessionID]string // sessionID -> file path
}

// New opens (or initializes) a Store rooted at root, indexing any existing logs
// so seq numbering and lookups survive process restarts.
func New(root string) (*Store, error) {
	s := &Store{
		root:  root,
		seqs:  make(map[session.SessionID]int64),
		paths: make(map[session.SessionID]string),
	}
	if err := s.index(); err != nil {
		return nil, err
	}
	return s, nil
}

// projectsDir is the parent of all per-workdir directories.
func (s *Store) projectsDir() string { return filepath.Join(s.root, "projects") }

// index scans existing logs to rebuild seq counters and path lookups.
func (s *Store) index() error {
	base := s.projectsDir()
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, dir := range entries {
		if !dir.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(base, dir.Name()))
		if err != nil {
			return err
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			sid := session.SessionID(strings.TrimSuffix(f.Name(), ".jsonl"))
			path := filepath.Join(base, dir.Name(), f.Name())
			evs, err := readFile(path, 0)
			if err != nil {
				return err
			}
			s.paths[sid] = path
			if n := len(evs); n > 0 {
				s.seqs[sid] = evs[n-1].Seq
			}
		}
	}
	return nil
}

// Append assigns seq numbers, writes the events as JSONL, and returns the
// assigned seqs in order. All events must be fact (persistable) types.
func (s *Store) Append(ctx context.Context, sid session.SessionID, evs ...event.Event) ([]int64, error) {
	if len(evs) == 0 {
		return nil, nil
	}
	for _, e := range evs {
		if e.Type.IsTransient() {
			return nil, fmt.Errorf("jsonl: cannot persist transient event %q", e.Type)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.pathFor(sid, evs)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	seq := s.seqs[sid]
	out := make([]int64, len(evs))
	for i := range evs {
		seq++
		evs[i].Seq = seq
		evs[i].SessionID = sid
		line, err := json.Marshal(evs[i])
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(line); err != nil {
			return nil, err
		}
		if err := w.WriteByte('\n'); err != nil {
			return nil, err
		}
		out[i] = seq
	}
	if err := w.Flush(); err != nil {
		return nil, err
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	s.seqs[sid] = seq
	s.paths[sid] = path
	return out, nil
}

// pathFor resolves the log file path for a session, deriving the workdir from a
// session.created event when the session is new.
func (s *Store) pathFor(sid session.SessionID, evs []event.Event) (string, error) {
	if p, ok := s.paths[sid]; ok {
		return p, nil
	}
	for _, e := range evs {
		if e.Type == event.TypeSessionCreated {
			var d event.SessionCreatedData
			if err := json.Unmarshal(e.Data, &d); err != nil {
				return "", fmt.Errorf("jsonl: bad session.created data: %w", err)
			}
			return s.sessionPath(d.Workdir, sid), nil
		}
	}
	return "", fmt.Errorf("jsonl: unknown session %q (first append must include session.created)", sid)
}

func (s *Store) sessionPath(workdir string, sid session.SessionID) string {
	return filepath.Join(s.projectsDir(), encodeWorkdir(workdir), string(sid)+".jsonl")
}

// Read returns events with Seq > fromSeq, in ascending seq order.
func (s *Store) Read(ctx context.Context, sid session.SessionID, fromSeq int64) ([]event.Event, error) {
	s.mu.Lock()
	path, ok := s.paths[sid]
	s.mu.Unlock()
	if !ok {
		return nil, nil
	}
	return readFile(path, fromSeq)
}

func readFile(path string, fromSeq int64) ([]event.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var evs []event.Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e event.Event
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("jsonl: corrupt line in %s: %w", path, err)
		}
		if e.Seq > fromSeq {
			evs = append(evs, e)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return evs, nil
}

// Compact rewrites the log so that all events with seq <= upToSeq are replaced
// by a single snapshot event, preserving events with seq > upToSeq. The original
// is kept alongside as "<file>.archive".
func (s *Store) Compact(ctx context.Context, sid session.SessionID, upToSeq int64, snapshot event.Event) error {
	if snapshot.Type.IsTransient() {
		return fmt.Errorf("jsonl: snapshot cannot be a transient event")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	path, ok := s.paths[sid]
	if !ok {
		return fmt.Errorf("jsonl: unknown session %q", sid)
	}
	existing, err := readFile(path, 0)
	if err != nil {
		return err
	}

	snapshot.SessionID = sid
	if snapshot.Seq == 0 {
		snapshot.Seq = upToSeq
	}
	kept := []event.Event{snapshot}
	for _, e := range existing {
		if e.Seq > upToSeq {
			kept = append(kept, e)
		}
	}

	tmp := path + ".tmp"
	if err := writeAll(tmp, kept); err != nil {
		return err
	}
	if err := os.Rename(path, path+".archive"); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return nil
}

func writeAll(path string, evs []event.Event) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, e := range evs {
		line, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if _, err := w.Write(line); err != nil {
			return err
		}
		if err := w.WriteByte('\n'); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	return f.Sync()
}

// Truncate rewrites the log keeping only events with seq <= upToSeq (rewind),
// archiving the original to "<file>.rewind". The in-memory seq counter is reset
// so subsequent appends continue from upToSeq.
func (s *Store) Truncate(ctx context.Context, sid session.SessionID, upToSeq int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, ok := s.paths[sid]
	if !ok {
		return fmt.Errorf("jsonl: unknown session %q", sid)
	}
	existing, err := readFile(path, 0)
	if err != nil {
		return err
	}
	kept := make([]event.Event, 0, len(existing))
	var last int64
	for _, e := range existing {
		if e.Seq <= upToSeq {
			kept = append(kept, e)
			if e.Seq > last {
				last = e.Seq
			}
		}
	}
	tmp := path + ".tmp"
	if err := writeAll(tmp, kept); err != nil {
		return err
	}
	if err := os.Rename(path, path+".rewind"); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	s.seqs[sid] = last
	return nil
}

// ListSessions returns metadata for top-level (non-subagent) sessions under
// workdir, newest first.
func (s *Store) ListSessions(ctx context.Context, workdir string) ([]session.SessionMeta, error) {
	all, err := s.scanSessions(workdir)
	if err != nil {
		return nil, err
	}
	metas := all[:0]
	for _, m := range all {
		if m.Parent == "" { // top-level only; subagents are listed via ChildSessions
			metas = append(metas, m)
		}
	}
	return metas, nil
}

// ChildSessions returns the subagent (child) sessions spawned by parentID, newest
// first — used to restore a parent's subagent panes on resume.
func (s *Store) ChildSessions(ctx context.Context, workdir, parentID string) ([]session.SessionMeta, error) {
	all, err := s.scanSessions(workdir)
	if err != nil {
		return nil, err
	}
	var out []session.SessionMeta
	for _, m := range all {
		if m.Parent == parentID {
			out = append(out, m)
		}
	}
	return out, nil
}

// scanSessions reads metadata for every session file under workdir, newest first.
func (s *Store) scanSessions(workdir string) ([]session.SessionMeta, error) {
	dir := filepath.Join(s.projectsDir(), encodeWorkdir(workdir))
	files, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var metas []session.SessionMeta
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
			continue
		}
		sid := session.SessionID(strings.TrimSuffix(f.Name(), ".jsonl"))
		evs, err := readFile(filepath.Join(dir, f.Name()), 0)
		if err != nil {
			return nil, err
		}
		if len(evs) == 0 {
			continue
		}
		m := session.SessionMeta{ID: sid, Workdir: workdir}
		m.Created = evs[0].TS
		m.LastActivity = evs[len(evs)-1].TS
		m.Title = firstPromptSummary(evs)
		if evs[0].Type == event.TypeSessionCreated {
			var d event.SessionCreatedData
			if json.Unmarshal(evs[0].Data, &d) == nil {
				m.Agent = d.Agent
				m.Parent = d.Parent
			}
		}
		metas = append(metas, m)
	}
	sort.Slice(metas, func(i, j int) bool {
		if metas[i].Created.Equal(metas[j].Created) {
			return metas[i].ID > metas[j].ID
		}
		return metas[i].Created.After(metas[j].Created)
	})
	return metas, nil
}

// firstPromptSummary extracts a one-line summary from the first user prompt in a
// session, for display in the resume picker. Empty if none found.
func firstPromptSummary(evs []event.Event) string {
	for _, e := range evs {
		if e.Type != event.TypePromptSubmitted {
			continue
		}
		var d event.PromptSubmittedData
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		for _, p := range d.Parts {
			if p.Kind != session.PartText {
				continue
			}
			s := strings.TrimSpace(p.Text)
			if s == "" {
				continue
			}
			// Collapse to a single line.
			s = strings.Join(strings.Fields(s), " ")
			r := []rune(s)
			if len(r) > 80 {
				s = string(r[:80]) + "…"
			}
			return s
		}
	}
	return ""
}

// encodeWorkdir maps an absolute workdir to a deterministic, filesystem-safe
// directory name (e.g. /Users/x/proj -> -Users-x-proj).
func encodeWorkdir(workdir string) string {
	var b strings.Builder
	for _, r := range workdir {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	enc := b.String()
	if enc == "" {
		enc = "-"
	}
	return enc
}
