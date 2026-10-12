package daemon

import (
	"path/filepath"
	"testing"
)

func TestPublishedApplicationTypeSurvivesRecord(t *testing.T) {
	for _, kind := range []string{"1", "figma", "mail", "slack", "powerpoint", "word", "excel"} {
		t.Run(kind, func(t *testing.T) {
			socket := filepath.Join(t.TempDir(), "d.sock")
			undo, err := Publish(socket, "/workspace", "session", Identity{Type: kind, Role: "user-defined description"})
			if err != nil {
				t.Fatal(err)
			}
			defer undo()
			record, err := Published(socket)
			if err != nil || record.Type != kind || record.Role != "user-defined description" {
				t.Fatalf("type and role lost: %#v %v", record, err)
			}
		})
	}
}
