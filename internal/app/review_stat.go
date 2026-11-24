package app

import (
	"encoding/json"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
)

var statRevision = regexp.MustCompile(`^(?:--- |commit )?([0-9a-f]{7,64})(?:\s.*)?$`)
var statFile = regexp.MustCompile(`^ (.+?)\s+\|\s+[0-9]+[ +\-]*$`)
var statTotal = regexp.MustCompile(`^ ([0-9]+) files? changed(?:,|$)`)

// Only pass complete numeric diffstats with an explicit revision to observers.
// Omitted files, a headline announcing HEAD, and the worktree status aren't review targets.
func observedReviewStat(args json.RawMessage, result session.ToolResult) string {
	if result.IsError || result.Background || len(result.Content) > 65536 {
		return ""
	}
	var call struct{ Command string }
	var output string
	if json.Unmarshal(args, &call) != nil || !strings.Contains(call.Command, "git ") || !strings.Contains(call.Command, "--stat") || json.Unmarshal(result.Content, &output) != nil {
		return ""
	}
	var rev string
	var files []string
	for _, line := range strings.Split(output, "\n") {
		if m := statRevision.FindStringSubmatch(line); m != nil {
			rev = m[1]
			files = nil
			continue
		}
		if rev == "" {
			continue
		}
		if m := statFile.FindStringSubmatch(line); m != nil {
			f := strings.TrimSpace(m[1])
			if len(files) >= 32 || len(f) > 256 || path.Clean(f) != f || strings.HasPrefix(f, "/") || f == ".." || strings.HasPrefix(f, "../") || strings.Contains(f, "...") || strings.Contains(f, "=>") || strings.ContainsAny(f, "\x00\r\t*?[]:\\\"") {
				rev = ""
				continue
			}
			files = append(files, f)
			continue
		}
		if m := statTotal.FindStringSubmatch(line); m != nil && len(files) > 0 && strconv.Itoa(len(files)) == m[1] {
			b, _ := json.Marshal(struct {
				Revision string   `json:"revision"`
				Files    []string `json:"files"`
			}{rev, files})
			return string(b)
		}
		if strings.TrimSpace(line) != "" {
			rev = ""
		}
	}
	return ""
}
