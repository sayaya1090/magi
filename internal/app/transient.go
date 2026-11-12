package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A busy backend — 429, 5xx, Anthropic's 529 "overloaded" — used to end the turn on the spot. The
// OpenAI adapter retries twice within a second, and a plugin backend (claudecode, codex, …) not at
// all; then the error reached generateStep, which recorded it, and app.go deliberately does not
// re-run a turn that ended on an error. So fifteen minutes of a turn were lost to one minute of a
// provider's rate window, and the user's only recovery was to type the prompt again.
//
// The recovery belongs where the other two "ask again" recoveries already live (generateStep:
// compact-and-reissue for a size refusal, reissue for a silent backend): the request is unchanged
// and nothing was committed (the error arrived before any output), so asking again is safe. What a
// rate window needs that a stall does not is TIME: an exponential ladder, jittered so the companions
// and council members that hit the same backend together do not all come back on the same beat, and
// a server's own Retry-After taking precedence over the guess.
//
// Vars so tests can shrink them.
var (
	// maxTransientRetries bounds the ladder. With the defaults the waits are ~2,4,8,16,32,60s
	// (jittered to 50–100%), so a backend still refusing after about two minutes ends the turn as
	// before — once, with its own words — rather than holding it forever.
	maxTransientRetries  = 6
	transientBackoffBase = 2 * time.Second
	transientBackoffMax  = 60 * time.Second
	// transientWaitBudget bounds the cumulative wait of one step's ladder. A server that names a
	// longer wait than what is left is not asked early: the step stops RESUMABLY instead.
	transientWaitBudget = 3 * time.Minute
)

// statusRe reads a status code the way the adapters and backends phrase one: "status 429" (the
// OpenAI adapter), "API Error: 529" (the Claude CLI, through the claudecode plugin), "HTTP 503". A
// bare number is not enough: "127.0.0.1:5003" and request ids are full of them.
var statusRe = regexp.MustCompile(`(status|error|http|code)[^0-9a-z]{0,3}([1-5][0-9][0-9])\b`)

var retryAfterRe = regexp.MustCompile(`retry after (\d+)s`)

// classifyBackend reads err into the common contract (port.BackendError). What a backend said in
// structure wins; the wording is read only when it said nothing structured — which is still the
// common case for a plugin, whose failure crosses the boundary as "provider stream: <text>". The
// harness owns recovery, so this must hold whatever a backend does or fails to do.
func classifyBackend(err error) port.BackendError {
	if err == nil {
		return port.BackendError{Kind: port.KindUnknown}
	}
	if be, ok := port.AsBackendError(err); ok {
		c := *be
		if c.Kind == "" {
			c.Kind = kindFromText(err)
		}
		if c.RetryAfter == 0 {
			c.RetryAfter = textHint(err)
		}
		if c.Cause == nil {
			c.Cause = err
		}
		return c
	}
	c := port.BackendError{Kind: kindFromText(err), Cause: err, RetryAfter: textHint(err)}
	if m := statusRe.FindStringSubmatch(strings.ToLower(err.Error())); m != nil {
		c.Status, _ = strconv.Atoi(m[2])
	}
	return c
}

