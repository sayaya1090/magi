// Package jsonl implements port.Store as an append-only JSONL log per session,
// grouped by working directory (D6):
//
//	<root>/projects/<encoded-workdir>/<sessionID>.jsonl
//
// One line = one fact event. Transient (bus-only) events are rejected. The
// store assigns a per-session monotonically increasing seq starting at 1.
package jsonl

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	seqs  map[session.SessionID]int64         // last assigned seq per session
	paths map[session.SessionID]string        // sessionID -> file path
	cache map[session.SessionID][]event.Event // read-through cache: parsed log per session
	// read is how many BYTES of each log the cache reflects. The cache used to assume this
	// process was the only writer, which stopped being true the day magi could run as a daemon
	// with a separate viewer: the viewer parsed the log once and then served that snapshot
	// forever, so a browser watching a live run showed a transcript frozen at the moment it
	// connected. Observed exactly that way. Comparing this against the file size on each Read
	// turns "the only writer" into "the only writer I know of".
	read map[session.SessionID]int64
}

// New opens (or initializes) a Store rooted at root, indexing any existing logs
// so seq numbering and lookups survive process restarts.
func New(root string) (*Store, error) {
	s := &Store{
		root:  root,
		seqs:  make(map[session.SessionID]int64),
		paths: make(map[session.SessionID]string),
		cache: make(map[session.SessionID][]event.Event),
		read:  make(map[session.SessionID]int64),
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
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	// Repair a torn final line: if the existing file does NOT end in a newline (a prior
	// write was interrupted/partial), a bare append would glue the new event onto the
	// broken line — and json.Unmarshal would then skip BOTH as one unparseable line,
	// silently losing this event. Write a separating newline first so the new event
	// lands on its own line (the torn line stays corrupt but is tolerated on read).
	if fi, statErr := f.Stat(); statErr == nil && fi.Size() > 0 {
		last := make([]byte, 1)
		if _, rErr := f.ReadAt(last, fi.Size()-1); rErr == nil && last[0] != '\n' {
			if err := w.WriteByte('\n'); err != nil {
				return nil, err
			}
		}
	}
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
		// Bytes may already be on disk but our in-memory state isn't updated — drop any
		// warm cache so the next Read re-derives truth from the file instead of serving a
		// stale view that silently omits the just-flushed events.
		delete(s.cache, sid)
		delete(s.read, sid)
		return nil, err
	}
	s.seqs[sid] = seq
	s.paths[sid] = path
	// Keep the read-through cache current so the next Read is a memory hit, not a re-parse.
	// Only when already loaded — an unread session stays lazy and loads on first Read
	// (the file, just written, is the source of truth either way).
	if c, ok := s.cache[sid]; ok {
		s.cache[sid] = append(c, evs...)
		// The cache now reflects the bytes just written, so the next Read must not treat them as
		// somebody else's tail and parse them a second time.
		if size, err := fileSize(path); err == nil {
			s.read[sid] = size
		} else {
			delete(s.cache, sid) // cannot say what the cache reflects — make the next Read reload
			delete(s.read, sid)
		}
	}
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

// Read returns events with Seq > fromSeq, in ascending seq order. Served from an
// in-memory cache (the parsed log) so the agent loop's per-step Read is a memory filter
// instead of a full-file JSON re-parse; the file is loaded once, lazily, on first access.
func (s *Store) Read(ctx context.Context, sid session.SessionID, fromSeq int64) ([]event.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, known := s.paths[sid]
	if !known {
		// A session this process has never heard of is not necessarily a session that does not
		// exist: the index is built at New, and another process may have created the log since.
		// A viewer started before the daemon it is watching hits exactly this.
		var found bool
		if path, found = s.locate(sid); !found {
			return nil, nil
		}
		s.paths[sid] = path
	}
	evs, warm := s.cache[sid]

	// Stat BEFORE reading, never after: a file that grows between the read and the stat would
	// record an offset past events that were never parsed, and those events would be skipped for
	// good. Recording a size that is too SMALL only costs re-reading a few lines, which the seq
	// filter below then discards.
	size, statErr := fileSize(path)
	switch {
	case !warm || statErr != nil || size < s.read[sid]:
		// Cold, unreadable, or shorter than what the cache holds — the last of those is a compact
		// or a rewind by another process, where the tail is not a tail of what we have.
		loaded, err := readFile(path, 0)
		if err != nil {
			return nil, err
		}
		s.cache[sid], s.read[sid] = loaded, size
		evs = loaded
	case size > s.read[sid]:
		tail, off, err := readFrom(path, s.read[sid])
		if err != nil {
			return nil, err
		}
		var last int64
		if len(evs) > 0 {
			last = evs[len(evs)-1].Seq
		}
		for _, e := range tail {
			// Only what is genuinely new. The offset is conservative by design (above), so a
			// re-read of already-cached lines is expected rather than exceptional.
			if e.Seq > last {
				evs = append(evs, e)
			}
		}
		s.cache[sid], s.read[sid] = evs, off
	}
	return filterFrom(evs, fromSeq), nil
}

// locate finds a session's log on disk when the in-memory index has never seen it.
func (s *Store) locate(sid session.SessionID) (string, bool) {
	matches, err := filepath.Glob(filepath.Join(s.projectsDir(), "*", string(sid)+".jsonl"))
	if err != nil || len(matches) == 0 {
		return "", false
	}
	return matches[0], true
}

// fileSize is the log's length in bytes, or 0 if it cannot be stat'd.
func fileSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// readFrom parses the complete lines starting at byte offset off, and returns the offset just past
// the last one.
//
// Complete lines only. A log is appended to by another process, and a read that lands mid-write
// sees a line with no newline yet — parsing that would either drop the event (the bytes are past
// the offset next time and never re-read) or, worse, accept a truncated JSON object. Stopping at
// the last newline leaves the partial line for the next call, when the rest of it has arrived.
func readFrom(path string, off int64) ([]event.Event, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, off, nil
		}
		return nil, 0, err
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, 0, err
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, 0, err
	}
	end := bytes.LastIndexByte(b, '\n')
	if end < 0 {
		return nil, off, nil // nothing complete yet
	}
	var evs []event.Event
	for _, line := range bytes.Split(b[:end], []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var e event.Event
		// Same tolerance readFile has: a corrupt line is skipped, not fatal.
		if json.Unmarshal(line, &e) == nil {
			evs = append(evs, e)
		}
	}
	return evs, off + int64(end) + 1, nil
}

