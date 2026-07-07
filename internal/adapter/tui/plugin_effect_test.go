package tui

import (
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// fakeCmdSource is a CommandSource whose queued UI effects are drained once.
type fakeCmdSource struct{ effects []string }

func (f *fakeCmdSource) PluginCommands() []port.PluginCommand           { return nil }
func (f *fakeCmdSource) DispatchCommand(string, []string) (bool, error) { return false, nil }
func (f *fakeCmdSource) TakeUIEffects() []string {
	e := f.effects
	f.effects = nil
	return e
}

// A plugin command's clear_transcript effect empties the visible transcript so the
// View falls back to the splash; the effect drains once.
func TestApplyPluginUIEffectsClearsTranscript(t *testing.T) {
	src := &fakeCmdSource{effects: []string{"clear_transcript"}}
	m := &Model{
		cmds:     src,
		blocks:   []block{{kind: blockUser, text: "old turn"}},
		cache:    []string{"rendered"},
		liveText: "streaming…",
	}
	m.applyPluginUIEffects()
	if len(m.blocks) != 0 {
		t.Errorf("blocks not cleared: %d remain", len(m.blocks))
	}
	if len(m.cache) != 0 {
		t.Errorf("cache not cleared: %d remain", len(m.cache))
	}
	if m.liveText != "" {
		t.Errorf("liveText not cleared: %q", m.liveText)
	}
	// A second apply with nothing queued is a no-op (does not panic).
	m.applyPluginUIEffects()
}

// An unknown effect is ignored rather than mutating the view or panicking.
func TestApplyPluginUIEffectsIgnoresUnknown(t *testing.T) {
	src := &fakeCmdSource{effects: []string{"not_a_real_effect"}}
	m := &Model{cmds: src, blocks: []block{{kind: blockUser, text: "keep"}}}
	m.applyPluginUIEffects()
	if len(m.blocks) != 1 {
		t.Errorf("unknown effect must not clear blocks, got %d", len(m.blocks))
	}
}
