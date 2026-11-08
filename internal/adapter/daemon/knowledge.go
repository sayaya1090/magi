package daemon

import (
	"context"
	"encoding/json"

	"github.com/sayaya1090/magi/internal/port"
)

type KnowledgeManager = port.KnowledgeManager

func answerKnowledge(ctx context.Context, eng Engine, req Request) Response {
	host, ok := eng.(KnowledgeManager)
	if !ok {
		return Response{Err: "this companion does not offer knowledge management"}
	}
	var action port.KnowledgeAction
	if len(req.Args) > 0 {
		if err := json.Unmarshal(req.Args, &action); err != nil {
			return Response{Err: "invalid knowledge action"}
		}
	}

	var doing string
	sm, hasSlots := eng.(port.SlotManager)
	if hasSlots && action.Action != "" && !isKnowledgeAction(action.Action) {
		// Generic slot action dispatch: action.Name carries the plugin. Its failure is the answer —
		// handing the action on to the knowledge store would replace what went wrong with a
		// complaint about a managed item nobody selected.
		out, err := sm.TriggerSlotAction(ctx, action.Name, action.Slot, action.Action)
		if err != nil {
			return Response{Err: err.Error()}
		}
		doing = out
		action = port.KnowledgeAction{}
	}

	items, err := host.ManageKnowledge(ctx, action)
	if err != nil {
		return Response{Err: err.Error()}
	}

	resp := Response{OK: true, Knowledge: items, Doing: doing}
	if hasSlots {
		slots, err := sm.QuerySlots(ctx, "knowledge")
		if err == nil {
			resp.Slots = slots
		}
	}
	return resp
}

// isKnowledgeAction names what the knowledge store itself does; anything else belongs to a slot.
func isKnowledgeAction(action string) bool {
	switch action {
	case "pin", "unpin", "archive", "restore", "import":
		return true
	}
	return false
}
