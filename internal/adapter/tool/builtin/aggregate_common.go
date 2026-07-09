package builtin

import (
	"fmt"
	"io/fs"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sayaya1090/magi/internal/port"
)

// The aggregate tools (tabulate, countmatches, countlines, groupby) give read-only
// agents a way to REDUCE data — sum a column, count matches, tally groups — without
// a shell. A read-only explorer with only read/grep cannot sum 8874 lines of
// coverage.out; it thrashes (re-reading the file) and delegates the arithmetic back
// to its parent. These tools close that gap. They are pure Go (no awk/wc/grep exec),
// jailed to the workdir, and behave identically on macOS, Linux, and Windows.

// splitLines splits file bytes into lines, tolerating both LF and CRLF endings (the
// trailing \r is trimmed) so a Windows-authored file tallies the same as a Unix one.
// A trailing newline does not produce a final empty line; a file with no trailing
// newline still counts its last line.
func splitLines(b []byte) []string {
	s := string(b)
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

// fields splits one line into columns. An empty delimiter means whitespace-delimited
// (strings.Fields — collapses runs, the coverage.out / ps / df shape); a non-empty
// delimiter (e.g. ",") splits on it exactly (CSV/TSV).
func fields(line, delim string) []string {
	if delim == "" {
		return strings.Fields(line)
	}
	return strings.Split(line, delim)
}

// cell returns the 1-indexed column value of a split line, or "" if out of range.
func cell(cols []string, oneIdx int) string {
	if oneIdx < 1 || oneIdx > len(cols) {
		return ""
	}
	return cols[oneIdx-1]
}

// parseFloatCell parses a numeric cell, tolerating surrounding spaces and a trailing
// '%'. ok is false for a non-numeric cell (the caller skips it).
func parseFloatCell(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "%")
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false // NaN/Inf tokens are not usable aggregation values
	}
	return v, true
}

// readSingle resolves a required single-file path (jailed) and reads it capped. It
// suggests same-named files on a miss (resolveOrSuggest), mirroring read/grep.
// truncated is true when the file exceeded maxReadBytes and was clipped — the caller
// MUST surface it so an aggregate over a huge file is not reported as complete.
func readSingle(env port.ToolEnv, path string) (data []byte, rel string, truncated bool, errMsg string) {
	if strings.TrimSpace(path) == "" {
		return nil, "", false, "path is required"
	}
	abs, located, suggest := resolveOrSuggest(env.Workdir, path)
	if abs == "" {
		if suggest != "" {
			return nil, "", false, "not found: " + path + " (" + suggest + ")"
		}
		return nil, "", false, "not found: " + path
	}
	d, trunc, err := readCapped(abs, maxReadBytes)
	if err != nil {
		return nil, "", false, err.Error()
	}
	rel = located
	if rel == "" {
		rel, _ = filepath.Rel(filepath.Clean(env.Workdir), abs)
		rel = filepath.ToSlash(rel)
	}
	return d, rel, trunc, ""
}

// walkFiles applies fn to each file selected by exactly one of path (a single file)
// or glob (a workdir tree filter, glob.go semantics). Binary files, dot-directories,
// and in-workdir symlinks that escape the jail are skipped. It returns the number of
// files visited and whether any file was clipped at maxReadBytes (the caller surfaces
// it). fn receives the workdir-relative slash path and the capped bytes.
func walkFiles(env port.ToolEnv, glob, path string, fn func(rel string, data []byte)) (count int, truncated bool, errMsg string) {
	hasPath, hasGlob := strings.TrimSpace(path) != "", strings.TrimSpace(glob) != ""
	if !hasPath && !hasGlob {
		return 0, false, "one of path or glob is required"
	}
	if hasPath && hasGlob {
		return 0, false, "use only one of path or glob"
	}
	if hasPath {
		data, rel, trunc, msg := readSingle(env, path)
		if msg != "" {
			return 0, false, msg
		}
		if isBinary(data) {
			return 0, false, ""
		}
		fn(rel, data)
		return 1, trunc, ""
	}
	root := filepath.Clean(env.Workdir)
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && p != root {
				return fs.SkipDir
			}
			return nil
		}
		if !grepGlobMatch(glob, env.Workdir, p, d.Name()) {
			return nil
		}
		if symlinkEscapesJail(env.Workdir, p, d) {
			return nil
		}
		data, trunc, rerr := readCapped(p, maxReadBytes)
		if rerr != nil || isBinary(data) {
			return nil
		}
		if trunc {
			truncated = true
		}
		rel, _ := filepath.Rel(root, p)
		fn(filepath.ToSlash(rel), data)
		count++
		return nil
	})
	return count, truncated, ""
}

// cmpOp evaluates a numeric comparison for the tabulate/groupby row filter.
func cmpOp(op string, a, b float64) (bool, bool) {
	switch op {
	case ">":
		return a > b, true
	case ">=":
		return a >= b, true
	case "<":
		return a < b, true
	case "<=":
		return a <= b, true
	case "==", "=":
		return a == b, true
	case "!=":
		return a != b, true
	}
	return false, false
}

// invalidArgs wraps a JSON-unmarshal failure into the shared error result form.
func invalidArgs(err error) string { return fmt.Sprintf("invalid arguments: %v", err) }
