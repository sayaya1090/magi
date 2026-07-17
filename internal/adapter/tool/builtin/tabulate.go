package builtin

import (
	"context"
	"encoding/json"
	"math"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Tabulate aggregates one numeric column of a delimited text file: sum, count, avg,
// min, or max, over the rows that pass an optional numeric filter. It is the tool a
// read-only agent reaches for instead of eyeballing a large table — e.g. the two
// calls that yield Go coverage (sum of column 2, then sum of column 2 where column 3
// > 0; the model divides).
type Tabulate struct{}

type tabFilter struct {
	Column flexInt `json:"column"`
	Op     string  `json:"op"`
	Value  float64 `json:"value"`
}

type tabArgs struct {
	Path       string     `json:"path"`
	Column     flexInt    `json:"column"`
	Op         string     `json:"op"`
	Delimiter  string     `json:"delimiter"`
	Filter     *tabFilter `json:"filter"`
	SkipHeader bool       `json:"skip_header"`
}

func (Tabulate) Name() string { return "tabulate" }
func (Tabulate) Description() string {
	return "Aggregate one numeric column of a delimited text file (sum|count|avg|min|max) over rows passing an optional numeric filter. Pure read-only arithmetic over tabular data (coverage.out, CSV/TSV, logs) — use instead of reading a large file to add up a column by hand."
}
func (Tabulate) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"path":{"type":"string","description":"file to read (workdir-relative)"},` +
		`"column":{"type":"integer","description":"1-indexed column to aggregate; required for every op except count"},` +
		`"op":{"type":"string","enum":["sum","count","avg","min","max"],"description":"aggregation (default sum); count tallies rows passing the filter and ignores column"},` +
		`"delimiter":{"type":"string","description":"column delimiter; omit for whitespace-delimited"},` +
		`"filter":{"type":"object","description":"only rows where this column compares true are counted","properties":{"column":{"type":"integer"},"op":{"type":"string","enum":[">",">=","<","<=","==","!="]},"value":{"type":"number"}},"required":["column","op","value"]},` +
		`"skip_header":{"type":"boolean","description":"skip the first line"}` +
		`},"required":["path"]}`)
}

func (Tabulate) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a tabArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", invalidArgs(err)), nil
	}
	op := a.Op
	if op == "" {
		op = "sum"
	}
	switch op {
	case "sum", "count", "avg", "min", "max":
	default:
		return errResult("", "unknown op: "+op+" (want sum|count|avg|min|max)"), nil
	}
	if op != "count" && a.Column < 1 {
		return errResult("", "column must be a 1-indexed positive integer"), nil
	}
	if a.Filter != nil {
		if a.Filter.Column < 1 {
			return errResult("", "filter.column must be a 1-indexed positive integer"), nil
		}
		if _, ok := cmpOp(a.Filter.Op, 0, 0); !ok {
			return errResult("", "unknown filter op: "+a.Filter.Op), nil
		}
	}

	data, rel, truncated, errMsg := readSingle(env, a.Path)
	if errMsg != "" {
		return errResult("", errMsg), nil
	}

	lines := splitLines(data)
	var (
		total    = len(lines)
		rows     int
		sum      float64
		min      = math.Inf(1)
		max      = math.Inf(-1)
		numCount int // rows contributing a numeric target value (for avg/min/max)
	)
	for i, ln := range lines {
		if a.SkipHeader && i == 0 {
			continue
		}
		cols := fields(ln, a.Delimiter)
		if a.Filter != nil {
			fv, ok := parseFloatCell(cell(cols, int(a.Filter.Column)))
			if !ok {
				continue
			}
			if pass, _ := cmpOp(a.Filter.Op, fv, a.Filter.Value); !pass {
				continue
			}
		}
		rows++ // a row that passed the filter (count reports this)
		v, ok := parseFloatCell(cell(cols, int(a.Column)))
		if !ok {
			continue
		}
		numCount++
		sum += v
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}

	out := map[string]any{"op": op, "rows": rows, "total_lines": total, "file": rel}
	if op != "count" {
		// numeric_rows distinguishes a genuine sum of 0 from a mis-indexed/empty column
		// (0 numeric cells) — the latter would otherwise report result:0 with no signal.
		out["column"] = a.Column
		out["numeric_rows"] = numCount
	}
	if truncated {
		out["truncated"] = true // file exceeded the read cap; result covers only the first bytes
	}
	switch op {
	case "count":
		out["result"] = rows
	case "sum":
		out["result"] = sum
	case "avg":
		if numCount == 0 {
			out["result"] = nil
		} else {
			out["result"] = sum / float64(numCount)
		}
	case "min":
		if numCount == 0 {
			out["result"] = nil
		} else {
			out["result"] = min
		}
	case "max":
		if numCount == 0 {
			out["result"] = nil
		} else {
			out["result"] = max
		}
	}
	return okJSON("", out), nil
}
