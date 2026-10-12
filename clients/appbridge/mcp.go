package appbridge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

func (s *Server) mcpHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/mcp/"), "/")
	if len(parts) != 2 {
		http.Error(w, "session-bound MCP URL required", 404)
		return
	}
	id := parts[0]
	b, err := s.binding(id)
	if err != nil {
		http.Error(w, "unknown binding", 404)
		return
	}
	if b.Session != parts[1] {
		http.Error(w, "expired session", 409)
		return
	}
	var p struct {
		Version string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if !decode(w, r, &p) {
		return
	}
	if p.Version != "2.0" {
		http.Error(w, "JSON-RPC 2.0 required", 400)
		return
	}
	if len(p.ID) == 0 || string(p.ID) == "null" {
		w.WriteHeader(202)
		return
	}
	var result any
	switch p.Method {
	case "initialize":
		result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "magi-" + s.Config.App, "version": "0.1.0"}, "instructions": s.Config.Instructions}
	case "ping":
		result = map[string]any{}
	case "tools/list":
		tools := []any{}
		for _, t := range s.Config.Tools {
			tools = append(tools, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.InputSchema, "annotations": map[string]any{"readOnlyHint": t.ReadOnly}})
		}
		result = map[string]any{"tools": tools}
	case "tools/call":
		var a struct {
			Name string          `json:"name"`
			Args json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(p.Params, &a) != nil {
			err = errors.New("도구 인자가 올바르지 않습니다.")
		} else {
			allowed := false
			for _, t := range s.Config.Tools {
				if a.Name == t.Name {
					allowed = true
					break
				}
			}
			if !allowed {
				err = errors.New("등록되지 않은 앱 도구입니다.")
			} else if s.Config.Call != nil {
				result, err = s.Config.Call(r.Context(), b.Target, a.Name, a.Args)
			} else {
				result, err = s.relay(r.Context(), b, a.Name, a.Args)
			}
		}
		text := ""
		bad := err != nil
		if bad {
			text = err.Error()
		} else {
			data, e := json.Marshal(result)
			if e != nil {
				text = "도구 결과를 읽지 못했습니다."
				bad = true
			} else {
				text = string(data)
			}
		}
		result = map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": bad}
	default:
		write(w, map[string]any{"jsonrpc": "2.0", "id": p.ID, "error": map[string]any{"code": -32601, "message": "unsupported method"}})
		return
	}
	write(w, map[string]any{"jsonrpc": "2.0", "id": p.ID, "result": result})
}
func (s *Server) relay(ctx context.Context, b Binding, name string, args json.RawMessage) (any, error) {
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	c := &toolCall{ID: id, Name: name, Args: args, binding: b.ID, Session: b.Session, reply: make(chan toolReply, 1)}
	s.mu.Lock()
	current := s.bindings[b.ID]
	if current == nil || current.Session != b.Session {
		s.mu.Unlock()
		return nil, errors.New("만료된 세션의 도구 요청입니다.")
	}
	s.calls[id] = c
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.calls, id); s.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case b.pending <- c:
	case <-ctx.Done():
		return nil, errors.New("Figma 플러그인이 연결되어 있는지 확인해 주세요.")
	}
	select {
	case r := <-c.reply:
		if r.Error != "" {
			return nil, errors.New(r.Error)
		}
		return r.Result, nil
	case <-ctx.Done():
		return nil, errors.New("Figma 도구 응답 시간이 초과되었습니다. 변경 여부를 확인한 뒤 다시 요청해 주세요.")
	}
}
func (s *Server) pollHTTP(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Binding string `json:"binding"`
	}
	if !decode(w, r, &p) {
		return
	}
	b, e := s.binding(p.Binding)
	if e != nil {
		fail(w, e)
		return
	}
	s.mu.Lock()
	if current := s.bindings[p.Binding]; current != nil {
		current.lastPoll = time.Now()
	}
	s.mu.Unlock()
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for {
		select {
		case c := <-b.pending:
			s.mu.Lock()
			current := s.bindings[b.ID]
			alive := s.calls[c.ID] == c && current != nil && current.Session == c.Session
			s.mu.Unlock()
			if alive {
				write(w, c)
				return
			}
		case <-r.Context().Done():
			return
		case <-timer.C:
			write(w, map[string]any{})
			return
		}
	}
}
func (s *Server) resultHTTP(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Binding string `json:"binding"`
		CallID  string `json:"callId"`
		Session string `json:"session"`
		Result  any    `json:"result"`
		Error   string `json:"error"`
	}
	if !decode(w, r, &p) {
		return
	}
	s.mu.Lock()
	c := s.calls[p.CallID]
	b := s.bindings[p.Binding]
	if c == nil || c.binding != p.Binding || b == nil || c.Session != b.Session || p.Session != c.Session {
		s.mu.Unlock()
		http.Error(w, "만료되었거나 다른 파일의 도구 요청입니다.", 409)
		return
	}
	delete(s.calls, p.CallID)
	s.mu.Unlock()
	c.reply <- toolReply{Result: p.Result, Error: p.Error}
	write(w, map[string]any{"ok": true})
}
