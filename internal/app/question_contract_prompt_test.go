package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/report"
)

func TestQuestionContractInActualRequest(t *testing.T) {
	for _, tt := range []struct {
		name, body, want        string
		interactive, restricted bool
	}{
		{name: "custom", body: "Decision grounds\n## sections\n- evidence: measured evidence\n- consequence: cost of the choice", want: "\n  evidence — measured evidence\n  consequence — cost of the choice", interactive: true},
		{name: "empty-contract-fallback", body: "Decision grounds without a sections block", want: report.Default.Spec(), interactive: true},
		{name: "missing-skill-fallback", want: report.Default.Spec(), interactive: true},
		{name: "headless", body: "## sections\n- evidence: measured evidence"},
		{name: "allowlist-excludes-question", body: "## sections\n- evidence: measured evidence", interactive: true, restricted: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, wd := newApp(t, &fakeLLM{}, Config{Permission: "allow", Interactive: tt.interactive})
			a.tools.Register(builtin.AskUser{})
			if tt.body != "" {
				writeSkill(t, filepath.Join(wd, ".magi", "skills"), report.SkillName, tt.body)
			}
			ctx := context.Background()
			sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
			if err != nil {
				t.Fatal(err)
			}
			agent := AgentSpec{Name: "main"}
			if tt.restricted {
				agent.Tools = []string{"read"}
			}
			tc := turnCtx{s: a.sessionInfo(ctx, sid), agent: agent, runStart: time.Now(), guard: newRunGuard(nil)}
			req, _ := a.buildStepRequest(ctx, tc, nil, 1, 0)
			var contractText string
			for _, m := range req.Messages {
				for _, p := range m.Parts {
					if strings.Contains(p.Text, "# Required ask_user report fields") {
						contractText += p.Text
					}
				}
			}
			available := tt.interactive && !tt.restricted
			if contextToolNames(req.Tools)["ask_user"] != available {
				t.Fatal("unexpected tool visibility")
			}
			if !available {
				if contractText != "" {
					t.Fatalf("unavailable question tool advertised: %s", contractText)
				}
				return
			}
			if !strings.Contains(contractText, "Each question's report must contain nonempty strings for these keys:"+tt.want) {
				t.Fatalf("request missing exact contract %q: %s", tt.want, contractText)
			}
			if tt.name == "custom" && strings.Contains(contractText, report.Default.Spec()) {
				t.Fatal("default contract leaked into custom contract")
			}
		})
	}
}
