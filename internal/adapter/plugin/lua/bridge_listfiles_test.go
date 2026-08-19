package lua

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
)

// A plugin can see WHAT is in a directory it may read.
//
// fs:read let a plugin fetch any file under its grant but never learn what was there, so every
// plugin that needed to know kept its own ledger of what it had written and consulted that instead
// of the directory. A ledger cannot see files a person added, or another process wrote — so the
// plugin acted on a list that was quietly short, and the failure looked like "there is nothing
// there" rather than "I cannot look".
//
// Directories come back with a trailing slash: one call has to distinguish a folder from a file,
// because there is no second call that could.
func TestListFilesShowsWhatIsUnderTheGrant(t *testing.T) {
	dir := writePlugin(t,
		`name="lister"`+"\n"+`capabilities=["tool"]`+"\n"+`permissions=["fs:read:."]`,
		`magi.register_tool{name="lister", execute=function(a)
		   local names, err = magi.list_files(a.dir)
		   if names == nil then return "ERR:" .. tostring(err), true end
		   return table.concat(names, ",")
		 end}`,
	)
	reg := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: reg})
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	wd := t.TempDir()
	os.MkdirAll(filepath.Join(wd, "skills", "run-tests"), 0o755)
	os.WriteFile(filepath.Join(wd, "skills", "loose.md"), []byte("x"), 0o644)

	tool, _ := reg.Get("lister")
	got, isErr := execTool(t, tool, `{"dir":"skills"}`, wd)
	if isErr {
		t.Fatalf("listing a granted directory failed: %s", got)
	}
	if !strings.Contains(got, "run-tests/") {
		t.Errorf("got %q — a directory must be marked, or a caller cannot tell a skill folder from a file", got)
	}
	if !strings.Contains(got, "loose.md") {
		t.Errorf("got %q — the loose file is missing", got)
	}
}

// Outside the grant it says permission denied, NOT an empty list.
//
// The two are different answers and a caller that cannot tell them apart acts on the wrong one:
// "there are no skills yet" leads to writing a new one, which is exactly the duplicate this whole
// mechanism exists to prevent.
func TestListFilesRefusesRatherThanAnsweringEmpty(t *testing.T) {
	dir := writePlugin(t,
		`name="nosy"`+"\n"+`capabilities=["tool"]`+"\n"+`permissions=["fs:read:allowed"]`,
		`magi.register_tool{name="nosy", execute=function(a)
		   local names, err = magi.list_files(a.dir)
		   if names == nil then return "DENIED:" .. tostring(err), true end
		   return "LISTED:" .. table.concat(names, ",")
		 end}`,
	)
	reg := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: reg})
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	wd := t.TempDir()
	os.MkdirAll(filepath.Join(wd, "secrets"), 0o755)
	os.WriteFile(filepath.Join(wd, "secrets", "key.pem"), []byte("x"), 0o644)

	tool, _ := reg.Get("nosy")
	got, isErr := execTool(t, tool, `{"dir":"secrets"}`, wd)
	if !isErr {
		t.Fatalf("a directory outside the grant was listed anyway: %s", got)
	}
	if !strings.Contains(got, "permission denied") {
		t.Errorf("the denial reads %q; it must say it was refused, not that nothing is there", got)
	}
}
