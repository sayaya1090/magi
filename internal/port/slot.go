package port

import "context"

// PluginSlot represents a dynamic UI slot/block contributed by a plugin.
type PluginSlot struct {
	ID          string       `json:"id"`
	Plugin      string       `json:"plugin"`
	Target      string       `json:"target"`
	Title       string       `json:"title"`
	Description string       `json:"description,omitempty"`
	Status      *SlotStatus  `json:"status,omitempty"`
	Items       []SlotItem   `json:"items,omitempty"`
	Actions     []SlotAction `json:"actions,omitempty"`
}

// SlotStatus provides a badge or visual status indicator for the slot.
type SlotStatus struct {
	Label   string `json:"label"`
	Variant string `json:"variant,omitempty"` // "info", "warning", "success", "error"
	Badge   int    `json:"badge,omitempty"`
}

// SlotItem represents a single data item/metric rendered in the slot.
type SlotItem struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
	Warning     bool   `json:"warning,omitempty"`
}

// SlotAction represents an actionable button/control inside the slot.
type SlotAction struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Variant     string `json:"variant,omitempty"` // "primary", "secondary", "ghost"
}

// SlotManager provides access to plugin UI slots and their actions.
type SlotManager interface {
	QuerySlots(ctx context.Context, target string) ([]PluginSlot, error)
	TriggerSlotAction(ctx context.Context, plugin, slot, action string) (string, error)
}