func containsAny(m string, subs ...string) bool {
	for _, s := range subs {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}

// kindFromText is the fallback reading. Order matters: an exhausted quota also says 429, a 504 also
// says "timeout", and the more specific reading must be taken first.
func kindFromText(err error) port.BackendErrorKind {
	// A cancellation is the person's, and a cut or magi-aborted stream is recovered elsewhere; none
	// is the backend asking to be asked again.
	if errors.Is(err, context.Canceled) || errors.Is(err, port.ErrStreamCut) || errors.Is(err, port.ErrStreamAborted) {
		return port.KindUnknown
	}
	if isContextOverflow(err) {
		return port.KindContextOverflow
	}
	m := strings.ToLower(err.Error())
	status := 0
	if sm := statusRe.FindStringSubmatch(m); sm != nil {
		status, _ = strconv.Atoi(sm[2])
	}
	switch {
	case containsAny(m, "quota", "billing", "usage limit"):
		return port.KindQuota
	case status == 401 || containsAny(m, "unauthorized", "invalid api key", "authentication_error"):
		return port.KindAuth
	case status == 403 || containsAny(m, "forbidden", "permission_error"):
		return port.KindPermission
	case status == 429 || containsAny(m, "rate limit", "rate_limit", "too many requests"):
		return port.KindRateLimit
	case status >= 500 || containsAny(m, "overloaded", "temporarily unavailable", "service unavailable",
		"server is busy", "try again later", "bad gateway"):
		return port.KindOverloaded
	case errors.Is(err, io.ErrUnexpectedEOF) || containsAny(m, "connection reset", "connection refused",
		"broken pipe", "unexpected eof", "exited before answering", "no such host", "network is unreachable"):
		// A plugin's backend process dying before it answered is the harness's to recover: the
		// plugin reports it and the step asks again (plugins no longer retry on their own).
		return port.KindNetwork
	case errors.Is(err, context.DeadlineExceeded) || containsAny(m, "timeout", "timed out", "deadline exceeded"):
		return port.KindTimeout
	case status >= 400 || containsAny(m, "bad request", "invalid_request"):
		return port.KindInvalidRequest
	}
	return port.KindUnknown
}

// isTransientBackend reports whether err is a backend saying "not now" rather than "no".
func isTransientBackend(err error) bool {
	return err != nil && classifyBackend(err).Kind.Retryable()
}

// retryAfterHint is the wait a backend named: the structured Retry-After when there is one, else
// the "retry after Ns" the OpenAI adapter writes into its error. 0 when none was named.
func retryAfterHint(err error) time.Duration {
	return classifyBackend(err).RetryAfter
}

func textHint(err error) time.Duration {
	m := retryAfterRe.FindStringSubmatch(strings.ToLower(err.Error()))
	if m == nil {
		return 0
	}
	n, cerr := strconv.Atoi(m[1])
	if cerr != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// transientJitter draws the jitter in [0, n). A variable so a test can fix it.
var transientJitter = rand.Int63n

// transientDelay is the wait before retry number attempt (0-based): the server's Retry-After when
// it named one — never shortened, since asking before then only earns another refusal; the
// budget check in generateStep is what bounds it — else an exponential step with jitter in
// [half, full].
func transientDelay(attempt int, hint time.Duration) time.Duration {
	if hint > 0 {
		return hint
	}
	full := transientBackoffBase << attempt
	if full <= 0 || full > transientBackoffMax {
		full = transientBackoffMax
	}
	half := full / 2
	return half + time.Duration(transientJitter(int64(full-half)+1))
}

// sleepCtx waits d, or less if ctx ends first; false means ctx ended.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// shortCause is the first line of err, clipped, for a progress note.
func shortCause(err error) string {
	s := strings.TrimSpace(err.Error())
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120]) + "…"
	}
	return s
}

// stopOnBackend records the error a model-backend failure ends the step with, in the common shape:
// its kind, how much recovery was spent, the wait the backend named and its request id — never
// request content. A retryable kind ends RESUMABLY, and the message says so: what was kept and how to
// carry on, so "error" is never the whole story.
func (a *App) stopOnBackend(ctx context.Context, sid session.SessionID, actor event.Actor, err error,
	attempts int, waited time.Duration, why string) error {
	cls := classifyBackend(err)
	resumable := cls.Kind.Retryable()
	out := err
	if resumable {
		switch {
		case why == "":
			why = fmt.Sprintf("still refused after %d retries over %s", attempts, waited.Round(time.Second))
		case attempts > 0:
			why += fmt.Sprintf(" (after %d retries over %s)", attempts, waited.Round(time.Second))
		}
		when := ""
		if cls.RetryAfter > 0 {
			when = fmt.Sprintf(" after %s (the backend asked to wait until %s)", cls.RetryAfter.Round(time.Second),
				time.Now().Add(cls.RetryAfter).Format("15:04"))
		}
		out = fmt.Errorf("%w — %s. Nothing was lost: the conversation and every finished tool result are kept, "+
			"and no unfinished tool call was run. To carry on from here, send a follow-up such as \"continue\"%s", err, why, when)
	}
	d, _ := json.Marshal(event.ErrorData{Message: out.Error(), Code: "provider", Kind: string(cls.Kind), Resumable: resumable,
		Attempts: attempts, WaitedMs: waited.Milliseconds(), RetryAfterMs: cls.RetryAfter.Milliseconds(), RequestID: cls.RequestID})
	a.appendFact(ctx, sid, event.TypeError, actor, d)
	return out
}

// ResumableTurn is a prompt whose turn stopped on a backend failure that asking again can fix.
type ResumableTurn struct {
	MessageID  string
	Text       string
	Kind       string
	RetryAfter time.Duration
}

// ResumableTurnOf reports whether the LAST prompt in a session stopped resumably. Kept apart from
// UnfinishedTurnOf on purpose: that turn did end (turn.finished is written, so a handoff asker
// resolves and the fleet does not show it working); what this answers is whether a follow-up can
// pick it up from the log as it stands. Read-only over the log, like UnfinishedTurnOf.
func (a *App) ResumableTurnOf(ctx context.Context, sid session.SessionID) (ResumableTurn, bool) {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return ResumableTurn{}, false
	}
	return resumableTurn(evs)
}

func resumableTurn(evs []event.Event) (ResumableTurn, bool) {
	var last ResumableTurn
	ok := false
	for _, e := range evs {
		switch e.Type {
		case event.TypePromptSubmitted:
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			last, ok = ResumableTurn{MessageID: d.MessageID, Text: promptText(d)}, false
		case event.TypeError:
			var d event.ErrorData
			if json.Unmarshal(e.Data, &d) == nil && !d.Recovered {
				ok = d.Resumable
				last.Kind = d.Kind
				last.RetryAfter = time.Duration(d.RetryAfterMs) * time.Millisecond
			}
		}
	}
	return last, ok
}
