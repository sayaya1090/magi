//go:build !windows

package tui

// configureConsole is a no-op off Windows: POSIX terminals already carry UTF-8
// end to end, so there is no code page to switch. Returns a no-op restorer to
// keep Run's call site platform-agnostic.
func configureConsole() func() { return func() {} }