// filterFrom returns a fresh slice of the events with Seq > fromSeq (a copy, so a caller
// can never mutate the cache's backing array). Events are seq-ascending, so a binary
// search locates the cut. NOTE: the per-event Data (json.RawMessage) is shared with the
// cache and every other reader — callers MUST treat it as immutable.
func filterFrom(evs []event.Event, fromSeq int64) []event.Event {
	i := 0
	if fromSeq > 0 {
		i = sort.Search(len(evs), func(i int) bool { return evs[i].Seq > fromSeq })
	}
	out := make([]event.Event, len(evs)-i)
	copy(out, evs[i:])
	return out
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
	r := bufio.NewReader(f)
	for {
		line, skip, rErr := readLogLine(r)
		if !skip && len(line) > 0 {
			var e event.Event
			// Tolerate a corrupt or torn line — e.g. a partially-flushed final line read
			// concurrently with an in-flight Append, or on-disk corruption. Skipping it
			// keeps the store openable and resume/ListSessions working; failing the whole
			// read here would brick every session that shares the (one bad line) log.
			if json.Unmarshal(line, &e) == nil && e.Seq > fromSeq {
				evs = append(evs, e)
			}
		}
		if rErr != nil {
			if rErr == io.EOF {
				return evs, nil
			}
			return nil, rErr
		}
	}
}

// maxLogLineBytes bounds how large a single JSONL line we'll buffer while reading.
// Realistic events (a 10 MiB read escaped into JSON, a base64 image) stay well under
// it; a line past it is drained and skipped rather than buffered — so a degenerate or
// corrupt length can't OOM the process nor, via the old scanner's ErrTooLong, brick
// New()/resume for every session sharing the directory. A var (not const) only so
// tests can shrink it to exercise the skip path without writing 128 MiB.
var maxLogLineBytes = 128 << 20 // 128 MiB

