package app

import (
	"path/filepath"
	"strings"
)

// escapesTree reports whether a filepath.Rel result climbs out of the base it was taken against.
//
// One line, six callers, and every one of them is a safety perimeter: is this path inside the
// workspace, inside the scratch area, inside a directory this run made, inside the tree the trash
// serves. They each wrote it as `rel == ".." || strings.HasPrefix(rel, "../")`.
//
// ⚠ `filepath.Rel` answers in the OS's separator. On Windows an escaping path comes back as
// `..\thing`, which does not start with `../` — so every one of those checks said "it does not
// escape", and every one of them fails in the PERMISSIVE direction:
//
//   - outsideWorkspace read a path outside the tree as inside it, and the gate that asks the
//     council before an irreversible delete outside the workspace never fired.
//   - arrivalHolds could not place a target it should have refused to place, and its "cannot say,
//     so assume it holds something" branch — written to fail closed — was unreachable.
//   - isScratchPath and the run guard read somebody else's directory as the run's own.
//
// A perimeter that fails open is worse than one that is missing, because it reports success.
// Measured 2026-09-10 on Windows: `rm -rf src` on a directory holding a file the person brought
// was not gated at all (TestWhatThisSessionBuiltIsNotAskedAbout).
//
// `trash.go` is the tell — the line above its check spells the separator correctly for the trash
// directory and the check itself does not. It was known here and forgotten one line later, which
// is what a rule written six times does.
func escapesTree(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// underTree reports whether a path, relative to the same base, is at or below rel.
//
// The prefix has to carry the separator or `src` matches `srcs`, and it has to be the OS's
// separator or it matches nothing at all on Windows — the two halves of the same mistake. Both
// sides are filepath.Rel results, so they agree about which separator that is.
func underTree(path, rel string) bool {
	return path == rel || strings.HasPrefix(path, rel+string(filepath.Separator))
}

// absAsGiven resolves a tool-supplied path against a base, without folding a rooted one into it.
//
// The question here is "which file does this name", not "may this be touched" — the callers
// reconstruct what a command changed, and a command naming `/etc/foo` changed `/etc/foo`.
//
// ⚠ `filepath.IsAbs` alone gets that wrong on Windows. `/etc/foo` and `\Windows\x` are rooted and
// carry no volume, so IsAbs says false and a plain join turns them into `<base>\etc\foo` — a
// different file, quietly. The change record then describes a file nobody wrote. Bash paths reach
// these callers without passing the file tools' jail, so a rooted path really does arrive here.
//
// A path that is rooted is returned as it is, which is where it points. A path that stays inside
// the base is joined to it. Nothing is refused: that decision belongs to the jails, and this is not
// one — see resolvePath and writeApprovalDiff, which mirror it and must refuse.
func absAsGiven(base, path string) string {
	if !filepath.IsAbs(path) && filepath.IsLocal(path) {
		return filepath.Clean(filepath.Join(base, path))
	}
	return filepath.Clean(path)
}
