package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// maxReadBytes bounds how much of a file read pulls into memory in one call, so a
// giant file (a multi-GB log, a generated blob) can't OOM the process or flood the
// model context. Beyond it, only the first maxReadBytes are read, with a note.
const maxReadBytes = 10 << 20 // 10 MiB

// defaultReadLines caps a read that supplies no explicit limit, so a bare
// read{path} of a large text file returns a navigable window instead of the whole
// file. The agent pages further with offset/limit. Matches the convention of
// mainstream agent read tools.
const defaultReadLines = 2000

// Read returns the contents of a file, optionally a line range. (F-TOOL-READ)
type Read struct{}

type readArgs struct {
	Path   string  `json:"path"`
	Offset flexInt `json:"offset"` // 1-based start line (optional); tolerant parse (flexInt)
	Limit  flexInt `json:"limit"`  // max lines (optional); tolerant parse (flexInt)
}

func (Read) Name() string        { return "read" }
func (Read) Description() string { return "Read the contents of a file, optionally a line range." }
func (Read) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer"},"limit":{"type":"integer"}},"required":["path"]}`)
}

func (Read) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a readArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if strings.TrimSpace(a.Path) == "" {
		return errResult("", "path is required"), nil
	}
	abs, err := resolvePath(env.Workdir, a.Path)
	if err != nil {
		return errResult("", err.Error()), nil
	}
	locatedNote := ""
	info, err := os.Stat(abs)
	if err != nil {
		if !os.IsNotExist(err) {
			return errResult("", err.Error()), nil
		}
		// Recover from an imprecise path: if a same-named file exists elsewhere in
		// the tree, read it (when unique) or suggest the candidates, instead of
		// dead-ending — weak models otherwise give up or bounce the task back.
		located, rel, suggest := resolveOrSuggest(env.Workdir, a.Path)
		if located == "" {
			if suggest != "" {
				return errResult("", "file not found: "+a.Path+" — "+suggest), nil
			}
			return errResult("", "file not found: "+a.Path), nil
		}
		abs = located
		locatedNote = "(note: \"" + a.Path + "\" not found; reading \"" + rel + "\" instead)\n"
		if info, err = os.Stat(abs); err != nil {
			return errResult("", "file not found: "+a.Path), nil
		}
	}
	if info.IsDir() {
		return errResult("", "is a directory: "+a.Path), nil
	}
	data, bytesTruncated, err := readCapped(abs, maxReadBytes)
	if err != nil {
		return errResult("", err.Error()), nil
	}
	if isBinary(data) {
		return errResult("", "binary file: "+a.Path), nil
	}

	start := int(a.Offset)
	if start < 1 {
		start = 1
	}
	// Apply a default line window when the caller gives no explicit limit, so a bare
	// read of a large file returns a navigable page instead of dumping everything.
	limit, defaulted := int(a.Limit), false
	if limit <= 0 {
		limit, defaulted = defaultReadLines, true
	}
	full := string(data)
	content := sliceLines(full, int(a.Offset), limit)
	total := countLines(full)

	body := locatedNote
	if bytesTruncated {
		body += fmt.Sprintf("(note: file larger than %d MiB; showing first %d MiB — use offset/limit to page)\n", maxReadBytes>>20, maxReadBytes>>20)
	}
	// An offset past the end reads as empty; say so explicitly so the model can tell
	// it over-paged rather than mistaking an out-of-range window for an empty file.
	if content == "" && start > 1 && start > total {
		body += fmt.Sprintf("(note: offset %d is past end of file; file has %d lines)\n", start, total)
	}
	// Prefix each line with "N\t" — its 1-based number and a tab (cat -n style) — so
	// the model can reference a line accurately and anchor an edit to it (the edit
	// tool's `at` field). The tab gutter reads as metadata, not as file content.
	body += numberLines(content, start)
	// If the default window (not an explicit limit) hid trailing lines, say so and
	// where to resume — otherwise the model can't tell the file was clipped.
	if defaulted && !bytesTruncated {
		shown := (start - 1) + countLines(content)
		if shown < total {
			body += fmt.Sprintf("\n…(%d more lines — read with offset=%d to continue)", total-shown, shown+1)
		}
	}
	return okText("", body), nil
}

// readCapped reads up to cap bytes of the file, reporting whether it was longer
// (so the caller can note the truncation). Bounds memory for pathologically large
// files instead of os.ReadFile's read-it-all.
func readCapped(path string, cap int64) (data []byte, truncated bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	data, err = io.ReadAll(io.LimitReader(f, cap+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > cap {
		return data[:cap], true, nil
	}
	return data, false, nil
}

// countLines counts the lines in s (a final line without a trailing newline still
// counts). Empty string is zero lines.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// numberLines prefixes each line with "N\t" — its 1-based number and a tab (see
// hashline.go) — starting at firstLine, so the model can quote a line back as an
// edit anchor.
func numberLines(s string, firstLine int) string {
	if s == "" {
		return s
	}
	hadTrailingNL := strings.HasSuffix(s, "\n")
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(formatNumberedLine(firstLine+i, line))
	}
	if hadTrailingNL {
		b.WriteByte('\n')
	}
	return b.String()
}

// sliceLines returns lines [offset, offset+limit) (1-based), preserving the
// trailing newline of each kept line.
func sliceLines(s string, offset, limit int) string {
	if offset < 1 {
		offset = 1
	}
	lines := strings.SplitAfter(s, "\n")
	// SplitAfter on "a\nb\n" yields ["a\n","b\n",""]; drop the trailing empty.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	start := offset - 1
	if start >= len(lines) {
		return ""
	}
	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	return strings.Join(lines[start:end], "")
}
