//go:build windows

package tui

import "golang.org/x/sys/windows"

// cpUTF8 is CP_UTF8 — the console code page under which the TUI's UTF-8 byte
// stream is interpreted correctly, so multi-byte glyphs (e.g. Korean) survive a
// drag-select copy out of the console into another app.
const cpUTF8 = 65001

// configureConsole switches the Windows console's input and output code pages to
// UTF-8 for the lifetime of the TUI and returns a restorer to run on exit. The
// legacy default (an ANSI code page such as CP949/CP936) makes conhost store the
// selection buffer in that code page, so drag-copying our UTF-8 output pastes as
// mojibake elsewhere; CP_UTF8 makes the copy round-trip as real Unicode. Best
// effort — a redirected handle or a console that rejects the change leaves the
// prior code page in place and the restorer is a no-op for whatever we could not
// set, so we never leave the user's console in a worse state than we found it.
func configureConsole() func() {
	prevOut, outErr := windows.GetConsoleOutputCP()
	prevIn, inErr := windows.GetConsoleCP()

	setOut := outErr == nil && windows.SetConsoleOutputCP(cpUTF8) == nil
	setIn := inErr == nil && windows.SetConsoleCP(cpUTF8) == nil

	return func() {
		if setOut {
			_ = windows.SetConsoleOutputCP(prevOut)
		}
		if setIn {
			_ = windows.SetConsoleCP(prevIn)
		}
	}
}
