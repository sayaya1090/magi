package app

import (
	"path/filepath"
	"strings"
)

// diffFile is one file's section of a unified git diff.
type diffFile struct {
	path   string // the b-side path from the "diff --git a/… b/…" header ("" if none)
	body   string // the section text, including its header line
	binary bool   // a binary diff ("Binary files … differ" / "GIT binary patch")
}

// splitDiffFiles splits a unified git diff into per-file sections (each beginning with a
// "diff --git " header). A diff with no such header (e.g. the `git status --short`
// fallback) is returned as a single path-less section so callers pass it through unchanged.
func splitDiffFiles(diff string) []diffFile {
	if strings.TrimSpace(diff) == "" {
		return nil
	}
	lines := strings.Split(diff, "\n")
	var files []diffFile
	flush := func(start, end int) {
		body := strings.Join(lines[start:end], "\n")
		f := diffFile{body: body, path: diffHeaderPath(lines[start])}
		if strings.Contains(body, "\nBinary files ") || strings.HasPrefix(body, "Binary files ") ||
			strings.Contains(body, "GIT binary patch") {
			f.binary = true
		}
		files = append(files, f)
	}
	start := -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, "diff --git ") {
			if start >= 0 {
				flush(start, i)
			}
			start = i
		}
	}
	switch {
	case start >= 0:
		flush(start, len(lines))
	default:
		files = append(files, diffFile{body: diff}) // no header → pass-through bucket
	}
	return files
}

// diffHeaderPath extracts the b-side path from a "diff --git a/PATH b/PATH" header.
// Best-effort: paths with embedded " b/" are rare and degrade to "".
func diffHeaderPath(header string) string {
	rest := strings.TrimPrefix(header, "diff --git ")
	if i := strings.LastIndex(rest, " b/"); i >= 0 {
		return rest[i+3:]
	}
	return ""
}

// scopeDiffToTurn drops file sections that were ALREADY dirty before the turn AND were not
// touched by this turn's write/edit tools — so a pre-existing uncommitted change (e.g. a
// stray build artifact) doesn't leak into the council's evidence as if the agent made it.
// A file the agent touched is always kept (even if it was already dirty); a file with no
// known path is kept (can't prove it's stale). dirtyBefore==nil (non-git / no baseline)
// keeps everything, preserving prior behavior.
func scopeDiffToTurn(diff string, dirtyBefore, touched map[string]bool, workdir string) string {
	if len(dirtyBefore) == 0 {
		return diff
	}
	// touched paths come from the model's raw tool args (may be absolute or "./"-prefixed),
	// while git paths are repo-root-relative — normalize so a touched pre-dirty file is
	// matched and kept, not wrongly dropped.
	touchedRel := make(map[string]bool, len(touched))
	for p := range touched {
		touchedRel[relToWorkdir(p, workdir)] = true
	}
	files := splitDiffFiles(diff)
	kept := make([]string, 0, len(files))
	for _, f := range files {
		if f.path != "" && dirtyBefore[f.path] && !touchedRel[f.path] {
			continue // pre-existing change the agent didn't make this turn
		}
		kept = append(kept, strings.TrimRight(f.body, "\n"))
	}
	return strings.Join(kept, "\n")
}

// relToWorkdir maps a tool-supplied path to the repo-root-relative, slash form git uses, so
// it can be compared against diff/status paths. Falls back to a cleaned form on any mismatch.
func relToWorkdir(p, workdir string) string {
	c := filepath.Clean(p)
	if workdir != "" && filepath.IsAbs(c) {
		if rel, err := filepath.Rel(workdir, c); err == nil {
			c = rel
		}
	}
	return filepath.ToSlash(c)
}

// filterCouncilDiff drops binary-file sections from a git diff: a binary blob (a .pyc, an
// image) is never useful evidence for the council and only adds noise and length to what
// each member must read. Text diffs are kept verbatim.
func filterCouncilDiff(diff string) string {
	files := splitDiffFiles(diff)
	if len(files) == 0 {
		return ""
	}
	kept := make([]string, 0, len(files))
	for _, f := range files {
		if f.binary {
			continue
		}
		kept = append(kept, strings.TrimRight(f.body, "\n"))
	}
	return strings.Join(kept, "\n")
}
