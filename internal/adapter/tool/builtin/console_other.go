//go:build !windows

package builtin

// guardConsole is a no-op off Windows: detachTTY puts every child in its own session with no
// controlling terminal (sandbox_procattr_other.go), so a child cannot reach — let alone reconfigure
// — the terminal magi's UI is drawn on. Nothing to snapshot, nothing to restore.
func guardConsole() func() { return func() {} }
