package update

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// StableWindow is how long a freshly-installed build has to keep running before the update that put
// it there is confirmed and the build it replaced is thrown away.
//
// Verify (`--version`) answers a narrower question than it looks like it does: the file can be
// executed and prints a version. A build that starts, answers, and then cannot serve — a bad config
// migration, a listener that panics, a dependency that is missing only on the serving path — passes
// that check and was, until this journal existed, committed by DELETING the only copy of the build
// that worked. This window is the second question: did it stay up.
//
// Sixty seconds, from CLIENT_LIFECYCLE §5 and §9.3 — the same number the clients use to decide a
// launch was not a crash. One number for "this came up and stayed up", not two that drift.
var StableWindow = 60 * time.Second

// Versions names the two builds in an update. A struct rather than two strings in the parameter list
// because `Commit(bin, path, from, to)` reads identically to `Commit(bin, path, to, from)`, and the
// only symptom of getting it backwards would be a rollback message naming the wrong direction.
type Versions struct{ From, To string }

// ledger is the per-install update record, kept beside the binary it describes.
//
// One file, two facts, because CLIENT_LIFECYCLE §9.3 asks that the candidate, the previous build and
// the transaction stage live in the same install unit: a pending transaction nobody has confirmed,
// and the version this install has already thrown out. Split across two files they could disagree
// after a crash between the writes.
type ledger struct {
	Pending *pendingUpdate `json:"pending,omitempty"`
	// Refused is a version this install installed, ran, and could not keep. The auto path will not
	// take it again; an explicit user retry (Retry) clears it, which is §9.3's "a new candidate or
	// the user's explicit retry" — a newer version passes on its own, since this matches exactly.
	Refused string `json:"refused,omitempty"`
}

type pendingUpdate struct {
	// To is the version now on disk; From is the one kept beside it at <target>.prev.
	To   string `json:"to"`
	From string `json:"from"`
	// Starts counts generations that began on this candidate without confirming it. One is the
	// generation we expect — the restart the update triggered. Two means the first one did not
	// survive its window, which is the failure this whole record exists to catch.
	Starts int    `json:"starts"`
	Since  string `json:"since"`
}

// journalOf is the ledger path for an install. Beside the binary, not in the config directory: two
// magi installs on one machine are two transactions, and a config directory is shared by both.
func journalOf(target string) string { return target + ".update.json" }

func readLedger(target string) (ledger, error) {
	var l ledger
	b, err := os.ReadFile(journalOf(target))
	if err != nil {
		if os.IsNotExist(err) {
			return ledger{}, nil // no transaction here is the ordinary case, not a failure
		}
		return ledger{}, err
	}
	if err := json.Unmarshal(b, &l); err != nil {
		// A torn or hand-edited record is not a reason to refuse to start, and it is not evidence
		// of a pending update either. Treat it as no transaction and let the next Commit rewrite it.
		return ledger{}, nil
	}
	return l, nil
}

