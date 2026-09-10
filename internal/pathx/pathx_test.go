package pathx

import "testing"

// The answer must be the same on every platform, because the string being judged was written by
// somebody who was not thinking about platforms.
//
// The literals are written out rather than composed with filepath.Join for that reason: `Join` uses
// THIS platform's separator, and the shapes that matter are the ones a caller types.
func TestRootedReadsBothSpellings(t *testing.T) {
	for name, p := range map[string]string{
		"a unix absolute":          "/etc/passwd",
		"the unix root":            "/",
		"rooted with no volume":    `\Windows\System32\drivers\etc\hosts`,
		"rooted, separators mixed": `\Windows/System32/config`,
		"a UNC share":              `\\host\share\x`,
		"a drive with a backslash": `C:\Windows`,
		"a drive with a slash":     "C:/Windows",
		"a lowercase drive":        "c:/windows",
		"a bare drive":             "C:",
	} {
		if !Rooted(p) {
			t.Errorf("%s (%q): 뿌리가 아니라고 읽었다 — 부른 이름과 다른 파일을 열게 된다", name, p)
		}
	}
}

// And an ordinary name stays ordinary, or the guard refuses honest callers.
//
// ⚠ `src\a.txt` is the one that decides this. A backslash INSIDE a name is not a root, and on Linux
// that is a legal file — refusing it would break a caller who did nothing wrong. Only the first
// character is allowed to decide.
func TestAnOrdinaryNameIsNotRooted(t *testing.T) {
	for name, p := range map[string]string{
		"empty":                       "",
		"a file":                      "a.txt",
		"the workdir itself":          ".",
		"with a leading dot":          "./a.txt",
		"below a directory":           "src/a.txt",
		"a backslash inside":          `src\a.txt`,
		"climbing out":                "../outside.txt",
		"a name with a space":         "my file.txt",
		"a colon that is not a drive": "ab:c",
		"a colon further along":       "src:a.txt",
		"a name that starts with a digit and a colon": "1:x",
	} {
		if Rooted(p) {
			t.Errorf("%s (%q): 뿌리라고 읽었다 — 멀쩡한 이름을 거절하게 된다", name, p)
		}
	}
}
