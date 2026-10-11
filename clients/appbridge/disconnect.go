package appbridge

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

// Caller holds mu. Buffered replies ensure cancellation never waits on a plugin.
func (s *Server) failCallsLocked(binding, message string) {
	for id, c := range s.calls {
		if c.binding == binding {
			delete(s.calls, id)
			select {
			case c.reply <- toolReply{Error: message}:
			default:
			}
		}
	}
}
func (s *Server) Disconnect(ctx context.Context, id string) error {
	s.connectMu.Lock()
	defer s.connectMu.Unlock()
	s.mu.Lock()
	b := s.bindings[id]
	if b == nil {
		s.mu.Unlock()
		return nil
	}
	delete(s.bindings, id)
	if s.targets[b.Target] == id {
		delete(s.targets, b.Target)
	}
	s.failCallsLocked(id, "플러그인 연결이 종료되어 도구 요청이 취소되었습니다.")
	snapshot := *b
	s.mu.Unlock()
	if len(s.Config.Tools) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cl, err := daemon.DialWithin(snapshot.socket, time.Second, 3*time.Second)
	if err != nil {
		return errors.New("연결은 종료했지만 기존 도구를 해제하지 못했습니다.")
	}
	defer cl.Close()
	_, err = cl.DetachMCP(snapshot.Session, s.Config.App)
	return err
}
func (s *Server) disconnectHTTP(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Binding string `json:"binding"`
	}
	if !decode(w, r, &p) {
		return
	}
	if p.Binding == "" {
		fail(w, errors.New("연결 ID가 필요합니다."))
		return
	}
	if err := s.Disconnect(r.Context(), p.Binding); err != nil {
		fail(w, err)
		return
	}
	write(w, map[string]any{"ok": true})
}
