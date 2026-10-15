package lua

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A dishonest or mistaken analyzer cannot promote a failed turn into a skill.
// Drive the bundled plugin, including its asynchronous observation worker.
func TestEngramSkillPromotionRequiresHostVerified(t *testing.T) {
	for _, outcome := range []string{"verified", "unverified", "guard", "error", "ungated", "done", ""} {
		t.Run(outcome, func(t *testing.T) {
			wd := t.TempDir()
			log := &syncLog{}
			h := loadEngram(t, wd, &fakeAnalyzer{reply: `{"lesson":{"task":"gate regression","approach":"run check","outcome":"success","lesson":"retain task evidence"},"skill":{"name":"outcome_gate","description":"run check","technique":"false success returned by analyzer"}}`}, log)
			h.FireEventWith("user_message", map[string]string{"session": "s", "text": "run check"})
			h.FireEventWith("turn_finished", map[string]string{"session": "s", "text": "analyzer claims success", "outcome": outcome})
			h.DrainEvents(5 * time.Second)
			b, err := os.ReadFile(filepath.Join(wd, ".claude", "skills", "outcome_gate", "SKILL.md"))
			if outcome == "verified" {
				if err != nil || !strings.Contains(string(b), "false success") {
					t.Fatalf("verified skill missing: %v; %s", err, log.String())
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("host outcome %q must not create a skill: %v; %s", outcome, err, b)
			}
			if outcome == "error" {
				if _, err := os.Stat(filepath.Join(wd, "SESSION_SUMMARY.md")); err != nil {
					t.Fatalf("failed-turn lessons must still be recorded: %v", err)
				}
			}
		})
	}
}
