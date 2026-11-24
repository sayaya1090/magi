package app

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"mvdan.cc/sh/v3/syntax"
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

// For a simple producer invocation, operands following a literal -- may change.
// Preserve the executable, options, subcommand and other Bash settings. A
// compound script or expansion keeps its exact request as the identity.
func producerSignature(tool string, raw json.RawMessage) string {
	var args map[string]json.RawMessage
	if json.Unmarshal(raw, &args) != nil {
		return string(raw)
	}
	if tool == "bash" {
		var command string
		if json.Unmarshal(args["command"], &command) == nil && strings.Contains(command, "--") && len(command) <= 65536 {
			file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
			if err == nil && len(file.Stmts) == 1 {
				stmt := file.Stmts[0]
				call, ok := stmt.Cmd.(*syntax.CallExpr)
				if ok && !stmt.Negated && !stmt.Background && !stmt.Coprocess && !stmt.Disown && len(stmt.Redirs) == 0 && len(call.Assigns) == 0 {
					separator := -1
					for i, word := range call.Args {
						literal := word.Lit()
						if literal == "" || strings.ContainsAny(literal, "*?[{~") {
							separator = -1
							break
						}
						if i > 0 && literal == "--" && separator < 0 {
							separator = i
						}
					}
					if separator >= 0 && separator < len(call.Args)-1 {
						// Encode a structural prefix as an array, distinct from an exact command string.
						prefix := make([]string, separator+1)
						for i := range prefix {
							prefix[i] = call.Args[i].Lit()
						}
						args["command"], _ = json.Marshal(prefix)
					}
				}
			}
		}
	}
	b, _ := json.Marshal(args)
	return string(b)
}

// Normalize a complete inventory. Field names are retained so subsequent calls
// can bind to values without copying the observed operands into a routine.
func resultInventory(tool string, args json.RawMessage, res session.ToolResult) string {
	if res.IsError || res.Background || len(res.Content) > 65536 {
		return ""
	}
	raw := res.Content
	if tool == "bash" {
		if stat := observedReviewStat(args, res); stat != "" {
			raw = []byte(stat)
		} else {
			var text string
			if json.Unmarshal(raw, &text) != nil {
				return ""
			}
			body, ok := strings.CutPrefix(text, "exit 0\n")
			if !ok {
				return ""
			}
			if strings.HasPrefix(body, "output: ") {
				line, rest, found := strings.Cut(body, "\n")
				if !found || !strings.HasSuffix(line, "bytes — all of it is above)") {
					return ""
				}
				body = rest
			}
			raw = []byte(body)
		}
	}
	var record map[string]json.RawMessage
	if json.Unmarshal(raw, &record) != nil {
		record = map[string]json.RawMessage{"items": raw}
	}
	if len(record) > 16 {
		return ""
	}
	var items []string
	fields := map[string]string{}
	schema := map[string]bool{}
	for key, value := range record {
		if len(key) == 0 || len(key) > 64 {
			return ""
		}
		schema[key] = false
		var scalar string
		if json.Unmarshal(value, &scalar) == nil && scalar != "" && len(scalar) <= 512 {
			fields[key] = scalar
			continue
		}
		var list []string
		if items != nil || json.Unmarshal(value, &list) != nil || len(list) == 0 || len(list) > 32 {
			return ""
		}
		seen := map[string]bool{}
		for _, v := range list {
			if v == "" || len(v) > 512 || seen[v] || strings.ContainsRune(v, 0) {
				return ""
			}
			seen[v] = true
		}
		items = list
		schema[key] = true
	}
	if items == nil {
		return ""
	}
	shape, _ := json.Marshal(schema)
	source := fmt.Sprintf("%x", sha256.Sum256([]byte(tool+"\x00"+producerSignature(tool, args)+"\x00"+string(shape))))
	b, _ := json.Marshal(map[string]any{"source": source, "items": items, "fields": fields})
	return string(b)
}

func inventoryIdentity(workdir, inventory string) string {
	if inventory == "" {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(workdir+"\x00"+inventory)))
}

// Observers need only correlate literal operands. Keep the raw inventory
// and its scalar fields inside the planner.
func inventoryItemDigests(workdir, inventory string) string {
	var v struct{ Items []string }
	if inventory == "" || json.Unmarshal([]byte(inventory), &v) != nil || len(v.Items) == 0 {
		return ""
	}
	for i, item := range v.Items {
		v.Items[i] = fmt.Sprintf("%x", sha256.Sum256([]byte(workdir+"\x00"+item)))
	}
	raw, _ := json.Marshal(v.Items)
	return string(raw)
}

type inventoryRoutinePlanner interface {
	InventoryRoutine(workdir, inventory, consumer string) string
}

func appendInventoryResult(content json.RawMessage, planner inventoryRoutinePlanner, guard *runGuard, workdir, inventory string, run func(json.RawMessage) (session.ToolResult, error)) json.RawMessage {
	plan := planner.InventoryRoutine(workdir, inventory, "")
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
	return appendToContent(content, "\n\n[inventory routine] "+output)
}
