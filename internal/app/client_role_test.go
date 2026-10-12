package app

import (
	"context"
	"encoding/json"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplicationRolesExcludeCodingAndOtherApplicationTools(t *testing.T) {
	for _, role := range []string{"figma", "mail", "slack", "powerpoint", "word", "excel"} {
		if !ValidClientRole(role) || !strings.Contains(ClientRoleSystem(role), "not a coding agent") {
			t.Fatalf("missing role contract: %s", role)
		}
		for _, name := range []string{"bash", "write", "read", "edit", "multi_edit", "mcp__ide__edit", "mcp__unrelated__write"} {
			if ClientRoleAllows(role, name) {
				t.Fatalf("%s allowed %s", role, name)
			}
		}
		server := role
		if role == "powerpoint" {
			server = "ppt"
		}
		if role == "excel" {
			server = "xl"
		}
		if !ClientRoleAllows(role, "mcp__"+server+"__read") || !ClientRoleAllows(role, "ask_user") {
			t.Fatalf("%s omitted application tool", role)
		}
	}
	if !ClientRoleAllows("github", "bash") || !ClientRoleAllows("", "edit") || ValidClientRole("unknown") {
		t.Fatal("coding role or validation changed")
	}
}

func TestClientKindCatalogHasUniqueValidRoles(t *testing.T) {
	seen := map[ClientKind]bool{}
	for _, kind := range ClientKinds() {
		if seen[kind] || !kind.Valid() {
			t.Fatalf("invalid catalog entry %q", kind)
		}
		seen[kind] = true
		want := string(kind)
		if kind == ClientCoding {
			want = "1"
		}
		if kind.CompanionType() != want {
			t.Fatalf("wrong console type for %q", kind)
		}
		if kind != ClientCoding && ClientRoleSystem(string(kind)) == "" {
			t.Fatalf("application kind has no prompt: %q", kind)
		}
	}
	if len(seen) != 7 || ClientKind("unknown").Valid() || ClientRoleAllows("unknown", "ask_user") {
		t.Fatal("unknown kind allowed or catalog incomplete")
	}
}

func TestCodingKindIsExplicitAndClientAliasesNormalize(t *testing.T) {
	for _, input := range []string{"", "coding", "github"} {
		kind := NormalizeClientKind(input)
		if kind != ClientCoding || string(kind) != "coding" || !ClientRoleAllows(input, "bash") {
			t.Fatalf("coding input %q did not normalize: %q", input, kind)
		}
		if got := (Config{ClientRole: input}).withDefaults().ClientRole; got != "coding" {
			t.Fatalf("config retained client alias %q", got)
		}
	}
	if ClientKind("").Valid() || ClientKind("github").Valid() {
		t.Fatal("legacy client spellings became canonical kinds")
	}
}

func TestApplicationRoleBlocksExecutionEvenWithPermissionAllow(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{ClientRole: "mail", Permission: "allow"})
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	path := filepath.Join(s.Workdir, "must-not-exist.txt")
	args, _ := json.Marshal(map[string]any{"path": path, "content": "changed"})
	agent := AgentSpec{Name: "default"}
	for _, spec := range a.toolSpecs(sid, agent) {
		if spec.Name == "write" || spec.Name == "bash" {
			t.Fatal("coding tool advertised to mail")
		}
	}
	a.executeTool(ctx, s, agent, 0, event.Actor{Kind: event.ActorAgent, ID: "default"}, &session.ToolCall{CallID: "blocked-write", Name: "write", Args: args}, newRunGuard(nil), "")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("role restriction allowed write")
	}
	if body := resultBody(t, a, sid, "blocked-write"); !strings.Contains(body, "not permitted") {
		t.Fatalf("missing refusal: %s", body)
	}
}
