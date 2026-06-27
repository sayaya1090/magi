package lua

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
)

// magi.on registers a lifecycle handler that the host runs on FireEvent.
func TestLifecycleStartupHook(t *testing.T) {
	var logged strings.Builder
	h := NewHostWithConfig(HostConfig{
		ToolSink: builtin.NewRegistry(),
		Logf:     func(s string) { logged.WriteString(s + "\n") },
	})
	dir := writePlugin(t, `name="hook"`+"\n"+`capabilities=["tool"]`,
		`magi.on("startup", function() magi.log("started up") end)`,
	)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Not fired until the host fires the event.
	if strings.Contains(logged.String(), "started up") {
		t.Fatal("hook ran before FireEvent")
	}
	h.FireEvent("startup")
	if !strings.Contains(logged.String(), "started up") {
		t.Errorf("startup hook did not run: %q", logged.String())
	}
	// A different event does not run the startup hook.
	logged.Reset()
	h.FireEvent("shutdown")
	if strings.Contains(logged.String(), "started up") {
		t.Errorf("startup hook ran on shutdown: %q", logged.String())
	}
}

// An unknown event name is rejected at registration.
func TestLifecycleUnknownEventRejected(t *testing.T) {
	h := NewHostWithConfig(HostConfig{ToolSink: builtin.NewRegistry(), Logf: func(string) {}})
	dir := writePlugin(t, `name="bad"`+"\n"+`capabilities=["tool"]`,
		`magi.on("whenever", function() end)`,
	)
	if _, err := h.Load(context.Background(), dir); err == nil {
		t.Fatal("expected load error for unknown event")
	}
}
