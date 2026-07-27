//go:build windows

package builtin

import "golang.org/x/sys/windows"

// guardConsole snapshots the console's input/output mode and code pages before a foreground child
// runs, and restores whatever the child changed.
//
// On Windows detachTTY is a deliberate no-op (see sandbox_windows.go), so every child INHERITS
// magi's console. A program that wants a terminal — ssh asking for a password, a serial getty —
// opens CONIN$/CONOUT$ directly, which the NUL stdin the tool hands it does not block, and calls
// SetConsoleMode to turn echo off. When the timeout then kills it with `taskkill /T /F` it never
// gets to put the mode back, and nothing else does: the TUI's own configureConsole saves the code
// PAGES only, once for the whole session. The console is left with ENABLE_MOUSE_INPUT and
// ENABLE_VIRTUAL_TERMINAL_INPUT cleared, so the UI's mouse clicks stop arriving — for the rest of
// the run, long after the command that did it.
//
// Restoring is the narrow fix: it does not change how the child is created (DETACHED_PROCESS was
// tried for that and broke output capture — sandbox_windows.go records why), it only undoes the
// damage afterwards. Best effort throughout: a redirected or non-console handle simply yields no
// snapshot and no restore, and a value the child left alone is not rewritten.
func guardConsole() func() {
	var restores []func()

	for _, id := range []uint32{windows.STD_INPUT_HANDLE, windows.STD_OUTPUT_HANDLE} {
		h, err := windows.GetStdHandle(id)
		if err != nil || h == windows.InvalidHandle {
			continue // redirected to a file/pipe, or no console at all (headless)
		}
		var mode uint32
		if windows.GetConsoleMode(h, &mode) != nil {
			continue // not a console handle
		}
		h, want := h, mode
		restores = append(restores, func() {
			var got uint32
			if windows.GetConsoleMode(h, &got) == nil && got != want {
				_ = windows.SetConsoleMode(h, want)
			}
		})
	}

	if in, err := windows.GetConsoleCP(); err == nil {
		restores = append(restores, func() {
			if got, err := windows.GetConsoleCP(); err == nil && got != in {
				_ = windows.SetConsoleCP(in)
			}
		})
	}
	if out, err := windows.GetConsoleOutputCP(); err == nil {
		restores = append(restores, func() {
			if got, err := windows.GetConsoleOutputCP(); err == nil && got != out {
				_ = windows.SetConsoleOutputCP(out)
			}
		})
	}

	return func() {
		for _, r := range restores {
			r()
		}
	}
}
