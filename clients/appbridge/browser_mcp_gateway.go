package appbridge

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// A transport session pins one already-authorized browser binding. It must never
// follow a newly active window or a replacement conversation after reconnect.
func (s *Server) registerBrowserMCPGateway() {
	if s.Config.App != "browser" {
		return
	}
	type lease struct {
		binding, session string
		at               time.Time
	}
	var mu sync.Mutex
	leases := map[string]lease{}
	cleanup := func(id string) {
		s.mu.Lock()
		s.failCallsLocked(id, "MCP 연결이 종료되었습니다.")
		delete(s.bindings, id)
		s.mu.Unlock()
		if s.Config.Release != nil {
			if err := s.Config.Release(id); err != nil {
				log.Printf("magi: browser MCP binding release failed: %v", err)
			}
		}
	}
	s.Mux.Handle("/mcp", s.Auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("Mcp-Session-Id")
		if r.Method == "DELETE" {
			mu.Lock()
			delete(leases, id)
			mu.Unlock()
			cleanup(id)
			w.WriteHeader(204)
			return
		}
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			http.Error(w, "MCP 요청이 너무 큽니다.", 400)
			return
		}
		var msg struct {
			Method string `json:"method"`
		}
		if json.Unmarshal(raw, &msg) != nil {
			http.Error(w, "잘못된 MCP 요청입니다.", 400)
			return
		}
		mu.Lock()
		now := time.Now()
		for key, l := range leases {
			if now.Sub(l.at) > time.Hour {
				delete(leases, key)
				cleanup(key)
			}
		}
		l, ok := leases[id]
		if msg.Method == "initialize" {
			if len(leases) >= 100 {
				mu.Unlock()
				http.Error(w, "MCP 연결 한도를 초과했습니다.", 429)
				return
			}
			s.mu.Lock()
			var selected *Binding
			count := 0
			for _, b := range s.bindings {
				if b.parent == "" {
					copy := *b
					selected = &copy
					count++
				}
			}
			s.mu.Unlock()
			if count != 1 {
				mu.Unlock()
				http.Error(w, "MCP 연결에는 브라우저 연결이 하나 필요합니다. 여러 창이면 해당 창의 세션별 MCP 주소를 사용해 주세요.", 409)
				return
			}
			id, err = randomID()
			if err != nil {
				mu.Unlock()
				http.Error(w, "MCP 연결 ID를 생성하지 못했습니다.", 500)
				return
			}
			l = lease{binding: selected.ID, session: selected.Session, at: now}
			leases[id] = l
			s.mu.Lock()
			s.bindings[id] = &Binding{ID: id, Session: id, Target: selected.Target, parent: selected.ID, expires: now.Add(time.Hour), pending: selected.pending}
			s.mu.Unlock()
			ok = true
		}
		if !ok {
			mu.Unlock()
			http.Error(w, "MCP를 먼저 초기화해 주세요.", 404)
			return
		}
		l.at = now
		leases[id] = l
		mu.Unlock()
		parent, err := s.binding(l.binding)
		if err != nil || parent.Session != l.session {
			http.Error(w, "원래 브라우저 창 또는 세션이 종료되었습니다. MCP를 다시 연결해 주세요.", 409)
			return
		}
		w.Header().Set("Mcp-Session-Id", id)
		copy := r.Clone(r.Context())
		u := *r.URL
		u.Path = "/mcp/" + id + "/" + id
		copy.URL = &u
		copy.Body = io.NopCloser(bytes.NewReader(raw))
		s.mcpHTTP(w, copy)
	})))
}
