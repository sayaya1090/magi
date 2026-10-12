package appbridge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

const browserLeaseLifetime = 5 * time.Minute

func (s *Server) externalSocketAllowed(socket string) bool {
	if s.Config.ExternalSocketAllowed != nil {
		return s.Config.ExternalSocketAllowed(socket)
	}
	abs, err := filepath.Abs(socket)
	if err != nil || abs != filepath.Clean(socket) {
		return false
	}
	dir, err := filepath.EvalSymlinks(daemon.SocketDir(s.Config.ConfigDir))
	if err != nil {
		return false
	}
	actual, err := filepath.EvalSymlinks(socket)
	if err != nil || filepath.Dir(actual) != dir {
		return false
	}
	name := filepath.Base(actual)
	if !strings.HasPrefix(name, "daemon-") || !strings.HasSuffix(name, ".sock") {
		return false
	}
	info, err := os.Stat(actual)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return false
	}
	_, err = daemon.Published(actual)
	return err == nil
}

func (s *Server) AttachBrowser(ctx context.Context, primary, socket, session string, proxy bool) (Binding, []string, error) {
	if s.Config.App != "browser" || !s.externalSocketAllowed(socket) || session == "" {
		return Binding{}, nil, errors.New("게시된 로컬 컴패니언과 세션을 선택해 주세요.")
	}
	s.connectMu.Lock()
	defer s.connectMu.Unlock()
	s.mu.Lock()
	count := 0
	for _, item := range s.bindings {
		if item.parent != "" {
			count++
		}
	}
	s.mu.Unlock()
	if count >= 20 {
		return Binding{}, nil, errors.New("브라우저 사용권은 최대 20개입니다. 기존 연결을 해제해 주세요.")
	}
	parent, err := s.binding(primary)
	if err != nil || parent.parent != "" {
		return Binding{}, nil, errors.New("Chrome 연결을 먼저 열어 주세요.")
	}
	cl, err := daemon.DialWithin(socket, time.Second, 15*time.Second)
	if err != nil {
		return Binding{}, nil, err
	}
	defer cl.Close()
	rows, err := cl.Sessions()
	if err != nil {
		return Binding{}, nil, err
	}
	found := false
	for _, row := range rows {
		if row.ID == session {
			found = true
			break
		}
	}
	if !found {
		return Binding{}, nil, errors.New("선택한 세션을 찾지 못했습니다.")
	}
	id, err := randomID()
	if err != nil {
		return Binding{}, nil, err
	}
	b := &Binding{ID: id, Session: session, Target: parent.Target, socket: socket, parent: primary, pending: parent.pending, expires: time.Now().Add(browserLeaseLifetime), registration: s.serverName()}
	if proxy {
		b.registration = ""
	}
	s.mu.Lock()
	s.bindings[id] = b
	s.mu.Unlock()
	tools := []string{}
	if proxy {
		for _, tool := range s.Config.Tools {
			tools = append(tools, tool.Name)
		}
	} else {
		tools, err = cl.AttachMCP(session, b.registration, "http://"+s.Config.Addr+"/mcp/"+id+"/"+session, map[string]string{"Authorization": "Bearer " + s.Token})
		if err != nil {
			s.mu.Lock()
			delete(s.bindings, id)
			s.mu.Unlock()
			return Binding{}, nil, err
		}
	}
	if ctx.Err() != nil {
		s.mu.Lock()
		delete(s.bindings, id)
		s.mu.Unlock()
		s.detachBinding(*b)
		return Binding{}, nil, ctx.Err()
	}
	// Timer cleanup also removes tool definitions from idle sessions.
	s.scheduleExpiry(id)
	return *b, tools, nil
}
func (s *Server) attachBrowserHTTP(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Binding string `json:"binding"`
		Socket  string `json:"socket"`
		Session string `json:"session"`
		Proxy   bool   `json:"proxy"`
	}
	if !decode(w, r, &p) {
		return
	}
	b, tools, err := s.AttachBrowser(r.Context(), p.Binding, p.Socket, p.Session, p.Proxy)
	if err != nil {
		fail(w, err)
		return
	}
	var exposed any = tools
	if p.Proxy {
		exposed = s.Config.Tools
	}
	write(w, map[string]any{"lease": b.ID, "session": b.Session, "tools": exposed, "expiresAt": b.expires.UTC()})
}
func (s *Server) releaseBrowserHTTP(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Lease string `json:"lease"`
	}
	if !decode(w, r, &p) {
		return
	}
	s.mu.Lock()
	b := s.bindings[p.Lease]
	child := b != nil && b.parent != ""
	s.mu.Unlock()
	if !child {
		fail(w, errors.New("브라우저 도구 연결을 찾지 못했습니다."))
		return
	}
	if err := s.Disconnect(r.Context(), p.Lease); err != nil {
		fail(w, err)
		return
	}
	write(w, map[string]any{"ok": true})
}

