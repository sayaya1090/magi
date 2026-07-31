package builtin

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The background registry is capped so a long session cannot leak *bgProc entries and log files
// without bound. What the cap must never do is cost the session something it still needs: a
// RUNNING job dropped from the registry is a job whose output can no longer be read and whose
// outcome can no longer be claimed, and a log file removed under a job that is still in the
// registry turns every later bash_output into an error.
//
// Nothing pinned any of that — the prune was reached by 10% of its statements. It is correct; this
// is the test that says so, and that says it again if the ordering or the running-job exemption is
// ever changed.
func TestPruningKeepsEveryRunningJobAndTheNewestFinishedOnes(t *testing.T) {
	dir := t.TempDir()
	m := &bgManager{procs: map[string]*bgProc{}}
	logOf := func(id string) string { return filepath.Join(dir, id+".log") }
	add := func(id string, done bool) {
		if err := os.WriteFile(logOf(id), []byte("captured output"), 0o644); err != nil {
			t.Fatal(err)
		}
		m.procs[id] = &bgProc{logPath: logOf(id), done: done}
	}

	const finished, running = 40, 5
	for i := 1; i <= finished; i++ {
		add(fmt.Sprintf("bg_%d", i), true)
	}
	for i := finished + 1; i <= finished+running; i++ {
		add(fmt.Sprintf("bg_%d", i), false)
	}

	m.pruneLocked()

	if len(m.procs) != maxBgProcs {
		t.Errorf("pruned to %d entries, want the cap of %d", len(m.procs), maxBgProcs)
	}
	// Every running job survives, however far over the cap the registry went.
	for i := finished + 1; i <= finished+running; i++ {
		if _, ok := m.procs[fmt.Sprintf("bg_%d", i)]; !ok {
			t.Errorf("bg_%d was still running and was dropped — its output is now unreadable", i)
		}
	}
	// The ones that went are the OLDEST finished ones. Dropping the newest instead would keep a
	// registry full of jobs nobody is going to ask about and evict the one just started.
	dropped := finished + running - maxBgProcs
	for i := 1; i <= dropped; i++ {
		if _, ok := m.procs[fmt.Sprintf("bg_%d", i)]; ok {
			t.Errorf("bg_%d is among the %d oldest finished jobs and was kept", i, dropped)
		}
	}
	for i := dropped + 1; i <= finished; i++ {
		if _, ok := m.procs[fmt.Sprintf("bg_%d", i)]; !ok {
			t.Errorf("bg_%d is newer than the %d that should have gone, and went", i, dropped)
		}
	}
	// A dropped job's log file goes with it; a kept job's log file stays. The first is the whole
	// point of the cap, and the second is what bash_output reads.
	for i := 1; i <= finished+running; i++ {
		id := fmt.Sprintf("bg_%d", i)
		_, kept := m.procs[id]
		_, err := os.Stat(logOf(id))
		if kept && err != nil {
			t.Errorf("%s is in the registry but its log was deleted: %v", id, err)
		}
		if !kept && err == nil {
			t.Errorf("%s was dropped but its log file is still on disk", id)
		}
	}
}

// Under the cap nothing is touched — a prune that trimmed a registry with room left would be
// deleting output the session can still ask for.
func TestPruningLeavesARegistryUnderTheCapAlone(t *testing.T) {
	m := &bgManager{procs: map[string]*bgProc{}}
	for i := 1; i <= maxBgProcs; i++ {
		m.procs[fmt.Sprintf("bg_%d", i)] = &bgProc{done: true}
	}
	m.pruneLocked()
	if len(m.procs) != maxBgProcs {
		t.Errorf("a registry exactly at the cap was trimmed to %d", len(m.procs))
	}
}

// A registry of nothing but running jobs is left whole however far over the cap it goes. The cap
// bounds what is FINISHED; there is no honest way to free a job that is still working.
func TestPruningCannotShrinkARegistryOfRunningJobs(t *testing.T) {
	m := &bgManager{procs: map[string]*bgProc{}}
	const n = maxBgProcs * 2
	for i := 1; i <= n; i++ {
		m.procs[fmt.Sprintf("bg_%d", i)] = &bgProc{done: false}
	}
	m.pruneLocked()
	if len(m.procs) != n {
		t.Errorf("%d of %d running jobs were pruned to honour the cap", n-len(m.procs), n)
	}
}