// readLogLine reads one '\n'-terminated line, tolerating lines of any length: a line
// longer than maxLogLineBytes is drained to its newline and reported with skip=true
// (line nil) instead of being buffered or raising an error. The final line may return
// with err==io.EOF and no trailing newline.
func readLogLine(r *bufio.Reader) (line []byte, skip bool, err error) {
	for {
		chunk, e := r.ReadSlice('\n') // valid only until the next read; we copy below
		if !skip {
			if len(line)+len(chunk) > maxLogLineBytes {
				skip, line = true, nil // over budget: drop and keep draining to the newline
			} else {
				line = append(line, chunk...)
			}
		}
		if e == bufio.ErrBufferFull {
			continue // delimiter not yet reached; keep accumulating this line
		}
		if n := len(line); n > 0 && line[n-1] == '\n' {
			line = line[:n-1]
		}
		return line, skip, e
	}
}

// Compact rewrites the log so that all events with seq <= upToSeq are replaced
// by a single snapshot event, preserving events with seq > upToSeq. The original
// is kept alongside as "<file>.archive".
func (s *Store) Compact(ctx context.Context, sid session.SessionID, upToSeq int64, snapshot event.Event) error {
	if snapshot.Type.IsTransient() {
		return fmt.Errorf("jsonl: snapshot cannot be a transient event")
	}
	// Compacting "up to 0" covers no events, and would stamp the snapshot with Seq 0 —
	// which Read(fromSeq=0) filters out (seqs start at 1), making the snapshot invisible
	// on replay. There is nothing to compact, so no-op.
	if upToSeq < 1 {
		return nil
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
	if err := archiveThenReplace(path, tmp, path+".archive"); err != nil {
		return err
	}
	delete(s.cache, sid) // log rewritten — drop the stale cache; next Read reloads it
	delete(s.read, sid)
	return nil
}

// archiveThenReplace swaps a rewritten log into place without ever leaving the session's path
// missing.
//
// It used to be two renames: the original moved aside to the archive name, then the replacement
// moved in. Between them the session has no file at all, and that state is not loud — measured,
// a fresh Read of it returns 0 events with a nil error and ListSessions returns 0 sessions with a
// nil error. The session reads as one that never existed, with its whole history sitting one
// filename away. A process killed at that instant is not exotic here: compaction runs mid-turn,
// and mid-turn is when a wall clock or an operator ends a run.
//
// A hard link gives the archive its own name while the path still points at the same bytes, so
// the only move left is the atomic one that replaces it. Filesystems that cannot link fall back
// to the old sequence — the window returns, which is still better than refusing to compact.
func archiveThenReplace(path, tmp, archive string) error {
	_ = os.Remove(archive) // Link refuses an existing target; the archive is overwritten by design
	if err := os.Link(path, archive); err != nil {
		if rerr := os.Rename(path, archive); rerr != nil {
			return rerr
		}
	}
	return os.Rename(tmp, path)
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
	if err := archiveThenReplace(path, tmp, path+".rewind"); err != nil {
		return err
	}
	s.seqs[sid] = last
	delete(s.cache, sid) // log rewritten — drop the stale cache; next Read reloads it
	delete(s.read, sid)
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
		// The LAST set wins. Each event carries the whole set, so the newest is the answer and
		// there is nothing to fold — and this loop is already walking every event for the title.
		for i := len(evs) - 1; i >= 0; i-- {
			if evs[i].Type != event.TypeLabelsChanged {
				continue
			}
			var ld event.LabelsChangedData
			if json.Unmarshal(evs[i].Data, &ld) == nil {
				m.Labels = ld.Labels
			}
			break
		}
		if evs[0].Type == event.TypeSessionCreated {
			var d event.SessionCreatedData
			if json.Unmarshal(evs[0].Data, &d) == nil {
				m.Agent = d.Agent
				m.Parent = d.Parent
				m.Model = d.Model.Model
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
