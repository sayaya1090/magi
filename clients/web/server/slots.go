package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// slots handles GET /slots for querying plugin UI slots, and POST /slots for executing slot actions.
func (s *server) slots(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		var slots []port.PluginSlot
		err := s.alone(r, func(cl *daemon.Client, _ session.SessionID) error {
			resp, err := cl.Knowledge(port.KnowledgeAction{})
			if err == nil && resp.OK {
				slots = resp.Slots
			}
			return err
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if slots == nil {
			slots = []port.PluginSlot{}
		}
		writeJSON(w, "slots", slots)
		return
	}

	if postOnly(w, r) {
		return
	}

	action := r.FormValue("action")
	plugin := r.FormValue("plugin")
	slot := r.FormValue("slot")
	if action == "" {
		http.Error(w, "action is required", http.StatusBadRequest)
		return
	}

	var resp daemon.Response
	err := s.alone(r, func(cl *daemon.Client, _ session.SessionID) error {
		var kerr error
		resp, kerr = cl.Knowledge(port.KnowledgeAction{Name: plugin, Slot: slot, Action: action})
		return kerr
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if !resp.OK {
		http.Error(w, resp.Err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, "slot-action", resp)
}

// slotFollowRetry is how long a slot watch waits before reconnecting to a daemon that went away.
const slotFollowRetry = 5 * time.Second

// followSlots tells changed whenever the daemon says a slot changed. The transcript half of /events
// polls the log, and slot.changed is never written there, so this one fact needs the live stream.
// It starts at the end of the log: everything before that is history with no slot event in it.
func (s *server) followSlots(ctx context.Context, socket string, sid session.SessionID, changed chan<- struct{}) {
	for ctx.Err() == nil {
		head, _, err := s.reader.NewSince(ctx, sid, 0)
		if err == nil {
			err = followSlotsOnce(ctx, socket, sid, head, changed)
		}
		if err != nil && ctx.Err() == nil {
			log.Printf("slot watch for %s: %v", sid, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(slotFollowRetry):
		}
	}
}

func followSlotsOnce(ctx context.Context, socket string, sid session.SessionID, since int64, changed chan<- struct{}) error {
	cl, err := daemon.Dial(socket)
	if err != nil {
		return err
	}
	defer cl.Close()
	// Follow blocks on the socket for as long as the stream is quiet; closing it is the only way out.
	stop := context.AfterFunc(ctx, func() { cl.Close() })
	defer stop()
	return cl.Follow(string(sid), since, daemon.Tail{Each: func(ev event.Event) bool {
		if ev.Type == event.TypeSlotChanged {
			select {
			case changed <- struct{}{}:
			default: // one is already waiting, and it means the same thing
			}
		}
		return ctx.Err() == nil
	}})
}
