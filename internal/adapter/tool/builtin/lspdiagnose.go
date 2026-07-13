package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Diagnostics for every language the LSP client knows (serverFor), via the
// server-pushed textDocument/publishDiagnostics notification that arrives after
// didOpen. Go keeps its dedicated gopls-CLI path (lspdiag.go); this covers the
// rest — Python, C/C++, Rust, TypeScript/JS, and the long tail — so the agent (and
// through its tool result, the council) gets a real correctness signal on a file
// in any of the benchmark languages. Best-effort: a missing server binary or a
// slow/silent server degrades to "no diagnostics" rather than stalling the turn.

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

// sameURI compares two file URIs tolerant of trailing-slash and host-empty forms
// ("file:///a" vs "file://a") that different servers emit.
func sameURI(a, b string) bool {
	norm := func(u string) string {
		u = strings.TrimPrefix(u, "file://")
		return strings.TrimPrefix(u, "/")
	}
	return norm(a) == norm(b)
}

// collectDiagnostics reads server messages after didOpen and returns the last
// diagnostics set published for uri. It returns as soon as a non-empty set
// arrives; if only an empty set (a clean file, or a not-yet-analyzed one) arrives
// it waits a short quiet period for a follow-up before giving up, and it caps the
// whole wait at `overall`. A background reader lets us honor those timers even
// though readMsg itself blocks; ctx cancellation (deferred by the caller) frees it.
func (c *lspClient) collectDiagnostics(ctx context.Context, uri string, overall, quiet time.Duration) []lspDiagnostic {
	type msg struct {
		m   map[string]json.RawMessage
		err error
	}
	ch := make(chan msg)
	go func() {
		for {
			m, err := c.readMsg()
			select {
			case ch <- msg{m, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	var latest []lspDiagnostic
	overallT := time.After(overall)
	var quietT <-chan time.Time
	for {
		select {
		case <-overallT:
			return latest
		case <-quietT:
			return latest
		case mm := <-ch:
			if mm.err != nil {
				return latest
			}
			m := mm.m
			method, hasMethod := m["method"]
			idRaw, hasID := m["id"]
			switch {
			case hasID && hasMethod: // server→client request — reply null so it doesn't block
				_ = c.writeMsg(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(idRaw), "result": nil})
			case hasMethod:
				var meth string
				if json.Unmarshal(method, &meth) == nil && meth == "textDocument/publishDiagnostics" {
					var p publishDiagParams
					if json.Unmarshal(m["params"], &p) == nil && sameURI(p.URI, uri) {
						latest = p.Diagnostics
						if len(latest) > 0 {
							return latest // real diagnostics — done
						}
						quietT = time.After(quiet) // empty set: wait briefly for a populated follow-up
					}
				}
			}
		}
	}
}

// lspDiagnose starts the language server for absPath, opens the file, and returns
// the diagnostics it publishes. Non-Go only (Go uses the gopls path); an
// unsupported extension or a missing server binary returns an error the caller
// treats as "no diagnostics available".
func lspDiagnose(ctx context.Context, workdir, absPath string) ([]lspDiagnostic, error) {
	srv, ok := serverFor(absPath)
	if !ok {
		return nil, fmt.Errorf("no LSP server configured for %s", filepath.Ext(absPath))
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	c, err := startLSP(ctx, srv, workdir)
	if err != nil {
		return nil, err
	}
	defer c.close()

	if _, err := c.call("initialize", map[string]any{
		"processId":    nil,
		"rootUri":      "file://" + workdir,
		"capabilities": map[string]any{"textDocument": map[string]any{"publishDiagnostics": map[string]any{}}},
	}); err != nil {
		return nil, err
	}
	_ = c.notify("initialized", map[string]any{})

	uri := "file://" + absPath
	data, _ := os.ReadFile(absPath)
	_ = c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{"uri": uri, "languageId": srv.langID, "version": 1, "text": string(data)},
	})
	return c.collectDiagnostics(ctx, uri, 8*time.Second, 800*time.Millisecond), nil
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
