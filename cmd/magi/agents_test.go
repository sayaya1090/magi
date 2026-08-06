package main

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/fleet"
)

func render(list []fleet.Agent) string {
	var b strings.Builder
	printAgents(&b, list, "/cfg")
	return b.String()
}

// An empty list must say why it is empty and what to do about it. "No daemons" on its own reads as
// a broken listing; naming the directory it looked in turns it into a diagnosis, because the usual
// cause is a config directory that is not the one the daemons used.
func TestListingNothingSaysWhereItLooked(t *testing.T) {
	out := render(nil)
	if !strings.Contains(out, "/cfg") {
		t.Errorf("the empty listing does not name the directory it searched: %q", out)
	}
	if !strings.Contains(out, "--daemon") {
		t.Errorf("the empty listing does not say how to start one: %q", out)
	}
}

// The line has to carry what a person came for: which agent, what state, and the FULL workspace
// path — the path is what gets copied into the next command, so it is never clipped to fit.
func TestTheListingCarriesTheWholePath(t *testing.T) {
	long := "/Users/somebody/very/deeply/nested/checkouts/magi-experiment-number-four"
	out := render([]fleet.Agent{{
		Name: "magi-experiment-number-four", Workdir: long, State: fleet.Working,
		Steps: 12, Idle: 45, Live: true, Task: "make the failing test pass",
	}})
	if !strings.Contains(out, long) {
		t.Errorf("the workspace path was clipped:\n%s", out)
	}
	for _, want := range []string{"working", "12", "45s", "make the failing test pass"} {
		if !strings.Contains(out, want) {
			t.Errorf("the listing is missing %q:\n%s", want, out)
		}
	}
}

// What an agent is BLOCKED on displaces what it was doing. The second is context; the first is the
// reason nothing is happening, and it is the only line that asks the reader for something.
func TestABlockedAgentShowsThePromptNotTheTask(t *testing.T) {
	out := render([]fleet.Agent{{
		Name: "api", Workdir: "/w/api", State: fleet.Waiting, Live: true, Steps: 3,
		Task: "port the handler", Asking: "bash: rm -rf build  (destructive command detected)",
	}})
	if !strings.Contains(out, "rm -rf build") {
		t.Errorf("the pending prompt is not shown:\n%s", out)
	}
	if strings.Contains(out, "port the handler") {
		t.Errorf("the task displaced the prompt that is holding everything up:\n%s", out)
	}
	if !strings.Contains(out, "waiting") {
		t.Errorf("the state is not named:\n%s", out)
	}
}

// The local agent is marked. With several in the list, the one you are standing in is the one you
// most often mean, and nothing else on the line distinguishes it.
func TestTheLocalAgentIsMarked(t *testing.T) {
	out := render([]fleet.Agent{
		{Name: "here", Workdir: "/w/here", State: fleet.Idle, Live: true, Here: true, Idle: 5},
		{Name: "there", Workdir: "/w/there", State: fleet.Idle, Live: true, Idle: 5},
	})
	var hereLine, thereLine string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "/w/here") {
			hereLine = l
		}
		if strings.Contains(l, "/w/there") {
			thereLine = l
		}
	}
	if !strings.Contains(hereLine, "*") {
		t.Errorf("the local agent is not marked: %q", hereLine)
	}
	if strings.Contains(thereLine, "*") {
		t.Errorf("another agent was marked as local: %q", thereLine)
	}
}

// A dead entry needs its own sentence. "stopped" and "abandoned" both mean the process is gone, and
// only one of them means work was thrown away — a distinction nobody can be expected to remember
// from the word alone.
func TestDeadAgentsAreExplained(t *testing.T) {
	live := render([]fleet.Agent{{Name: "a", Workdir: "/w/a", State: fleet.Idle, Live: true}})
	if strings.Contains(live, "not running") {
		t.Errorf("a listing of live agents explains a situation that is not there:\n%s", live)
	}
	dead := render([]fleet.Agent{
		{Name: "a", Workdir: "/w/a", State: fleet.Idle, Live: true},
		{Name: "b", Workdir: "/w/b", State: fleet.Abandoned, Steps: 9},
	})
	if !strings.Contains(dead, "not running") || !strings.Contains(dead, "abandoned") {
		t.Errorf("a dead entry is not explained:\n%s", dead)
	}
}

// Ages read the way people say them, and "never" is not zero seconds: an agent whose log has no
// timestamp must not be reported as having just done something.
func TestAgesReadLikeAges(t *testing.T) {
	for _, c := range []struct {
		sec  int
		want string
	}{{-1, "-"}, {0, "0s"}, {45, "45s"}, {90, "1m"}, {3600 * 5, "5h"}, {86400 * 3, "3d"}} {
		if got := since(c.sec); got != c.want {
			t.Errorf("since(%d) = %q, want %q", c.sec, got, c.want)
		}
	}
}

// A task that is a paragraph gets its first line only, and a long one is cut. A listing whose rows
// are different heights is not a listing.
func TestALongTaskIsOneClippedLine(t *testing.T) {
	out := render([]fleet.Agent{{
		Name: "a", Workdir: "/w/a", State: fleet.Working, Live: true,
		Task: "first line of the request\nsecond line nobody asked to see\n" + strings.Repeat("x", 300),
	}})
	if strings.Contains(out, "second line nobody asked to see") {
		t.Errorf("the whole paragraph was printed:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 160 {
			t.Errorf("a %d-byte line got through:\n%s", len(line), line)
		}
	}
}
