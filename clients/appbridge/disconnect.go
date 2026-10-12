package appbridge

import (
	"context"
	"errors"
	"net/http"
)

// Caller holds mu. Buffered replies ensure cancellation never waits on a plugin.
func (s *Server) failCallsLocked(binding, message string) {
	for id, c := range s.calls {
		if c.binding == binding || c.Lease == binding {
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
	s.cancelBrowserChildren(id)
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
	return s.detachBinding(snapshot)
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