// writeLedger replaces the record atomically, for the same reason the binaries are written that way:
// this file decides whether a rollback is possible, and a half-written one read after a power cut
// would be read as "no transaction" — the one answer that throws the previous build away.
func writeLedger(target string, l ledger) error {
	path := journalOf(target)
	if l.Pending == nil && l.Refused == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".magi-update-journal-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Began records that a candidate has been installed over target and the build it replaced is beside
// it. Commit calls this in the same breath as the replacement — a separate step would leave a window
// where the new binary is in place and nothing says the old one is recoverable.
func Began(target string, v Versions) error {
	l, err := readLedger(target)
	if err != nil {
		return err
	}
	l.Pending = &pendingUpdate{To: v.To, From: v.From, Since: time.Now().UTC().Format(time.RFC3339)}
	return writeLedger(target, l)
}

// Refused reports the version this install ran and could not keep, or "" if there is none.
func Refused(target string) string {
	abs := resolveInstall(target)
	l, err := readLedger(abs)
	if err != nil {
		return ""
	}
	return l.Refused
}

// Retry forgets a refusal, so the build that was rolled back here can be tried again. Only a person
// asking for it calls this: §9.3 blocks the automatic path until a new candidate or an explicit retry.
func Retry(target string) error {
	abs := resolveInstall(target)
	l, err := readLedger(abs)
	if err != nil {
		return err
	}
	if l.Refused == "" {
		return nil
	}
	l.Refused = ""
	return writeLedger(abs, l)
}

// Recovery is what Resume found.
type Recovery struct {
	// Watching says this process IS the unconfirmed candidate and is the first generation to run it.
	// The caller keeps serving and calls Confirm once StableWindow has passed.
	Watching bool
	// RolledBack says the candidate did not survive a previous start, the build at target is now the
	// one it replaced, and the candidate is recorded as refused. The RUNNING IMAGE is still the bad
	// build — the caller has to restart onto the restored file.
	RolledBack bool
	From, To   string
}

// Resume reads the install's transaction record at startup and moves it one step.
//
// Three cases, and the middle one is the point of the whole file:
//
//   - nothing pending, or the running image is not the candidate — leave it alone. A person starting
//     an older binary by hand is not evidence about somebody else's update.
//   - pending, this is the first generation on the candidate — note the start and let the caller
//     confirm after the window. A crash before Confirm leaves Starts at one, which is what the next
//     start reads.
//   - pending and a generation already started without confirming — the candidate came up and did not
//     stay up. Put the previous build back and record the refusal so the auto path stops taking it.
//
// ⚠ It only sees failures that get this far. A build that dies before Resume runs never increments
// anything, and Verify's `--version` pre-flight is what catches those — this is the layer above it,
// not a replacement for it.
func Resume(target, running string) (Recovery, error) {
	abs := resolveInstall(target)
	l, err := readLedger(abs)
	if err != nil || l.Pending == nil {
		return Recovery{}, err
	}
	p := l.Pending
	if p.To != running {
		return Recovery{}, nil
	}
	if p.Starts >= 1 {
		if err := restorePrevious(abs); err != nil {
			return Recovery{From: p.From, To: p.To}, err
		}
		l.Pending, l.Refused = nil, p.To
		if err := writeLedger(abs, l); err != nil {
			return Recovery{From: p.From, To: p.To}, err
		}
		return Recovery{RolledBack: true, From: p.From, To: p.To}, nil
	}
	p.Starts++
	if err := writeLedger(abs, l); err != nil {
		return Recovery{}, err
	}
	return Recovery{Watching: true, From: p.From, To: p.To}, nil
}

// Confirm closes the transaction: the candidate stayed up, so the build it replaced is dropped and
// the record goes away. Idempotent — a second call with nothing pending is not an error.
func Confirm(target string) error {
	abs := resolveInstall(target)
	l, err := readLedger(abs)
	if err != nil || l.Pending == nil {
		return err
	}
	l.Pending = nil
	if err := writeLedger(abs, l); err != nil {
		return err
	}
	if err := os.Remove(abs + ".prev"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// LeftCleanly says this generation is shutting down on purpose before its window elapsed, so the next
// start should not read it as a build that fell over. Without it, stopping a daemon a few seconds
// after an update — an ordinary thing to do — would roll that update back on the next start.
func LeftCleanly(target string) error {
	abs := resolveInstall(target)
	l, err := readLedger(abs)
	if err != nil || l.Pending == nil || l.Pending.Starts == 0 {
		return err
	}
	l.Pending.Starts--
	return writeLedger(abs, l)
}

// restorePrevious swaps <target>.prev back over target and removes it.
func restorePrevious(target string) error {
	prev := target + ".prev"
	b, err := os.ReadFile(prev)
	if err != nil {
		return fmt.Errorf("no saved build to roll back to at %s: %w", prev, err)
	}
	if err := writeBinary(target, b); err != nil {
		return err
	}
	return os.Remove(prev)
}

// resolveInstall is the one spelling of "which install is this" — absolute, symlinks followed, so a
// bin/magi → /opt/magi/magi layout keeps one record beside the real file instead of one per link.
func resolveInstall(target string) string {
	abs, err := filepath.Abs(target)
	if err != nil {
		return target
	}
	if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil {
		return resolved
	}
	return abs
}
