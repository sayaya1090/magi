package office

import (
	"context"
	"encoding/json"
	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestEmptyConversationRetriesDoNotAnnounceLive(t *testing.T) {
	dir, err := os.MkdirTemp("", "mbe-")
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
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 3; i++ {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			var req daemon.Request
			if e = json.NewDecoder(c).Decode(&req); e != nil {
				return
			}
			if e = json.NewEncoder(c).Encode(daemon.Response{Err: "unknown session"}); e != nil {
				return
			}
			if e = c.Close(); e != nil {
				return
			}
		}
	}()
	b := NewBridge()
	b.session = "empty"
	b.gen = 1
	b.empty = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i := 0; i < 3; i++ {
		if err := b.readOnce(ctx, 1, socket, "empty", 0); err == nil {
			t.Fatal("expected empty response")
		}
		if b.live || !b.empty {
			t.Fatal("socket connect changed empty conversation to live")
		}
	}
	<-done
}
