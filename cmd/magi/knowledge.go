package main

import (
	"context"
	"fmt"

	"github.com/sayaya1090/magi/internal/port"
)

func (d daemonEngine) ManageKnowledge(ctx context.Context, action port.KnowledgeAction) ([]port.KnowledgeItem, error) {
	return d.App.ManageProjectKnowledge(ctx, d.workdir, action)
}

func (d daemonEngine) QuerySlots(ctx context.Context, target string) ([]port.PluginSlot, error) {
	if d.obs == nil {
		return nil, nil
	}
	return d.obs.QuerySlots(ctx, target)
}

func (d daemonEngine) PluginCommandList() []port.PluginCommandInfo {
	if d.obs == nil {
		return nil
	}
	return d.obs.PluginCommandList()
}

func (d daemonEngine) RunPluginCommand(ctx context.Context, session, name string, args []string) (string, bool, error) {
	if d.obs == nil {
		return "", false, fmt.Errorf("plugin observer is unavailable")
	}
	return d.obs.RunPluginCommand(ctx, session, name, args)
}

func (d daemonEngine) TriggerSlotAction(ctx context.Context, plugin, slot, action string) (string, error) {
	if d.obs == nil {
		return "", fmt.Errorf("plugin observer is unavailable")
	}
	return d.obs.TriggerSlotAction(ctx, plugin, slot, action)
}
