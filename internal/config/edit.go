package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
)

// setKeyMu serializes the read-modify-write in SetKey so concurrent writers (plugins via
// magi.set_config_key, the /route editor) can't race and lose updates or corrupt the file.
var setKeyMu sync.Mutex

// SetKey surgically sets `key = "value"` under the given TOML table in the file
// at path, preserving the rest of the file (comments, template, other sections).
// section "" targets a top-level key (in the preamble before the first table).
// An empty value removes the key. The file is created if absent.
//
// It is intentionally limited to flat string keys (the /route editor only writes
// `model` and the `[routing]` table), so it stays a safe line-level edit rather
// than a full TOML round-trip that would discard comments.
func SetKey(path, section, key, value string) error {
	setKeyMu.Lock()
	defer setKeyMu.Unlock()
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

	// Find the key line to act on, preferring an ACTIVE (uncommented) line over a
	// commented template line. keyRe matches both `key =` and `# key =` (so we can
	// uncomment a template default), but activating a comment while an active line
	// already exists would produce a DUPLICATE top-level key — which makes the whole
	// file fail TOML parse. So only activate a comment when nothing active matches.
	active, commented := -1, -1
	for i := start; i < end; i++ {
		if !keyRe.MatchString(lines[i]) {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
			if commented < 0 {
				commented = i
			}
		} else if active < 0 {
			active = i
		}
	}
	if value == "" {
		// Clearing: remove the active line if there is one; a bare template comment
		// is already inert, so leave it be.
		if active >= 0 {
			lines = append(lines[:active], lines[active+1:]...)
			return writeLines(path, lines)
		}
		return nil // nothing active to clear
	}
	if idx := active; idx >= 0 || commented >= 0 {
		if idx < 0 {
			idx = commented
		}
		lines[idx] = target
		return writeLines(path, lines)
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

// AppendListItem appends value to the single-line string array `key = [...]` in
// the top-level preamble of the TOML file at path, preserving everything else.
// The key is created (`key = ["value"]`) when absent, and the append is a no-op
// when the value is already present. Written for the permission persister
// ("always allow for this project" → the project config's allow rules); like
// SetKey it is a deliberate line-level edit, limited to single-line arrays —
// a hand-formatted multi-line array is left alone with an error rather than
// risk mangling it.
func AppendListItem(path, key, value string) error {
	setKeyMu.Lock()
	defer setKeyMu.Unlock()
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := []string{}
	if len(b) > 0 {
		lines = strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	}
	isTable := func(s string) bool { return strings.HasPrefix(strings.TrimSpace(s), "[") }
	end := len(lines)
	for i, ln := range lines {
		if isTable(ln) {
			end = i
			break
		}
	}
	keyRe := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=\s*\[(.*)$`)
	for i := 0; i < end; i++ {
		m := keyRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		rest := strings.TrimSpace(m[1])
		if !strings.HasSuffix(rest, "]") {
			return fmt.Errorf("config %s: %s is a multi-line array; add %q by hand", path, key, value)
		}
		if strings.Contains(lines[i], fmt.Sprintf("%q", value)) {
			return nil // already present
		}
		inner := strings.TrimSuffix(rest, "]")
		inner = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(inner), ","))
		item := fmt.Sprintf("%q", value)
		if inner != "" {
			item = inner + ", " + item
		}
		lines[i] = lines[i][:strings.IndexByte(lines[i], '[')+1] + item + "]"
		return writeLines(path, lines)
	}
	// Absent: create in the preamble.
	target := fmt.Sprintf("%s = [%q]", key, value)
	lines = append(lines[:end], append([]string{target}, lines[end:]...)...)
	return writeLines(path, lines)
}
