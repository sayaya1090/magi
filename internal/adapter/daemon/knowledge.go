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
	items, err := host.ManageKnowledge(ctx, action)
	if err != nil {
		return Response{Err: err.Error()}
	}
	return Response{OK: true, Knowledge: items}
}
