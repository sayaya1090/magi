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
	if hasSlots && action.Action != "" {
		// Generic slot action dispatch.
		// action.Name carries plugin/slot ID if present.
		out, err := sm.TriggerSlotAction(ctx, action.Name, action.Action)
		if err == nil && out != "" {
			doing = out
			action = port.KnowledgeAction{}
		}
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
