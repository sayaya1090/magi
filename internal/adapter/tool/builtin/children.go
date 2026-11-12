package builtin

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// The child ledger is how a magi killed outright (SIGKILL, a crash, the machine pulling the plug)
// still gets its processes back. Every command runs under Setsid, so nothing ends a child when the
// parent dies, and no shutdown hook ran to do it. Each child is written down — its pid and its
// start time, fsynced — while it runs and struck off when it is reaped; the next start reads what
// is left and ends those process groups. The start time is what makes that safe: a pid the system
// has since handed to something else has a different one, and is left alone.
var (
	ledgerMu  sync.Mutex
	ledgerDir string
)

// SetChildLedger turns the ledger on (dir) or off (""). Off is the default, so a test or a
// one-shot run that never sets it writes nothing. An error means the directory could not be made:
// the ledger stays off, and a crash would leave this process's children running.
func SetChildLedger(dir string) error {
	var err error
	if dir != "" {
		if err = os.MkdirAll(dir, 0o700); err != nil {
			dir = ""
		}
	}
	ledgerMu.Lock()
	defer ledgerMu.Unlock()
	ledgerDir = dir
	return err
}

// dropEntry strikes an entry off. One already gone is not a failure.
func dropEntry(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// noteChild records a started child and returns the call that strikes it off once it is reaped.
// When the ledger is off, or the start time cannot be read, it records nothing. A write that
// fails leaves the child unrecorded — the run goes on; only a crash's cleanup would miss it.
func noteChild(pid int) (release func()) {
	ledgerMu.Lock()
	dir := ledgerDir
	ledgerMu.Unlock()
	none := func() {}
	if dir == "" || pid <= 0 {
		return none
	}
	start, ok := procStart(pid)
	if !ok {
		return none
	}
	path := filepath.Join(dir, strconv.Itoa(pid))
	if err := writeEntry(path, start); err != nil {
		bashDebugf("child ledger: pid %d not recorded: %v", pid, err)
		return none
	}
	return func() {
		if err := dropEntry(path); err != nil {
			bashDebugf("child ledger: pid %d not struck off: %v", pid, err)
		}
	}
}

// writeEntry writes path atomically: a temp file, synced, then renamed. A failure removes the temp.
func writeEntry(path, start string) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.WriteString(start)
	if serr := f.Sync(); werr == nil {
		werr = serr
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp, path)
	}
	if werr != nil {
		if derr := dropEntry(tmp); derr != nil {
			return errors.Join(werr, derr)
		}
	}
	return werr
}

// ReapOrphans ends the process groups a previous process left in the ledger at dir and empties
// it, returning the pids it ended and, as text, whatever it could not settle (a group that would
// not die, an entry it could not remove). Run it at start-up, once this process holds the
// workspace — before then the previous owner may still be alive and those children still its own.
// An entry whose pid is gone, or now belongs to a process started at another time, is only dropped.
func ReapOrphans(dir string) (reaped []int, unsettled []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			unsettled = append(unsettled, fmt.Sprintf("ledger %s unreadable: %v", dir, err))
		}
		return nil, unsettled
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if pid, perr := strconv.Atoi(e.Name()); perr == nil && pid > 0 {
			recorded, rerr := os.ReadFile(path)
			if now, ok := procStart(pid); rerr == nil && ok && now == strings.TrimSpace(string(recorded)) {
				if kerr := killGroup(pid); kerr != nil {
					unsettled = append(unsettled, fmt.Sprintf("process group %d not ended: %v", pid, kerr))
					continue // keep the entry: the next start tries again
				}
				reaped = append(reaped, pid)
			}
		}
		// A torn write (.tmp), a stale pid, or a group just ended: the entry is done.
		if derr := dropEntry(path); derr != nil {
			unsettled = append(unsettled, fmt.Sprintf("entry %s not removed: %v", path, derr))
		}
	}
	sort.Ints(reaped)
	return reaped, unsettled
}
