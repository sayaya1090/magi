package builtin

import (
	"path/filepath"
	"sync"
)

// pathLocks serializes the read-modify-write cycle of the file-writing tools
// (edit, multiedit, write) per target path within this process. Atomic
// temp-and-rename keeps any single write from tearing a file, but two concurrent
// edits of the SAME file — e.g. from parallel subagents sharing a workdir — can
// still interleave their read → modify → write and lose one update:
//
//	A: read v0 ─────────────── rename v1
//	B:      read v0 ───────────────────── rename v1'  (v1 clobbered, A's edit lost)
//
// Holding the per-path lock across the whole cycle serializes them so both land in
// order. The lock is keyed per path, so edits to different files stay parallel.
//
// It is in-process only: it does NOT guard against a separate process, the agent's
// own bash tool, or an external editor mutating the file. The edit tool's
// hash-anchored line refs remain the guard for changes made outside this lock.
var pathLocks = &pathLockSet{m: map[string]*refMutex{}}

// refMutex is a mutex plus a reference count (waiters + current holder) so an
// idle entry can be dropped from the set instead of leaking one mutex per file
// ever touched over a long autonomous run. n is guarded by pathLockSet.mu.
type refMutex struct {
	mu sync.Mutex
	n  int
}

type pathLockSet struct {
	mu sync.Mutex
	m  map[string]*refMutex
}

// lock acquires the lock for key and returns the release function. key must be a
// stable per-file identity (see lockKey) so two references to the same file share
// one lock. The returned func must be called exactly once (defer it).
func (s *pathLockSet) lock(key string) func() {
	s.mu.Lock()
	rm := s.m[key]
	if rm == nil {
		rm = &refMutex{}
		s.m[key] = rm
	}
	rm.n++ // registered as a waiter before blocking, so the entry survives until we hold it
	s.mu.Unlock()

	rm.mu.Lock()
	return func() {
		rm.mu.Unlock()
		s.mu.Lock()
		rm.n--
		if rm.n == 0 {
			delete(s.m, key) // last user gone: reclaim the entry
		}
		s.mu.Unlock()
	}
}

// lockKey maps an absolute path to a stable per-file lock key, resolving symlinks
// so two paths pointing at the same file share one lock — mirroring the symlink
// resolution atomicWriteFile does before it writes. A path that doesn't exist yet
// (a fresh write) falls back to its cleaned form.
func lockKey(abs string) string {
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return filepath.Clean(abs)
}
