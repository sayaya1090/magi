package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A `&` at the END of a background command is stripped and said out loud. A `&` in the MIDDLE is
// neither: what follows it becomes the job's foreground, so the job's exit is that command's and
// says nothing about the detached work — and it arrives labelled with the job's own id, which is
// the most convincing form the wrong answer could take.
//
// Live shape (fix-ocaml-gc, 2026-08-02): `make world 2>&1 &` newline `sleep 300` with
// background:true. bash_output answered `[bg_1 exited 0]` — the sleep's — while a later `ps` still
// found make running, and thirty calls on the same job's output carried
// `make: *** [Makefile:696: coldstart] Terminated`.
//
// The foreground path has warned about an interior `&` since the note was written. This is the
// background branch saying the same thing.
func TestBackgroundWithAnInteriorDetachSaysTheExitIsNotTheJobs(t *testing.T) {
	for _, tc := range []struct {
		what, cmd string
		want      bool
	}{
		{"the live shape", "cd /app && make world 2>&1 &\nsleep 300\n", true},
		{"detach then report", "make build & echo started", true},
		{"trailing & (stripped instead)", "python server.py &", false},
		{"nothing detached", "make world", false},
		{"an && chain is not a detach", "make world && ./run", false},
	} {
		t.Run(tc.what, func(t *testing.T) {
			args, _ := json.Marshal(map[string]any{"command": tc.cmd, "background": true})
			res, err := Bash{}.Execute(context.Background(), args,
				port.ToolEnv{Workdir: t.TempDir(), SessionID: session.SessionID("s-bgdetach")})
			if err != nil {
				t.Fatal(err)
			}
			var got string
			if json.Unmarshal(res.Content, &got) != nil {
				got = string(res.Content)
			}
			if !strings.Contains(got, "started background command") {
				t.Fatalf("the job did not start, so this asserts nothing:\n%s", got)
			}
			said := strings.Contains(got, "detaches work with `&`")
			if said != tc.want {
				t.Errorf("interior-detach note = %v, want %v:\n%s", said, tc.want, got)
			}
			// The two notes are alternatives, never both: a stripped trailing `&` leaves nothing
			// detached to warn about.
			if said && strings.Contains(got, "dropped a redundant") {
				t.Errorf("both notes fired for one command:\n%s", got)
			}
		})
	}
}

// The start-of-job note scrolls away; `[job exited 0]` is what the model acts on, and it wears the
// job's own id. On the run that prompted this there was one warning at start and then FIVE such
// reads while make was still running, so the fact belongs on the exit line too.
func TestAnExitedJobSaysWhoseExitItIsWhenSomethingDetached(t *testing.T) {
	for _, tc := range []struct {
		what, cmd string
		want      bool
	}{
		{"interior detach", "sleep 0 & true", true},
		{"nothing detached", "true", false},
	} {
		t.Run(tc.what, func(t *testing.T) {
			p := &bgProc{id: "bg_t", command: tc.cmd, done: true, exit: 0}
			got := p.status()
			if !strings.Contains(got, "exited 0") {
				t.Fatalf("expected a finished job, got %q", got)
			}
			if said := strings.Contains(got, "detached work with `&`"); said != tc.want {
				t.Errorf("note = %v, want %v: %q", said, tc.want, got)
			}
		})
	}
	// A killed or signalled job takes a different branch and must not grow the note twice.
	for _, p := range []*bgProc{
		{id: "bg_k", command: "sleep 0 & true", killed: true},
		{id: "bg_s", command: "sleep 0 & true", done: true, exit: -1},
	} {
		if n := strings.Count(p.status(), "detached work"); n > 1 {
			t.Errorf("%q repeats the note %d times", p.status(), n)
		}
	}
}
