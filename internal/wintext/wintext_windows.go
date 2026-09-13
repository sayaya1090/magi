//go:build windows

package wintext

import (
	"bytes"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/sys/windows"
)

// mbErrInvalidChars makes MultiByteToWideChar REFUSE a byte sequence the code page cannot
// represent, instead of substituting U+FFFD for it. That refusal is what lets the candidates below
// be tried in order: a code page that cannot read these bytes says so and the next one gets a turn.
const mbErrInvalidChars = 0x00000008

// ToUTF8 decodes b from the code page a Windows child most likely wrote it in, when b is text that
// is not already UTF-8. See the package doc for why, and for what it leaves alone.
func ToUTF8(b []byte) []byte {
	if len(b) == 0 || utf8.Valid(b) {
		return b
	}
	// Not text. A NUL means binary — an ELF header, a UTF-16 stream, an image — and this tree
	// already treats that as a thing to LABEL rather than to read (builtin.isBinary).
	if bytes.IndexByte(b, 0) >= 0 {
		return b
	}
	cps := candidates()
	for _, cp := range cps {
		if s, ok := decode(cp, b, mbErrInvalidChars); ok {
			return s
		}
	}
	// Nothing read it cleanly — mixed encodings, or a cut through a multi-byte character (the
	// capture elides the middle of large output, and the cut lands where it lands). Take the most
	// likely code page leniently: the characters it can place are then readable, and the ones it
	// cannot become U+FFFD — which is exactly what ALL of them become without this call.
	if len(cps) > 0 {
		if s, ok := decode(cps[0], b, 0); ok {
			return s
		}
	}
	return b
}

// candidates lists the code pages to try, most likely first.
//
// The console's output code page comes first because that is what a child writing to our redirected
// handles uses: PowerShell initialises `[Console]::OutputEncoding` from it, and console programs
// call it directly. A process with no console (the detached daemon) has no such answer, and there
// PowerShell 5.1 falls back to .NET's `Encoding.Default`, which on .NET Framework is the machine's
// ANSI code page — so that is second, and on a machine where the two agree it is the same answer
// twice and the list collapses to one.
//
// UTF-8 is never a candidate: ToUTF8 has already ruled out valid UTF-8 before asking.
func candidates() []uint32 {
	var cps []uint32
	add := func(cp uint32) {
		if cp == 0 || cp == 65001 {
			return
		}
		for _, had := range cps {
			if had == cp {
				return
			}
		}
		cps = append(cps, cp)
	}
	if cp, err := windows.GetConsoleOutputCP(); err == nil {
		add(cp)
	}
	add(windows.GetACP())
	return cps
}

// decode runs one MultiByteToWideChar pass, sized then converted. It returns false when the code
// page refuses the bytes (with mbErrInvalidChars) or the call fails for any other reason.
func decode(cp uint32, b []byte, flags uint32) ([]byte, bool) {
	n, err := windows.MultiByteToWideChar(cp, flags, &b[0], int32(len(b)), nil, 0)
	if err != nil || n <= 0 {
		return nil, false
	}
	w := make([]uint16, n)
	n, err = windows.MultiByteToWideChar(cp, flags, &b[0], int32(len(b)), &w[0], n)
	if err != nil || n <= 0 {
		return nil, false
	}
	// utf16.Decode, not windows.UTF16ToString: the latter stops at the first NUL, and while NULs are
	// ruled out above for the INPUT, a code page can still produce one and silently losing the rest
	// of a command's output is not a thing to leave available.
	return []byte(string(utf16.Decode(w[:n]))), true
}
