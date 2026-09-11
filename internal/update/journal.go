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
	Starts int `json:"starts"`
	// Stage is what this record was in the middle of. Two values, and the second exists only so an
	// interruption is legible: `trial` is a build being watched, `confirming` is one that already
	// proved itself and is having its backup dropped. Confirm writes `confirming` BEFORE removing
	// anything, so a process killed in that half-second leaves a record that says which half it was
	// in — without it, "a `.prev` beside a pending record" means both "roll this back" and "finish
	// throwing this away", and those are opposite actions.
	Stage string `json:"stage,omitempty"`
	Since string `json:"since"`
}

const (
	stageTrial      = "trial"
	stageConfirming = "confirming"
)

// removePrev drops the backup. A package variable because the ORDER of Confirm's two destructive
// steps is the thing worth testing, and the only way to ask about an order is to stop between them.
// A test that sets the stage by hand measures the branch that reads it, not the code that writes it
// — measured: writing the record second instead of first survived such a test untouched.
var removePrev = os.Remove

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
// Began records that a candidate has been installed over target and the build it replaced is beside
// it, taking the install lock for the write — the same arbitration every other step of a transaction
// now uses (review R10).
func Began(target string, v Versions) error {
	abs := resolveInstall(target)
	release, got := takeInstall(abs)
	if !got {
		return errInstallHeld(abs)
	}
	defer release()
	return beganHeld(abs, v)
}

// ⚠ **Unexported and lock-free on purpose.** Commit calls this while it HOLDS the install lock, so
// taking it again here would be a process deadlocking against itself — both platforms' locks are
// per-handle, and a second handle from the same process conflicts exactly like another process's.
// Everything else that touches this ledger goes through the exported wrappers below, which lock.
func beganHeld(target string, v Versions) error {
	l, err := readLedger(target)
	if err != nil {
		return err
	}
	l.Pending = &pendingUpdate{To: v.To, From: v.From, Stage: stageTrial,
		Since: time.Now().UTC().Format(time.RFC3339)}
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
	release, got := takeInstall(abs)
	if !got {
		return errInstallHeld(abs)
	}
	defer release()
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
	// ⚠ **Held across the read AND the decision.** This reads a count, may restore a file over the
	// binary, and writes the count back — three steps a concurrent replacement can land between, and
	// the file it restores is the one that replacement is writing.
	release, got := takeInstall(abs)
	if !got {
		return Recovery{}, errInstallHeld(abs)
	}
	defer release()
	l, err := readLedger(abs)
	if err != nil || l.Pending == nil {
		return Recovery{}, err
	}
	p := l.Pending
	// A confirm that was interrupted. The build already lasted its window — that is what got it
	// here — so finishing means dropping the backup, never restoring it. This branch runs whatever
	// is running now, because the question is not "who am I" but "what was left half-done".
	if p.Stage == stageConfirming {
		if err := os.Remove(abs + ".prev"); err != nil && !os.IsNotExist(err) {
			return Recovery{}, err
		}
		l.Pending = nil
		return Recovery{}, writeLedger(abs, l)
	}
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
	// ⚠ **This one DELETES the backup.** A Commit starting beside it writes a new `.prev` for its own
	// transaction, and an unsynchronised Confirm removes it — leaving that replacement with nothing
	// to roll back to, which is the state its whole journal exists to prevent (review R10).
	release, got := takeInstall(abs)
	if !got {
		return errInstallHeld(abs)
	}
	defer release()
	l, err := readLedger(abs)
	if err != nil || l.Pending == nil {
		return err
	}
	// Say what is about to happen before doing it. Removing the backup first and clearing the record
	// second would leave "a .prev with no record" on an interruption — which is the very state that
	// means "a replacement was interrupted before it was recorded, put the old build back" (see
	// Salvage). The two would be indistinguishable, and the recovery for one is the opposite of the
	// recovery for the other.
	l.Pending.Stage = stageConfirming
	if err := writeLedger(abs, l); err != nil {
		return err
	}
	if err := removePrev(abs + ".prev"); err != nil && !os.IsNotExist(err) {
		return err
	}
	l.Pending = nil
	return writeLedger(abs, l)
}

// LeftCleanly says this generation is shutting down on purpose before its window elapsed, so the next
// start should not read it as a build that fell over. Without it, stopping a daemon a few seconds
// after an update — an ordinary thing to do — would roll that update back on the next start.
func LeftCleanly(target string) error {
	abs := resolveInstall(target)
	// Read, decrement, write — a lost update here is a generation that either never counted or
	// counted twice, and counting twice is what rolls a good build back.
	release, got := takeInstall(abs)
	if !got {
		return errInstallHeld(abs)
	}
	defer release()
	l, err := readLedger(abs)
	if err != nil || l.Pending == nil || l.Pending.Starts == 0 {
		return err
	}
	l.Pending.Starts--
	return writeLedger(abs, l)
}

// Salvage recovers from a replacement that was interrupted before it was recorded.
//
// Commit writes the backup, replaces the binary, pre-flights it, and only then writes the journal.
// A process or machine that dies inside that sequence leaves a `.prev` and NO record — and the
// binary at target is then one of two things, with nothing on disk to say which: the original (the
// replacement had not happened yet) or a new build that never finished its pre-flight.
//
// Putting the backup back is right for both. In the first case it writes the same bytes that are
// already there; in the second it undoes an unverified replacement. CLIENT_LIFECYCLE §9.3: "even if
// the process or machine is interrupted, the next start reconciles the journal against the files and
// recovers the last good version."
//
// Returns the path it restored, or "" when there was nothing to do.
func Salvage(target string) (string, error) {
	abs := resolveInstall(target)
	// ⚠ **A `.prev` means two opposite things, and the lock is what tells them apart.** Commit writes
	// the backup, replaces the binary, pre-flights it, and only THEN writes the journal — so for the
	// whole length of a pre-flight there is a backup on disk with no record beside it, which is
	// exactly the state this function is built to act on. Measured with two processes: without this,
	// a daemon starting in that window restored the old build over the new one and deleted the
	// backup, so the update was undone mid-flight AND its rollback source was gone (review R10).
	//
	// Waiting rather than skipping, and the ordering below is why it needs nothing else: by the time
	// the lock is free the replacement has written its journal, so the `l.Pending != nil` check just
	// below sends this away on its own.
	release, got := takeInstall(abs)
	if !got {
		return "", errInstallHeld(abs)
	}
	defer release()
	prev := abs + ".prev"
	if _, err := os.Stat(prev); err != nil {
		return "", nil // no interrupted replacement here
	}
	l, err := readLedger(abs)
	if err != nil {
		return "", err
	}
	if l.Pending != nil {
		return "", nil // a recorded transaction; Resume owns it, not this
	}
	if err := restorePrevious(abs); err != nil {
		return "", err
	}
	return abs, nil
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
