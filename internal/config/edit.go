package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// SetKey surgically sets `key = "value"` under the given TOML table in the file
// at path, preserving the rest of the file (comments, template, other sections).
// section "" targets a top-level key (in the preamble before the first table).
// An empty value removes the key. The file is created if absent.
//
// It is intentionally limited to flat string keys (the /route editor only writes
// `model` and the `[routing]` table), so it stays a safe line-level edit rather
// than a full TOML round-trip that would discard comments.
func SetKey(path, section, key, value string) error {
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := []string{}
	if len(b) > 0 {
		lines = strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	}

	target := fmt.Sprintf("%s = %q", key, value)
	keyRe := regexp.MustCompile(`^\s*#?\s*` + regexp.QuoteMeta(key) + `\s*=`)
	headerRe := func(name string) *regexp.Regexp {
		return regexp.MustCompile(`^\s*\[` + regexp.QuoteMeta(name) + `\]\s*$`)
	}
	isTable := func(s string) bool { return strings.HasPrefix(strings.TrimSpace(s), "[") }

	// Determine the [start,end) line range of the target section.
	start, end := 0, len(lines)
	if section != "" {
		hdr := headerRe(section)
		h := -1
		for i, ln := range lines {
			if hdr.MatchString(ln) {
				h = i
				break
			}
		}
		if h < 0 {
			// No such section: append it (unless we're clearing, which is then a no-op).
			if value == "" {
				return nil
			}
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, "["+section+"]", target)
			return writeLines(path, lines)
		}
		start = h + 1
		end = len(lines)
		for i := start; i < len(lines); i++ {
			if isTable(lines[i]) {
				end = i
				break
			}
		}
	} else {
		// Top-level: the preamble before the first table.
		for i, ln := range lines {
			if isTable(ln) {
				end = i
				break
			}
		}
	}

	// Replace an existing (active or commented) key line within the section.
	for i := start; i < end; i++ {
		if keyRe.MatchString(lines[i]) {
			if value == "" {
				lines = append(lines[:i], lines[i+1:]...)
			} else {
				lines[i] = target
			}
			return writeLines(path, lines)
		}
	}
	if value == "" {
		return nil // nothing to clear
	}
	// Insert: top-level at the end of the preamble, a table right after its header.
	at := start
	if section == "" {
		at = end
	}
	lines = append(lines[:at], append([]string{target}, lines[at:]...)...)
	return writeLines(path, lines)
}

func writeLines(path string, lines []string) error {
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
