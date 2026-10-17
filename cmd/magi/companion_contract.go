package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sayaya1090/magi/internal/adapter/companionpolicy"
	"github.com/sayaya1090/magi/internal/core/session"
)

func (d daemonEngine) CompanionOperation(ctx context.Context, action, id string, args json.RawMessage) (json.RawMessage, *session.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if action == "call" {
		res, err := companionpolicy.Call(ctx, d.configDir, d.workdir, id, args)
		if err != nil {
			return nil, nil, err
		}
		return nil, &res, nil
	}
	c, err := companionpolicy.Load(d.configDir, d.workdir)
	if err != nil {
		return nil, nil, err
	}
	switch action {
	case "list":
		b, err := json.Marshal(companionpolicy.Summaries(c))
		return b, nil, err
	case "describe":
		op, err := companionpolicy.Find(c, id)
		if err != nil {
			return nil, nil, err
		}
		b, err := json.Marshal(op)
		return b, nil, err
	default:
		return nil, nil, fmt.Errorf("unknown companion operation action %q", action)
	}
}
