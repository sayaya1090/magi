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
	Action string `json:"action"`
	Reason string `json:"reason"`
}

func (RouteInterjection) Name() string { return "route_interjection" }
func (RouteInterjection) Description() string {
	return "Decide how to handle a NEW user request that arrived while you are mid-task. The safe default is to do " +
		"NOTHING and let it stay QUEUED — it runs as its own turn after you finish the current task. Call this ONLY when " +
		"you are confident: action \"redirect\" to set the current task aside and switch to the new request now; " +
		"\"append\" to fold the new request into the current task and satisfy both before finishing; \"queue\" to " +
		"explicitly confirm deferral. When unsure, do not call this. Give a one-line reason."
}
func (RouteInterjection) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["queue","redirect","append"],"description":"queue (defer to its own turn), redirect (switch to it now), or append (fold into current task)"},"reason":{"type":"string","description":"one line: why this routing"}},"required":["action","reason"]}`)
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
	case "queue", "redirect", "append":
	case "":
		action = "queue"
	default:
		return errResult("", `action must be one of "queue", "redirect", or "append"`), nil
	}
	if err := env.RouteInterjection(action, strings.TrimSpace(a.Reason)); err != nil {
		return errResult("", err.Error()), nil
	}
	msg := map[string]string{
		"queue":    "Interjection kept queued — it will run as its own turn after the current task. Continue the current task now.",
		"redirect": "Redirecting: the queued request becomes your task now and the previous task is set aside. Your plan and no-progress window are reset next step — plan and proceed on the new request.",
		"append":   "Appended: the queued request is folded into your current task — satisfy both before finishing. Your no-progress window is reset next step.",
	}[action]
	return okText("", msg), nil
}
