//go:build !darwin && !linux

package builtin

// procStart cannot tell when a process started here, so the ledger records nothing and a reap
// never ends a process it cannot prove it started.
func procStart(int) (string, bool) { return "", false }
