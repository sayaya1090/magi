// Package pathx answers one question the standard library answers per-platform: does a path start
// at a root, in EITHER platform's spelling.
//
// `filepath.IsAbs` and `filepath.IsLocal` are built for the platform they run on, and that is right
// for opening files. It is wrong for judging a string a CALLER wrote. A model writes `/server` or
// `\Windows\System32` without thinking about which OS it is on, and on Linux the second one is not
// rooted at all — it is an ordinary filename that happens to contain backslashes. So the same guard
// refused it on Windows and passed it everywhere else, and the tests that said "however this
// platform spells its own paths" went red on the Linux runner (2026-09-10).
//
// One predicate, in one place, because this repository has already paid for the alternative: the
// irreversible-delete gate kept its own inline copy of the same three lines, and the note left
// there says what that cost — "the rooted-path reading had to be fixed twice or stay half fixed".
package pathx

// Rooted reports whether p begins at a root in either platform's spelling.
//
// True for a leading separator of either kind (`/etc/passwd`, `\Windows\System32`), for a UNC share
// (`\\host\share`), and for a drive letter (`C:\x`, `c:/x`, and bare `C:` — which names the current
// directory ON that drive, not a file here).
//
// False for everything that begins with a name. A backslash INSIDE a name is not a root: `src\a.txt`
// is a legal file on Linux and this must not refuse it. Only the first character decides, which is
// also what makes the answer the same on every platform.
func Rooted(p string) bool {
	if p == "" {
		return false
	}
	if p[0] == '/' || p[0] == '\\' {
		return true
	}
	return len(p) >= 2 && p[1] == ':' && isDriveLetter(p[0])
}

func isDriveLetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
