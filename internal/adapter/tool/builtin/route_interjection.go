package builtin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// RouteInterjection lets the top-level orchestrator decide what to do with a user
// request that arrived MID-TASK (a steer). By default such a request is QUEUED to run
// as its own turn after the current task, so the agent keeps focus instead of thrashing
// between the two. Call this only when confident the request should instead change
// course now (redirect) or be folded into the current task (append). Offered only to the
// orchestrator (env.RouteInterjection is nil for subagents, which the user does not steer).
type RouteInterjection struct{}

type routeInterjectionArgs struct {
	Action    string `json:"action"`
	Reason    string `json:"reason"`
	RequestID string `json:"request_id"`
}

func (RouteInterjection) Name() string { return "route_interjection" }
func (RouteInterjection) Description() string {
	return "Decide how to handle a NEW user request that arrived mid-task. Safe default: do NOTHING — it stays " +
		"QUEUED and runs as its own turn after you finish. Call ONLY when confident: \"redirect\" (set the current " +
		"task aside, switch now), \"append\" (fold into the current task, satisfy both before finishing), or " +
		"\"queue\" (confirm deferral). With several pending, pass request_id (the [req: …] handle) to say which. " +
		"When unsure, do not call this. Give a one-line reason."
}
func (RouteInterjection) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["queue","redirect","append","answered"],"description":"queue (defer to its own turn), redirect (switch to it now), append (fold into current task), or answered (you already answered it in your reply — stop showing it as pending)"},"reason":{"type":"string","description":"one line: why this routing"},"request_id":{"type":"string","description":"which pending request to route: the [req: …] handle shown with the message; omit to route the oldest pending request"}},"required":["action","reason"]}`)
}

func (RouteInterjection) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.RouteInterjection == nil {
		return errResult("", "route_interjection is only available to the top-level orchestrator"), nil
	}
	var a routeInterjectionArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	action := strings.ToLower(strings.TrimSpace(a.Action))
	switch action {
	case "queue", "redirect", "append", "answered":
	case "":
		action = "queue"
	default:
		return errResult("", `action must be one of "queue", "redirect", "append", or "answered"`), nil
	}
	if err := env.RouteInterjection(action, strings.TrimSpace(a.Reason), strings.TrimSpace(a.RequestID)); err != nil {
		return errResult("", err.Error()), nil
	}
	msg := map[string]string{
		"queue":    "Interjection kept queued — it will run as its own turn after the current task. Continue the current task now.",
		"redirect": "Redirecting: the queued request becomes your task now and the previous task is set aside. The no-progress count the old task ran up is cleared next step, so start on the new request without carrying it.",
		"append":   "Appended: the queued request is folded into your current task — satisfy both before you declare it finished. The no-progress count is cleared next step.",
		"answered": "Marked answered — it will stop being shown as pending. magi checks at the end of this turn that you actually said something after this call; if you said nothing, it goes back in the queue and runs as its own turn.",
	}[action]
	return okText("", msg), nil
}
