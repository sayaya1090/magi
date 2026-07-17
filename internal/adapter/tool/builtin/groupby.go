package builtin

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// GroupBy tallies rows of a delimited file by a key — a column value or the first
// capture group of a regex — producing per-group count or sum(value_column). It
// answers distribution questions (status-code counts in a log, rows per category)
// that need reduction, not reading.
type GroupBy struct{}

type groupByArgs struct {
	Path        string  `json:"path"`
	KeyColumn   flexInt `json:"key_column"`
	KeyPattern  string  `json:"key_pattern"`
	ValueColumn flexInt `json:"value_column"`
	Op          string  `json:"op"`
	Delimiter   string  `json:"delimiter"`
	SkipHeader  bool    `json:"skip_header"`
	Top         flexInt `json:"top"`
}

func (GroupBy) Name() string { return "groupby" }
func (GroupBy) Description() string {
	return "Group rows of a delimited file by a column value or a regex capture group, and report per-group count or sum(value_column), sorted by value. Pure read-only reduction for distribution/tally questions."
}
func (GroupBy) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"path":{"type":"string","description":"file to read (workdir-relative)"},` +
		`"key_column":{"type":"integer","description":"1-indexed column to group by (use this or key_pattern)"},` +
		`"key_pattern":{"type":"string","description":"regex whose first capture group is the key (use this or key_column)"},` +
		`"value_column":{"type":"integer","description":"1-indexed numeric column to sum when op=sum"},` +
		`"op":{"type":"string","enum":["count","sum"],"description":"count rows (default) or sum value_column per group"},` +
		`"delimiter":{"type":"string","description":"column delimiter; omit for whitespace-delimited"},` +
		`"skip_header":{"type":"boolean"},` +
		`"top":{"type":"integer","description":"max groups to return, by descending value (default 100)"}` +
		`},"required":["path"]}`)
}

func (GroupBy) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a groupByArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", invalidArgs(err)), nil
	}
	if a.KeyColumn < 1 && strings.TrimSpace(a.KeyPattern) == "" {
		return errResult("", "one of key_column or key_pattern is required"), nil
	}
	if a.KeyColumn >= 1 && strings.TrimSpace(a.KeyPattern) != "" {
		return errResult("", "use only one of key_column or key_pattern"), nil
	}
	op := a.Op
	if op == "" {
		op = "count"
	}
	if op != "count" && op != "sum" {
		return errResult("", "unknown op: "+op+" (want count|sum)"), nil
	}
	if op == "sum" && a.ValueColumn < 1 {
		return errResult("", "op=sum requires a 1-indexed value_column"), nil
	}
	var re *regexp.Regexp
	normUni := false
	if a.KeyPattern != "" {
		pat := a.KeyPattern
		if !isASCIIOnly(pat) {
			normUni = true
			pat = norm.NFC.String(pat)
		}
		var err error
		re, err = regexp.Compile(pat)
		if err != nil {
			return errResult("", "invalid key_pattern: "+err.Error()), nil
		}
		if re.NumSubexp() < 1 {
			return errResult("", "key_pattern must have a capture group, e.g. (\\d+)"), nil
		}
	}
	top := a.Top
	if top <= 0 {
		top = 100
	}

	data, rel, truncated, errMsg := readSingle(env, a.Path)
	if errMsg != "" {
		return errResult("", errMsg), nil
	}

	agg := map[string]float64{}
	order := []string{} // first-seen order, for a stable tie-break
	matched := 0
	for i, ln := range splitLines(data) {
		if a.SkipHeader && i == 0 {
			continue
		}
		var key string
		if re != nil {
			hay := ln
			if normUni {
				hay = norm.NFC.String(hay)
			}
			m := re.FindStringSubmatch(hay)
			if m == nil || m[1] == "" {
				continue
			}
			key = m[1]
		} else {
			cols := fields(ln, a.Delimiter)
			key = cell(cols, int(a.KeyColumn))
			if key == "" {
				continue
			}
		}
		var inc float64 = 1
		if op == "sum" {
			cols := fields(ln, a.Delimiter)
			v, ok := parseFloatCell(cell(cols, int(a.ValueColumn)))
			if !ok {
				continue
			}
			inc = v
		}
		if _, seen := agg[key]; !seen {
			order = append(order, key)
		}
		agg[key] += inc
		matched++
	}

	type kv struct {
		Key   string  `json:"key"`
		Value float64 `json:"value"`
	}
	pos := make(map[string]int, len(order))
	for i, k := range order {
		pos[k] = i
	}
	groups := make([]kv, 0, len(agg))
	for k, v := range agg {
		groups = append(groups, kv{k, v})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Value != groups[j].Value {
			return groups[i].Value > groups[j].Value // descending by value
		}
		return pos[groups[i].Key] < pos[groups[j].Key] // stable: first-seen wins ties
	})
	groupsTruncated := false
	if len(groups) > int(top) {
		groups = groups[:int(top)]
		groupsTruncated = true
	}
	out := map[string]any{
		"op": op, "groups": groups, "distinct": len(agg), "rows_matched": matched,
		"groups_truncated": groupsTruncated, "file": rel,
	}
	if truncated {
		out["truncated"] = true // file exceeded the read cap; tallies cover only the first bytes
	}
	return okJSON("", out), nil
}
