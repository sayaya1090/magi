package app

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// overflowSizePatterns read the REAL request size out of a size refusal. Each backend words it
// differently; the first capture group is the request's token count:
//
//   - Claude Code CLI:  "Prompt is too long · the request is ~1035211 tokens (limit 1000000) …"
//   - Anthropic API:    "prompt is too long: 1035211 tokens > 1000000 maximum"
//   - OpenAI-compatible: "… maximum context length is 131072 tokens, however you requested 140000 tokens"
var overflowSizePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)request is ~?\s*([0-9][0-9,]*)\s*tokens`),
	regexp.MustCompile(`(?i)prompt is too long:\s*([0-9][0-9,]*)\s*tokens`),
	regexp.MustCompile(`(?i)you requested\s*([0-9][0-9,]*)\s*tokens`),
}

// overflowRequestTokens is the request size a size refusal states, or 0 when it states none.
func overflowRequestTokens(err error) int {
	if err == nil {
		return 0
	}
	msg := err.Error()
	for _, re := range overflowSizePatterns {
		if m := re.FindStringSubmatch(msg); m != nil {
			if n, e := strconv.Atoi(strings.ReplaceAll(m[1], ",", "")); e == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}

// maxEstimateScale bounds a learned ratio: a misparsed number must not make every later fold keep
// nothing. Measured on Korean/JSON history the truth ran at ~2.1x the bytes/4 estimate.
const maxEstimateScale = 8.0

// calibratedScale is real/estimated, never below 1 (an estimate that ran HIGH is the safe side and
// is left alone) and never above maxEstimateScale. The request also carried the system prompt the
// estimate excludes, so this errs toward folding a little more, never less.
func calibratedScale(real, estimated int) float64 {
	if real <= 0 || estimated <= 0 {
		return 0
	}
	r := float64(real) / float64(estimated)
	if r <= 1 {
		return 0
	}
	if r > maxEstimateScale {
		r = maxEstimateScale
	}
	return r
}

// learnEstimateScale records a session's real/estimated ratio. Only ever raised: one refusal is a
// floor on how wrong the estimate is for this conversation, and a later smaller ratio from a
// shorter request does not show the earlier one was wrong.
func (a *App) learnEstimateScale(sid session.SessionID, real, estimated int) {
	r := calibratedScale(real, estimated)
	if r == 0 {
		return
	}
	a.mu.Lock()
	st := a.stateLocked(sid)
	if r > st.estimateScale {
		st.estimateScale = r
	}
	a.mu.Unlock()
}

func (a *App) estimateScaleOf(sid session.SessionID) float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok {
		return st.estimateScale
	}
	return 0
}

// observeEstimateScale records the ratio measured on an ANSWERED request. Unlike a refusal this is
// the current conversation's own truth, so it replaces the old value — in either direction, and back
// to 0 when the estimate ran high (a fold that keeps too much is the failure; one that keeps too
// little only costs context).
func (a *App) observeEstimateScale(sid session.SessionID, real, estimated int) {
	r := calibratedScale(real, estimated)
	a.mu.Lock()
	a.stateLocked(sid).estimateScale = r
	a.mu.Unlock()
}

// estimateChatRequest is estimateTokens over exactly what one request carries: system, messages
// and tool specs. It is the denominator for observeEstimateScale, so it must count the same parts
// the backend's prompt_tokens counted.
func estimateChatRequest(r port.ChatRequest) int {
	return estimateTokens(r.System, r.Messages) + toolSpecTokens(r.Tools)
}

// scaleEstimate turns an estimate into real-token units with a learned scale (0 = unlearned).
func scaleEstimate(n int, scale float64) int {
	if scale <= 1 {
		return n
	}
	return int(float64(n) * scale)
}
