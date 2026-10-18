package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	expgit "github.com/sayaya1090/magi/internal/adapter/experience/git"
	"github.com/sayaya1090/magi/internal/port"
)

func TestLegacySkillImportPreservesSourceAndRejectsStaleReview(t *testing.T) {
	t.Setenv("MAGI_CONFIG_DIR", t.TempDir())
	a, _, wd := newWorkflowApp(t, nil, nil, Config{})
	a.cfg.Experience = expgit.New(t.TempDir())
	dir := filepath.Join(wd, ".claude", "skills", "handmade")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "SKILL.md")
	original := []byte("---\nname: handmade\n---\nHuman procedure with a local verifier.")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	list, err := a.ManageProjectKnowledge(ctx, wd, port.KnowledgeAction{})
	if err != nil || len(list) != 1 || !list[0].Importable {
		t.Fatal(list, err)
	}
	action := port.KnowledgeAction{Name: list[0].Name, Revision: "stale", Action: "import"}
	if _, err := a.ManageProjectKnowledge(ctx, wd, action); err == nil {
		t.Fatal("stale review accepted")
	}
	action.Revision = list[0].Revision
	list, err = a.ManageProjectKnowledge(ctx, wd, action)
	if err != nil || len(list) != 1 || !list[0].Managed || !list[0].Pinned || list[0].ApplicationCount != 0 {
		t.Fatal(list, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != string(original) {
		t.Fatal("source modified", err)
	}
	if _, err := a.ManageProjectKnowledge(ctx, wd, port.KnowledgeAction{Name: list[0].Name, Revision: list[0].Revision, Action: "unpin"}); err != nil {
		t.Fatal(err)
	}
	retry, err := a.ManageProjectKnowledge(ctx, wd, action)
	if err != nil || len(retry) != 1 || retry[0].Pinned {
		t.Fatal("retry changed user pin", retry, err)
	}
	if err := os.WriteFile(path, []byte("human correction"), 0644); err != nil {
		t.Fatal(err)
	}
	action.Revision = digestString("human correction")
	if _, err := a.ManageProjectKnowledge(ctx, wd, action); err == nil {
		t.Fatal("silently overwrote existing managed copy")
	}
	if _, err := a.ManageProjectKnowledge(ctx, wd, port.KnowledgeAction{Name: "legacy:../outside", Revision: action.Revision, Action: "import"}); err == nil {
		t.Fatal("path traversal accepted")
	}
}

func TestLegacySkillImportRejectsWorkspaceEscape(t *testing.T) {
	wd := t.TempDir()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "SKILL.md"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wd, ".claude", "skills"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(wd, ".claude", "skills", "escape")); err != nil {
		t.Skip(err)
	}
	if _, err := legacySkillDocument(wd, "escape"); err == nil {
		t.Fatal("read external skill through symlink")
	}
}
