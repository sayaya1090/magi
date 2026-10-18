package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	expgit "github.com/sayaya1090/magi/internal/adapter/experience/git"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

func activityFixture(t *testing.T, script string) (*App, session.Session, *runGuard, *expgit.Store, string, string) {
	t.Helper()
	t.Setenv("MAGI_CONFIG_DIR", t.TempDir())
	a, sid, wd := newWorkflowApp(t, nil, nil, Config{Permission: "allow"})
	dir := filepath.Join(wd, ".claude", "skills", "sample")
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0755); err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(dir, "SKILL.md")
	verify := filepath.Join(dir, "scripts", "verify.sh")
	if err := os.WriteFile(doc, []byte("---\nname: sample\ndescription: Test skill\n---\nRun the sample procedure."), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(verify, []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	st := expgit.New(t.TempDir())
	c := port.ExperienceChange{ObjectID: "sample-object", OperationID: "publish", ObservationID: "created", Owner: "plugin:engram", Kind: "skill", State: "active", HostOutcome: "verified", Skill: &port.Skill{Name: "sample", Body: "Run the sample procedure.", DocumentDigest: fileDigest(doc), VerificationDigest: fileDigest(verify)}}
	if _, err := st.ApplyExperience(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	a.cfg.Experience = st
	g := newRunGuard(a.touchesFile)
	g.experience = a.newTurnExperience(context.Background(), wd)
	t.Cleanup(g.experience.close)
	return a, a.sessionInfo(context.Background(), sid), g, st, doc, verify
}
func activityTool(t *testing.T, a *App, s session.Session, g *runGuard, id, name string, args any) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	a.executeTool(context.Background(), s, AgentSpec{Name: "coder"}, 0, event.Actor{Kind: event.ActorAgent, ID: "coder"}, &session.ToolCall{CallID: id, Name: name, Args: raw}, g, "")
}
func activityStatus(t *testing.T, st *expgit.Store) expgit.LocalRetention {
	t.Helper()
	inv, err := st.Inventory(context.Background())
	if err != nil || len(inv) != 1 || inv[0].Retention == nil {
		t.Fatalf("inventory: %v %v", inv, err)
	}
	return *inv[0].Retention
}
func TestSkillApplicationRequiresRealForegroundVerifierExecution(t *testing.T) {
	a, s, g, st, _, verify := activityFixture(t, "printf 'verified\\n'\n")
	activityTool(t, a, s, g, "load", "skill", map[string]any{"name": "sample"})
	if r := activityStatus(t, st); !r.Protected || r.ApplicationCount != 0 || !r.LastApplied.IsZero() {
		t.Fatal("load treated as application", r)
	}
	command := "bash -- " + shellQuoted(verify)
	activityTool(t, a, s, g, "masked", "bash", map[string]any{"command": command + " || true"})
	if r := activityStatus(t, st); r.ApplicationCount != 0 {
		t.Fatal("wrapped command renewed lifetime", r)
	}
	activityTool(t, a, s, g, "verification", "bash", map[string]any{"command": command})
	if r := activityStatus(t, st); r.ApplicationCount != 1 || r.LastApplied.IsZero() {
		t.Fatal("actual successful verification not recorded", r)
	}
	g.experience.close()
	if r := activityStatus(t, st); r.Protected {
		t.Fatal("completed turn still protected", r)
	}
}
func TestFailedOrDeniedVerificationDoesNotRenew(t *testing.T) {
	for _, mode := range []string{"exit-failure", "permission-denied", "modified-script", "modified-document"} {
		t.Run(mode, func(t *testing.T) {
			script := "exit 0\n"
			if mode == "exit-failure" {
				script = "exit 7\n"
			}
			a, s, g, st, doc, verify := activityFixture(t, script)
			activityTool(t, a, s, g, "load", "skill", map[string]any{"name": "sample"})
			if mode == "permission-denied" {
				a.SetPermission("deny")
			}
			if mode == "modified-script" {
				if err := os.WriteFile(verify, []byte("echo edited\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "modified-document" {
				if err := os.WriteFile(doc, []byte("human changed the procedure"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			activityTool(t, a, s, g, "verify", "bash", map[string]any{"command": "bash -- " + shellQuoted(verify)})
			if r := activityStatus(t, st); r.ApplicationCount != 0 || !r.LastApplied.IsZero() {
				t.Fatal("unproven application recorded", r)
			}
		})
	}
}
func TestBackgroundReferencesOutliveForegroundWithoutBlockingIt(t *testing.T) {
	a, s, g, st, _, _ := activityFixture(t, "exit 0\n")
	activityTool(t, a, s, g, "load", "skill", map[string]any{"name": "sample"})
	done := g.experience.background()
	g.experience.close()
	if r := activityStatus(t, st); !r.Protected {
		t.Fatal("background reference lost when foreground ended", r)
	}
	done()
	done() // completion retry is harmless
	if r := activityStatus(t, st); r.Protected {
		t.Fatal("background reference not released", r)
	}
}
func TestUnpersistedResultCannotBecomeUsageEvidence(t *testing.T) {
	a, s, g, st, _, verify := activityFixture(t, "exit 0\n")
	activityTool(t, a, s, g, "load", "skill", map[string]any{"name": "sample"})
	tc := &session.ToolCall{CallID: "fake", Name: "bash", Args: json.RawMessage(`{"command":` + strconvQuote("bash -- "+shellQuoted(verify)) + `}`)}
	res := session.ToolResult{CallID: "fake", Content: json.RawMessage(`"exit 0\n"`)}
	if a.persistedVerification(context.Background(), s.ID, "missing", res) {
		t.Fatal("missing result reported as persisted")
	}
	if len(g.experience.before(tc)) != 1 {
		t.Fatal("test did not match verifier")
	}
	if r := activityStatus(t, st); r.ApplicationCount != 0 {
		t.Fatal(r)
	}
}
func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }
func TestApplicationContractDoesNotMislabelAPlainLoad(t *testing.T) {
	a, _, g, _, _, _ := activityFixture(t, "exit 0\n")
	body, ok := a.skillBody(g.experience.workdir, "sample")
	if !ok {
		t.Fatal("missing skill")
	}
	result := g.experience.load(context.Background(), "sample", body)
	if !strings.Contains(result, "loading is not proof") || !strings.Contains(result, "bash -- ") {
		t.Fatal("verification contract missing")
	}
}

func TestBackgroundVerifierRunsWithoutPrematureSuccessReceipt(t *testing.T) {
	a, s, g, st, _, verify := activityFixture(t, "sleep 0.1\nprintf 'done\\n'\n")
	activityTool(t, a, s, g, "load", "skill", map[string]any{"name": "sample"})
	activityTool(t, a, s, g, "background-verify", "bash", map[string]any{"command": "bash -- " + shellQuoted(verify), "background": true})
	g.experience.close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !activityStatus(t, st).Protected {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r := activityStatus(t, st); r.Protected || r.ApplicationCount != 0 {
		t.Fatal("background was treated as foreground verified use or leaked reference", r)
	}
}

func TestRecallMemoryProtectionDoesNotRenewItsLifetime(t *testing.T) {
	a, s, g, st, _, _ := activityFixture(t, "exit 0\n")
	c := port.ExperienceChange{ObjectID: "remembered", OperationID: "publish-memory", ObservationID: "memory", Owner: "plugin:engram", Kind: "memory", State: "active", Memory: &port.Memory{Text: "rarecompilermemoryfact"}}
	if _, err := st.ApplyExperience(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	out, err := a.recallMemoryFn(context.Background(), AgentSpec{Name: "coder"}, g.experience)("rarecompilermemoryfact")
	if err != nil || !strings.Contains(out, "rarecompilermemoryfact") {
		t.Fatalf("recall failed: %q %v", out, err)
	}
	inv, err := st.Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range inv {
		if entry.Name == "managed:remembered" {
			found = true
			if !entry.Retention.Protected || !entry.Retention.LastApplied.IsZero() || entry.Retention.ApplicationCount != 0 {
				t.Fatal("recall mislabeled as application", entry.Retention)
			}
		}
	}
	if !found {
		t.Fatal("memory not in inventory")
	}
	g.experience.close()
	inv, _ = st.Inventory(context.Background())
	for _, entry := range inv {
		if entry.Retention.Protected {
			t.Fatal("memory protection leaked")
		}
	}
	_ = s
}
