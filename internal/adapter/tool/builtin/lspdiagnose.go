package builtin

import (
	"fmt"
	"sort"
	"strings"
)

// Diagnostic shaping shared by the LSP diagnostics paths. The warm pool
// (lsppool.go) owns the live server connection and reader; here we keep only the
// wire types and the formatting that turns a published diagnostics set into the
// agent-facing (and, through the tool result, council-facing) correctness signal.

type lspDiagnostic struct {
	Range    lspRng `json:"range"`
	Severity int    `json:"severity"` // 1=error 2=warning 3=info 4=hint
	Message  string `json:"message"`
	Source   string `json:"source"`
}

type publishDiagParams struct {
	URI         string          `json:"uri"`
	Diagnostics []lspDiagnostic `json:"diagnostics"`
}

// severityLabel maps the LSP DiagnosticSeverity enum to a short word.
func severityLabel(s int) string {
	switch s {
	case 1:
		return "error"
	case 2:
		return "warning"
	case 3:
		return "info"
	case 4:
		return "hint"
	default:
		return "note"
	}
}

// formatDiagnostics renders diagnostics as "path:line:col: severity: message"
// lines, errors and warnings first, capped so a flood can't drown the result. It
// returns "" when there is nothing worth surfacing (no errors or warnings).
func formatDiagnostics(diags []lspDiagnostic, relPath string) string {
	// Keep only errors and warnings — info/hint are noise for a correctness signal.
	var keep []lspDiagnostic
	for _, d := range diags {
		if d.Severity == 0 || d.Severity == 1 || d.Severity == 2 {
			keep = append(keep, d)
		}
	}
	if len(keep) == 0 {
		return ""
	}
	sort.SliceStable(keep, func(i, j int) bool {
		si, sj := keep[i].Severity, keep[j].Severity
		if si == 0 {
			si = 1
		}
		if sj == 0 {
			sj = 1
		}
		if si != sj {
			return si < sj // errors (1) before warnings (2)
		}
		return keep[i].Range.Start.Line < keep[j].Range.Start.Line
	})
	const maxShown = 10
	total := len(keep)
	if len(keep) > maxShown {
		keep = keep[:maxShown]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d diagnostic(s):", total)
	for _, d := range keep {
		msg := strings.TrimSpace(strings.ReplaceAll(d.Message, "\n", " "))
		fmt.Fprintf(&b, "\n  %s:%d:%d: %s: %s", relPath, d.Range.Start.Line+1, d.Range.Start.Char+1, severityLabel(d.Severity), clipRef(msg, 200))
	}
	if total > maxShown {
		fmt.Fprintf(&b, "\n  … and %d more", total-maxShown)
	}
	return b.String()
}
