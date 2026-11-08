package office

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/port"
)

func TestBridgeSlotsAnswerTheTargetAndActionsNameTheSlot(t *testing.T) {
	dir, err := os.MkdirTemp("", "mbs-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var mu sync.Mutex
	var asked []string
	go func() {
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				dec := json.NewDecoder(c)
				for {
					var raw json.RawMessage
					if dec.Decode(&raw) != nil {
						return
					}
					mu.Lock()
					asked = append(asked, string(raw))
					mu.Unlock()
					if json.NewEncoder(c).Encode(daemon.Response{OK: true, Slots: []port.PluginSlot{
						{ID: "a", Plugin: "p", Target: "knowledge"}, {ID: "b", Plugin: "p", Target: "panel"},
					}}) != nil {
						return
					}
				}
			}(c)
		}
	}()
	b := NewBridge()
	b.socket = socket
	for target, want := range map[string]string{"": "a,b", "panel": "b", "knowledge": "a", "none": ""} {
		slots, err := b.Slots(target)
		if err != nil {
			t.Fatalf("%q: %v", target, err)
		}
		if slots == nil {
			t.Fatalf("%q: nil slots marshal as null, not an empty list", target)
		}
		ids := make([]string, len(slots))
		for i, s := range slots {
			ids[i] = s.ID
		}
		if got := strings.Join(ids, ","); got != want {
			t.Errorf("target %q: got %q, want %q", target, got, want)
		}
	}
	if _, _, err := b.SlotAction("p", "b", "go"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	var found bool
	for _, a := range asked {
		if strings.Contains(a, `"slot":"b"`) && strings.Contains(a, `"action":"go"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no request named the slot: %v", asked)
	}
}