// Caller must hold connectMu. Remove children before detaching so late results fail closed.
func (s *Server) cancelBrowserChildren(primary string) {
	s.mu.Lock()
	children := []Binding{}
	for id, b := range s.bindings {
		if b.parent == primary {
			children = append(children, *b)
			delete(s.bindings, id)
			s.failCallsLocked(id, "브라우저 연결이 변경되어 요청이 취소되었습니다.")
		}
	}
	s.mu.Unlock()
	for _, b := range children {
		s.detachBinding(b)
	}
}
func (s *Server) detachBinding(b Binding) error {
	if s.Config.Release != nil {
		if err := s.Config.Release(b.ID); err != nil {
			return err
		}
	}
	if len(s.Config.Tools) == 0 || (b.parent != "" && b.registration == "") {
		return nil
	}
	cl, err := daemon.DialWithin(b.socket, time.Second, 3*time.Second)
	if err != nil {
		return errors.New("연결은 종료했지만 기존 도구를 해제하지 못했습니다.")
	}
	defer cl.Close()
	name := b.registration
	if name == "" {
		name = s.serverName()
	}
	if b.parent != "" {
		raw, _ := json.Marshal(daemon.Request{Method: "mcp-detach-if", Owner: b.Session, Name: name, URL: "http://" + s.Config.Addr + "/mcp/" + b.ID + "/" + b.Session, Headers: map[string]string{"Authorization": "Bearer " + s.Token}})
		response, e := cl.Raw(raw)
		if e != nil {
			return e
		}
		var reply struct {
			OK  bool   `json:"ok"`
			Err string `json:"err"`
		}
		if json.Unmarshal(response, &reply) != nil || !reply.OK {
			return errors.New("브라우저 MCP 등록 해제 여부를 확인하지 못했습니다.")
		}
		return nil
	}
	_, err = cl.DetachMCP(b.Session, name)
	return err
}

func (s *Server) acquireBrowser() (func(), error) {
	if s.Config.App != "browser" {
		return func() {}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.browserBusy {
		return nil, errors.New("브라우저가 다른 요청을 처리 중입니다. 완료 후 다시 요청해 주세요.")
	}
	s.browserBusy = true
	return func() { s.mu.Lock(); s.browserBusy = false; s.mu.Unlock() }, nil
}

// Caller holds mu. Both the source lease and the Chrome UI session must still match.
func (s *Server) callAliveLocked(c *toolCall) bool {
	parent := s.bindings[c.binding]
	if parent == nil || parent.Session != c.Session {
		return false
	}
	if c.Lease == "" {
		return true
	}
	source := s.bindings[c.Lease]
	return source != nil && source.parent == parent.ID && source.Session == c.SourceSession && time.Now().Before(source.expires)
}

func (s *Server) scheduleExpiry(id string) {
	time.AfterFunc(browserLeaseLifetime, func() {
		s.mu.Lock()
		b := s.bindings[id]
		expired := b != nil && !time.Now().Before(b.expires)
		s.mu.Unlock()
		if expired {
			s.Disconnect(context.Background(), id)
		} else if b != nil {
			s.scheduleExpiry(id)
		}
	})
}
func (s *Server) availableBrowserHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "GET required", 405)
		return
	}
	s.mu.Lock()
	items := []Binding{}
	for _, b := range s.bindings {
		if b.parent == "" && (s.Config.Call != nil || time.Since(b.lastPoll) < time.Minute) {
			items = append(items, *b)
		}
	}
	s.mu.Unlock()
	write(w, map[string]any{"bindings": items})
}
func (s *Server) callBrowserHTTP(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Lease   string          `json:"lease"`
		Socket  string          `json:"socket"`
		Session string          `json:"session"`
		Name    string          `json:"name"`
		Args    json.RawMessage `json:"args"`
	}
	if !decode(w, r, &p) {
		return
	}
	s.mu.Lock()
	b := s.bindings[p.Lease]
	valid := b != nil && b.parent != "" && b.socket == p.Socket && b.Session == p.Session && time.Now().Before(b.expires) && s.bindings[b.parent] != nil
	var snapshot Binding
	if valid {
		b.expires = time.Now().Add(browserLeaseLifetime)
		snapshot = *b
	}
	s.mu.Unlock()
	if !valid {
		fail(w, errors.New("브라우저 도구 연결이 만료되었거나 다른 세션의 연결입니다."))
		return
	}
	allowed := false
	for _, t := range s.Config.Tools {
		if t.Name == p.Name {
			allowed = true
		}
	}
	if !allowed {
		write(w, map[string]any{"error": "등록되지 않은 브라우저 도구입니다.", "expiresAt": snapshot.expires.UTC()})
		return
	}
	release, err := s.acquireBrowser()
	if err != nil {
		write(w, map[string]any{"error": err.Error(), "expiresAt": snapshot.expires.UTC()})
		return
	}
	defer release()
	var result any
	if s.Config.Call != nil {
		result, err = s.Config.Call(r.Context(), snapshot.ID, p.Name, p.Args)
	} else {
		result, err = s.relay(r.Context(), snapshot, p.Name, p.Args)
	}
	if err != nil {
		write(w, map[string]any{"error": err.Error(), "expiresAt": snapshot.expires.UTC()})
		return
	}
	s.mu.Lock()
	current := s.bindings[snapshot.ID]
	alive := current != nil && current.Session == snapshot.Session && s.bindings[snapshot.parent] != nil
	s.mu.Unlock()
	if !alive {
		write(w, map[string]any{"error": "브라우저 연결이 실행 중 종료되었습니다. 이미 수행된 조작의 결과를 확인해 주세요."})
		return
	}
	write(w, map[string]any{"result": result, "expiresAt": snapshot.expires.UTC()})
}

func (s *Server) checkBrowserHTTP(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Binding string `json:"binding"`
		CallID  string `json:"callId"`
		Session string `json:"session"`
	}
	if !decode(w, r, &p) {
		return
	}
	s.mu.Lock()
	c := s.calls[p.CallID]
	valid := c != nil && c.binding == p.Binding && c.Session == p.Session && s.callAliveLocked(c)
	s.mu.Unlock()
	write(w, map[string]bool{"valid": valid})
}
