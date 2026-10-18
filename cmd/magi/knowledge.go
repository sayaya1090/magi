package main

import (
	"context"

	"github.com/sayaya1090/magi/internal/port"
)

func (d daemonEngine) ManageKnowledge(ctx context.Context, action port.KnowledgeAction) ([]port.KnowledgeItem, error) {
	return d.App.ManageProjectKnowledge(ctx, d.workdir, action)
}
