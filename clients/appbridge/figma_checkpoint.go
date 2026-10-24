package appbridge

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/event"
)

type figmaCheckpointReservation struct {
	Token     string
	After     int64
	Finishing bool
}

func checkpointActionName(args json.RawMessage) string {
	var a struct {
		Action string `json:"action"`
	}
	if json.Unmarshal(args, &a) != nil {
		return ""
	}
	return a.Action
}
func (s *Server) figmaCheckpointCursor(ctx context.Context, b Binding, name string, args json.RawMessage) (int64, error) {
	if s.Config.App != "figma" || name != "figma_checkpoint" || checkpointActionName(args) != "finish" {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	cl, err := daemon.DialWithin(b.socket, time.Second, 5*time.Second)
	if err != nil {
		return 0, err
	}
	defer cl.Close()
	history, err := cl.History(b.Session)
	if err != nil {
		return 0, err
	}
	var seq int64
	for _, e := range history {
		if e.Seq > seq {
			seq = e.Seq
		}
	}
	return seq, nil
}
func (s *Server) recordFigmaCheckpoint(b Binding, name string, args json.RawMessage, callID string, after int64, result any) error {
	if s.Config.App != "figma" || name != "figma_checkpoint" {
		return nil
	}
	action := checkpointActionName(args)
	if action != "finish" && action != "reopen" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.bindings[b.ID]
	if current == nil || current.Session != b.Session {
		return errors.New("checkpoint session changed before reservation")
	}
	if action == "reopen" {
		current.checkpoint = nil
		return nil
	}
	payload, ok := result.(map[string]any)
	if !ok || payload["checkpoint_ready"] != true || payload["checkpoint_token"] != callID {
		return errors.New("Figma did not acknowledge the host checkpoint reservation")
	}
	current.checkpoint = &figmaCheckpointReservation{Token: callID, After: after}
	return nil
}
func validCheckpointCompletion(e event.Event) bool {
	if e.Type != event.TypeTurnFinished || e.Seq <= 0 || e.Actor.Kind != event.ActorAgent {
		return false
	}
	var d event.TurnFinishedData
	return json.Unmarshal(e.Data, &d) == nil && !d.Unverified && d.Reason == ""
}
func (s *Server) observeFigmaCheckpoint(b Binding, e event.Event) {
	if s.Config.App != "figma" || e.Type != event.TypeTurnFinished {
		return
	}
	s.mu.Lock()
	current := s.bindings[b.ID]
	if current == nil || current.Session != b.Session || current.checkpoint == nil || current.checkpoint.Finishing || e.Seq <= current.checkpoint.After {
		s.mu.Unlock()
		return
	}
	reservation := current.checkpoint
	// The first terminal event consumes the reservation. A cancelled/failed turn
	// cannot leave a publication armed for a later unrelated task.
	if !validCheckpointCompletion(e) {
		current.checkpoint = nil
		s.mu.Unlock()
		return
	}
	reservation.Finishing = true
	s.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		if err := s.completeFigmaCheckpoint(ctx, b, reservation, e.Seq); err != nil {
			log.Printf("figma checkpoint retained: %v", err)
		}
		s.mu.Lock()
		if live := s.bindings[b.ID]; live != nil && live.checkpoint == reservation {
			live.checkpoint = nil
		}
		s.mu.Unlock()
	}()
}
func (s *Server) completeFigmaCheckpoint(ctx context.Context, b Binding, reservation *figmaCheckpointReservation, seq int64) error {
	cl, err := daemon.DialWithin(b.socket, time.Second, 5*time.Second)
	if err != nil {
		return err
	}
	history, err := cl.History(b.Session)
	cl.Close()
	if err != nil {
		return err
	}
	// Re-read authoritative facts: a later correction to an initially successful
	// completion must win over the streamed event and must prevent publication.
	var last *event.Event
	for i := range history {
		e := &history[i]
		if e.Type == event.TypeTurnFinished && e.Seq >= seq && (last == nil || e.Seq > last.Seq) {
			last = e
		}
	}
	if last == nil || last.Seq != seq || !validCheckpointCompletion(*last) {
		return errors.New("completion changed or could not be confirmed")
	}
	s.mu.Lock()
	live := s.bindings[b.ID]
	active := live != nil && live.Session == b.Session && live.checkpoint == reservation
	s.mu.Unlock()
	if !active {
		return errors.New("checkpoint reservation expired")
	}
	args, err := json.Marshal(map[string]string{"token": reservation.Token})
	if err != nil {
		return err
	}
	_, err = s.relay(ctx, b, "__magi_checkpoint_complete", args)
	return err
}
