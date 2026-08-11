package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/app"
)

// standingWork is the engine with something parked and something scheduled. The rest of the engine
// is the real one — only the two answers this panel section reads are stood in for, so the test
// cannot pass by accident on a model that never asked.
type standingWork struct {
	Engine
	parked []app.Parked
	jobs   []app.ScheduledJobInfo
}

func (s standingWork) ParkedWork() []app.Parked                    { return s.parked }
func (s standingWork) ScheduledJobs(string) []app.ScheduledJobInfo { return s.jobs }

// The panel says what is waiting on this turn, and what is coming later.
//
// The console has shown both for as long as it has had a pane. The terminal showed neither: what
// somebody typed while it was working went into a queue with no sign of it anywhere on screen, and
// standing work was only visible to whoever thought to open /cron.
func TestThePanelSaysWhatIsWaitingAndWhatIsScheduled(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 100, 40
	m.workdir = t.TempDir()
	m.app = standingWork{
		Engine: m.app,
		parked: []app.Parked{{Text: "also rename the token"}},
		jobs: []app.ScheduledJobInfo{
			{Name: "nightly-audit", Enabled: true, Next: time.Now().Add(3 * time.Hour)},
			{Name: "broken-one", Enabled: true, Problem: "matches no instant"},
			{Name: "switched-off", Enabled: false},
		},
	}

	// Waiting work opens the panel by itself: it is news, and this is the only place it is said.
	if !m.hasPanel() {
		t.Fatal("something is waiting on the turn and the panel is hidden")
	}
	panel := m.statusPanel(2)
	for _, want := range []string{"Waiting", "also rename the token", "Scheduled", "nightly-audit"} {
		if !strings.Contains(panel, want) {
			t.Errorf("the panel does not say %q:\n%s", want, panel)
		}
	}
	// The job that can never run is named — nothing else on any screen will mention it again.
	if !strings.Contains(panel, "broken-one") || !strings.Contains(panel, "never") {
		t.Errorf("a job with a schedule that never matches is not marked:\n%s", panel)
	}
	// The one somebody switched off is not: it is a fact about the config, not about what is coming.
	if strings.Contains(panel, "switched-off") {
		t.Errorf("a disabled job is listed as standing work:\n%s", panel)
	}

	// And a run with none of it keeps the panel as it was: standing jobs alone do not open it,
	// because they are true all day and a panel that opens for them is always open.
	m.app = standingWork{Engine: mm.app, jobs: []app.ScheduledJobInfo{{Name: "nightly-audit", Enabled: true, Next: time.Now().Add(time.Hour)}}}
	if m.hasPanel() {
		t.Error("a workspace with a nightly job has the panel open with nothing happening")
	}
}
