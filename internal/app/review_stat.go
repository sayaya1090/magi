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
	var complete string
	for _, line := range strings.Split(output, "\n") {
		// The caller already fetched a patch.
		if strings.HasPrefix(line, "diff --git ") {
			return ""
		}
		if m := statRevision.FindStringSubmatch(line); m != nil {
			if complete != "" {
				return ""
			}
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
			complete = string(b)
			rev = ""
		}
		if strings.TrimSpace(line) != "" {
			rev = ""
		}
	}
	return complete
}

type reviewRoutinePlanner interface {
	ReviewRoutine(workdir, request, inventory string) string
}

func appendReviewDiff(content json.RawMessage, planner reviewRoutinePlanner, guard *runGuard, workdir, request, inventory string, run func(json.RawMessage) (session.ToolResult, error)) json.RawMessage {
	plan := planner.ReviewRoutine(workdir, request, inventory)
	if plan == "" || !json.Valid([]byte(plan)) {
		return content
	}
	if _, seen := guard.reviewedInventories.LoadOrStore(inventory, true); seen {
		return content
	}
	result, err := run(json.RawMessage(plan))
	var output string
	if err != nil {
		output = err.Error()
	} else if json.Unmarshal(result.Content, &output) != nil {
		output = string(result.Content)
	}
	return appendToContent(content, "\n\n[review routine] "+output)
}
