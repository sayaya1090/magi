package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// One bash command has as many destinations as it has, and the sentence noteEdit returns names
// none of them — it was written for write/edit, whose result describes the single path their own
// call carried. Observed live (cobol-modernization, 2026-07-29): `cp a.bak a && cp b.bak b &&
// cp c.bak c && ./program` came back carrying
//
//	[self-edit check] this write left the file byte-for-byte as it already was — nothing changed.
//
// three times over, identical, with nothing to say which file each was about — or that they were
// about three different files at all rather than one finding rendered thrice.
func TestBashSelfEditNamesTheFileItIsAbout(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	dir := t.TempDir()

	var changes []bashChange
	for _, n := range []string{"ACCOUNTS.DAT", "BOOKS.DAT", "TRANSACTIONS.DAT"} {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte("same bytes for "+n), 0o644); err != nil {
			t.Fatal(err)
		}
		changes = append(changes, bashChange{path: p, before: "same bytes for " + n, readable: true})
	}

	const cmd = "cp ACCOUNTS.DAT.bak ACCOUNTS.DAT && cp BOOKS.DAT.bak BOOKS.DAT && " +
		"cp TRANSACTIONS.DAT.bak TRANSACTIONS.DAT && ./program"
	tc := &session.ToolCall{CallID: "c", Name: "bash",
		Args: json.RawMessage(`{"command":` + strconv.Quote(cmd) + `}`)}
	res := session.ToolResult{CallID: "c", Content: json.RawMessage(`"exit 0"`)}
	a.noteToolOutcome(sid, newRunGuard(), toolOutcome{
		tc: tc, res: &res, workdir: dir, fp: "fp", novel: true, toolOK: true,
		bashChanges: changes,
	})
	got := string(res.Content)

	if n := strings.Count(got, "self-edit check"); n != 3 {
		t.Fatalf("one note per destination left untouched, got %d:\n%s", n, got)
	}
	for _, want := range []string{"ACCOUNTS.DAT", "BOOKS.DAT", "TRANSACTIONS.DAT"} {
		if !strings.Contains(got, want+": this write left the file") {
			t.Errorf("the note must name %s, the path magi already had:\n%s", want, got)
		}
	}
}
