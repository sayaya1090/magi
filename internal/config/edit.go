package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// setKeyMu serializes the read-modify-write in SetKey so concurrent writers (plugins via
// magi.set_config_key, the /route editor) can't race and lose updates or corrupt the file.
// It only covers writers in THIS process; withFileLock adds the cross-process half.
var setKeyMu sync.Mutex

// withFileLock serializes the read-modify-write of `target` across SEPARATE processes,
// which setKeyMu cannot: two magi instances sharing one config.toml (e.g. a plugin doing
// a live set_model in each) otherwise both read the same bytes, edit independently, and
// the last WriteFile clobbers the other's change — or, mid-write, the reader parses a torn
// file. An O_EXCL sidecar lock (portable to Windows, unlike flock) makes the RMW atomic
// between processes. On contention it waits briefly then proceeds unlocked rather than
// deadlock a rare config write; a lock left by a crashed process is reclaimed once stale.
func withFileLock(target string, fn func() error) error {
	lock := target + ".lock"
	deadline := time.Now().Add(3 * time.Second)
	for {
		f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			defer os.Remove(lock)
			return fn()
		}
		if !os.IsExist(err) {
			return fn() // can't create the lock for an unrelated reason — don't block the write
		}
		if time.Now().After(deadline) {
			// Reclaim a lock a crashed writer never released; otherwise give up waiting and
			// proceed (setKeyMu still serializes same-process writers).
			if fi, e := os.Stat(lock); e == nil && time.Since(fi.ModTime()) > 15*time.Second {
				os.Remove(lock)
				continue
			}
			return fn()
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// BareName reports whether s is a valid TOML bare key: non-empty, every rune in [A-Za-z0-9_-].
//
// The one gate for every NAME that becomes part of a raw-concatenated table header written by
// SetKey/RemoveSection below ("[llm.profiles.<name>]", "[cron.<name>]", "[mcp.<name>]"): %q escaping
// covers values, never the header, so anything outside the bare-key set either splits the header (a
// newline) or makes it fail to parse (a space, dot, bracket, comma, non-ASCII letter) — and either
// way the whole config.toml stops loading and the daemon's next start fails. An allowlist on
// purpose: this rule lived as per-writer denylists first, and each copy kept missing characters —
// the web writers missed a newline, then a comma; the agent-facing schedule writer and the TUI's
// profile persister were still on the old denylist when the audit swept. It lives HERE, beside the
// functions it protects, so the next writer cannot miss it.
func BareName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// QuotedName reports whether s is safe INSIDE a quoted TOML key — `people."<s>"` — which is the
// header shape for keys that legitimately carry dots and @ signs (an email address) and so cannot
// meet BareName. The quotes contain everything except the three things that end or escape them: a
// double quote, a backslash, and a control character (a newline splits the header; the others make
// the file fail to parse). Same placement rule as BareName: it lives beside the writers it
// protects, so a header writer that quotes its name still has its gate one line away.
func QuotedName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r == '"' || r == '\\' || r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// SetKey surgically sets `key = "value"` under the given TOML table in the file
// at path, preserving the rest of the file (comments, template, other sections).
// section "" targets a top-level key (in the preamble before the first table).
// An empty value removes the key. The file is created if absent.
//
// It is intentionally limited to flat string keys (its writers set single keys
// like `model` or one profile field at a time), so it stays a safe line-level
// edit rather than a full TOML round-trip that would discard comments.
//
// The value is ALWAYS quoted. Handing it a bool or a number writes a string, and
// the file stops parsing the moment something reads that key into a typed field —
// use SetRawKey for those.
func SetKey(path, section, key, value string) error {
	return setKey(path, section, key, value, true)
}

// SetRawKey is SetKey for a value that is not a string: `enabled = true`, not
// `enabled = "true"`. The caller owns the TOML syntax of raw and is responsible
// for it being valid; an empty raw removes the key, exactly as in SetKey.
//
// It exists because SetKey quoting everything is right for the paths that write
// model names and URLs, and silently wrong for a bool — a config written that way
// loads fine until the field it lands in is typed, and then the whole file fails.
func SetRawKey(path, section, key, raw string) error {
	return setKey(path, section, key, raw, false)
}

func setKey(path, section, key, value string, quote bool) error {
	// The file is created if absent, and so is the directory holding it. A workspace has no .magi
	// until something writes one, which is the ordinary state of a project that has never been
	// configured — and the first write to it is exactly this. Without the MkdirAll the console
	// answered a person adding their first scheduled job with a raw errno and a 500, about a
	// directory they had no reason to know was supposed to exist.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	setKeyMu.Lock()
	defer setKeyMu.Unlock()
	return withFileLock(path, func() error { return setKeyLocked(path, section, key, value, quote) })
}

func setKeyLocked(path, section, key, value string, quote bool) error {
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := []string{}
	if len(b) > 0 {
		lines = strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	}

	target := key + " = " + value
	if quote {
		target = fmt.Sprintf("%s = %q", key, value)
	}
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
	return withFileLock(path, func() error { return appendListItemLocked(path, key, value) })
}

func appendListItemLocked(path, key, value string) error {
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

// RemoveSection deletes a whole TOML table and its lines, preserving everything else.
//
// SetKey with an empty value clears one key; deleting a server means clearing every key it might
// have and then the header, and a caller doing that by hand has to know which keys those are —
// which it cannot, since a table it did not write may hold keys this build does not know. So the
// removal is by SHAPE: the header line, everything up to the next table header, and one blank line
// left behind if there was one.
//
// A comment immediately above the header goes with it. A table written by a person usually has its
// explanation on the line above, and leaving that orphaned above an unrelated table is worse than
// losing it — the explanation would then describe something else.
//
// Missing file or missing section is not an error: the caller asked for the section to be gone.
func RemoveSection(path, section string) error {
	setKeyMu.Lock()
	defer setKeyMu.Unlock()
	return withFileLock(path, func() error { return removeSectionLocked(path, section) })
}

func removeSectionLocked(path, section string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	hdr := regexp.MustCompile(`^\s*\[` + regexp.QuoteMeta(section) + `\]\s*$`)
	h := -1
	for i, ln := range lines {
		if hdr.MatchString(ln) {
			h = i
			break
		}
	}
	if h < 0 {
		return nil
	}
	end := len(lines)
	for i := h + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			end = i
			break
		}
	}
	// Trailing blank lines belong to the gap between tables, not to this one — taking them all
	// would close up the spacing of the file every time something is removed.
	for end > h+1 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	start := h
	for start > 0 && strings.HasPrefix(strings.TrimSpace(lines[start-1]), "#") {
		start--
	}
	out := append(append([]string{}, lines[:start]...), lines[end:]...)
	// One blank line where the table was, and never two: the file is read by people.
	for len(out) > start && start > 0 && strings.TrimSpace(out[start-1]) == "" &&
		len(out) > start && strings.TrimSpace(out[start]) == "" {
		out = append(out[:start], out[start+1:]...)
	}
	return writeLines(path, out)
}
