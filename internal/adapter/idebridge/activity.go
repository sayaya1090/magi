package idebridge

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

// The one word a screen shows for what the companion is doing.
//
// One vocabulary, decided in one place, because this repository has already paid for letting each
// screen decide: the VS Code status bar drew "idle" for what the daemon had not said while another
// panel drew "nothing running" for the same silence — one rule, two copies, two different sentences
// about one fact. That copy is in TypeScript and a third was about to be written in C#.
//
// Unknown is an answer, not a shrug. "We could not ask" is a different fact from "it said it is
// idle", and a screen that folds them claims to know something it does not.
const (
	NotRunning = "not-running"
	Idle       = "idle"
	Working    = "working"
	Waiting    = "waiting"
	Unknown    = "unknown"
)

// activity is the answer, split the way the screens need it.
//
// Setup is separate from State on purpose: State is what it is DOING and moves every second, Setup
// is what it is RUNNING ON and moves when somebody changes it. Folded into one, every poll redraws
// a whole footer, and "the model is unknown" arrives as the same kind of news as "it is idle".
type activity struct {
	State  string
	Asking string
	Doing  string
	Why    string
	Setup  map[string]string
}

// activityOf reads one status reply.
//
// Waiting beats working: a turn blocked on somebody IS running, but what the person needs to know
// is that it wants them. Reporting "working" there leaves a question standing with nothing pointing
// at it.
func activityOf(st daemon.Status) activity {
	a := activity{State: Idle, Setup: setupOf(st)}
	if st.Asking != nil {
		a.State = Waiting
		a.Asking = firstNonEmpty(st.Asking.What, st.Asking.Reason, st.Asking.Kind)
		return a
	}
	if doing := strings.TrimSpace(st.Doing); doing != "" {
		a.State, a.Doing = Working, doing
		return a
	}
	return a
}

// setupOf keeps a key OUT rather than setting it empty. A caller asking "did it say?" gets the true
// answer, and a screen merging this over a previous reading does not overwrite what was known with
// nothing. `model` in particular is only filled when the request named a session — reading its
// absence as "no model" would be wrong.
func setupOf(st daemon.Status) map[string]string {
	out := map[string]string{}
	for k, v := range map[string]string{
		"model": st.Model, "backend": st.Backend, "permission": st.Permission, "user": st.User,
	} {
		if t := strings.TrimSpace(v); t != "" {
			out[k] = t
		}
	}
	return out
}

// reach decides between "nobody is there" and "we could not ask", which are different facts.
//
// A socket file that exists with nothing behind it is a corpse rather than an absence, but for the
// person the useful word is still not-running: there is nothing to talk to. The reason is carried
// in Why anyway, because the two cases are fixed differently and a client that only ever saw the
// word could not tell somebody which.
func (b *bridge) reach() (*daemon.Client, activity) {
	if why := tooLong(b.socket); why != "" {
		return nil, activity{State: Unknown, Why: why}
	}
	if _, err := os.Stat(b.socket); err != nil {
		return nil, activity{State: NotRunning, Why: "no socket at " + b.socket}
	}
	c, err := b.dial()
	if err != nil {
		return nil, activity{State: NotRunning, Why: err.Error() + hint(b.socket)}
	}
	return c, activity{}
}

func (b *bridge) activity(req request) {
	c, bad := b.reach()
	if c == nil {
		b.answerActivity(req.ID, bad)
		return
	}
	st, err := c.Status(req.Session)
	if err != nil {
		// The connection is the suspect: a reply that never came leaves the stream out of step, and
		// reusing it would hand the next caller this call's answer.
		b.hangUp()
		b.answerActivity(req.ID, activity{State: Unknown, Why: err.Error()})
		return
	}
	b.answerActivity(req.ID, activityOf(st))
}

func (b *bridge) answerActivity(id int, a activity) {
	resp := map[string]any{"id": id, "ok": true, "state": a.State}
	// Only what was actually said. An empty string for a thing nobody reported reads on the other
	// side as a reported emptiness.
	for k, v := range map[string]string{"asking": a.Asking, "doing": a.Doing, "why": a.Why} {
		if v != "" {
			resp[k] = v
		}
	}
	if len(a.Setup) > 0 {
		resp["setup"] = a.Setup
	}
	b.reply(resp)
}

// tooLong asks the DAEMON package whether the OS will refuse this path.
//
// Not a copy of the rule. This package held its own limit and its own sentence, and the two drifted
// on the thing that matters: the core's copy still told people to move MAGI_CONFIG_DIR, which is the
// advice that broke the Office companions. One constant, one sentence, one place to correct it —
// which is the whole argument this package exists to make about the derivations it carries.
func tooLong(p string) string {
	if err := daemon.TooLong(p); err != nil {
		return err.Error()
	}
	return ""
}

// hint adds what the length check cannot see.
//
// Measured 2026-09-09 on Windows 11: under %AppData% an AF_UNIX bind succeeds and the CONNECT fails
// with WSAEINVAL, whatever the length — 46-byte paths there fail while a 64-byte path under %TEMP%
// works. So the listener believes it is up and every caller is refused, and neither side's error
// says where to look. The escape hatch already exists; this sentence is how somebody finds it.
func hint(socket string) string {
	if runtime.GOOS != "windows" {
		return ""
	}
	for _, seg := range strings.Split(filepath.ToSlash(socket), "/") {
		if strings.EqualFold(seg, "AppData") {
			return " — the socket is under %AppData%, where AF_UNIX connects are refused on this" +
				" platform however short the path; set MAGI_SOCKET_DIR to a directory outside it"
		}
	}
	return ""
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}
