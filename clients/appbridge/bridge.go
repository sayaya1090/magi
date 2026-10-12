// Package appbridge connects application clients to isolated Magi companions.
package appbridge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/event"
)

type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
	ReadOnly    bool   `json:"-"`
}
type Config struct {
	App, Addr, ConfigDir, Token, Binary, Instructions string
	Tools                                             []Tool
	Call                                              func(context.Context, string, string, json.RawMessage) (any, error)
	// Socket injects a transport for tests. Production derives the path itself.
	Socket func(context.Context, string) (string, error)
}
type Binding struct {
	ID       string `json:"binding"`
	Session  string `json:"session"`
	Target   string `json:"target"`
	socket   string
	pending  chan *toolCall
	lastPoll time.Time
}
type Request struct {
	Binding  string `json:"binding"`
	Method   string `json:"method"`
	Text     string `json:"text,omitempty"`
	Name     string `json:"name,omitempty"`
	CallID   string `json:"callId,omitempty"`
	Decision string `json:"decision,omitempty"`
	Answer   string `json:"answer,omitempty"`
}
type toolCall struct {
	ID      string          `json:"callId"`
	Name    string          `json:"name"`
	Args    json.RawMessage `json:"args"`
	binding string
	Session string `json:"session"`
	reply   chan toolReply
}
type toolReply struct {
	Result any    `json:"result"`
	Error  string `json:"error"`
}
type Server struct {
	Config    Config
	Token     string
	Mux       *http.ServeMux
	mu        sync.Mutex
	connectMu sync.Mutex
	bindings  map[string]*Binding
	targets   map[string]string
	calls     map[string]*toolCall
}

func randomID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func New(c Config) (*Server, error) {
	if c.App != "figma" && c.App != "mail" && c.App != "slack" && c.App != "browser" && c.App != "docker" && c.App != "kubernetes" {
		return nil, errors.New("unknown application")
	}
	if c.Addr == "" {
		c.Addr = "127.0.0.1:8734"
	}
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return nil, errors.New("the client bridge must listen on a loopback IP")
	}
	if c.Binary == "" {
		c.Binary = os.Getenv("MAGI_BINARY")
	}
	if c.ConfigDir == "" {
		c.ConfigDir = os.Getenv("MAGI_CONFIG_DIR")
	}
	if c.ConfigDir == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		c.ConfigDir = filepath.Join(dir, "magi")
	}
	if c.Token == "" {
		c.Token, err = randomID()
		if err != nil {
			return nil, err
		}
	}
	s := &Server{Config: c, Token: c.Token, Mux: http.NewServeMux(), bindings: map[string]*Binding{}, targets: map[string]string{}, calls: map[string]*toolCall{}}
	s.Mux.Handle("/api/connect", s.Auth(http.HandlerFunc(s.connectHTTP)))
	s.Mux.Handle("/api/disconnect", s.Auth(http.HandlerFunc(s.disconnectHTTP)))
	s.Mux.Handle("/api/request", s.Auth(http.HandlerFunc(s.requestHTTP)))
	s.Mux.Handle("/api/events", s.Auth(http.HandlerFunc(s.eventsHTTP)))
	s.Mux.Handle("/api/figma/events", s.Auth(http.HandlerFunc(s.pollEventsHTTP)))
	s.Mux.Handle("/api/browser/poll", s.Auth(http.HandlerFunc(s.pollHTTP)))
	s.Mux.Handle("/api/browser/result", s.Auth(http.HandlerFunc(s.resultHTTP)))
	s.Mux.Handle("/api/figma/poll", s.Auth(http.HandlerFunc(s.pollHTTP)))
	s.Mux.Handle("/api/figma/result", s.Auth(http.HandlerFunc(s.resultHTTP)))
	s.Mux.Handle("/mcp/", s.Auth(http.HandlerFunc(s.mcpHTTP)))
	return s, nil
}
func (s *Server) Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, MCP-Protocol-Version, Mcp-Session-Id")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.Token)) != 1 {
			http.Error(w, "연결 토큰을 확인해 주세요.", 401)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	if err := d.Decode(v); err != nil {
		http.Error(w, "잘못된 요청입니다.", 400)
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		http.Error(w, "요청 하나만 전달해 주세요.", 400)
		return false
	}
	return true
}
func write(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, err error) { http.Error(w, err.Error(), 400) }
func (s *Server) binding(id string) (Binding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.bindings[id]
	if b == nil {
		return Binding{}, errors.New("연결이 만료되었습니다. 다시 연결해 주세요.")
	}
	return *b, nil
}

func (s *Server) socket(ctx context.Context, target string) (string, error) {
	if s.Config.Socket != nil {
		return s.Config.Socket(ctx, target)
	}
	h := sha256.Sum256([]byte(target))
	wd := filepath.Join(s.Config.ConfigDir, "clients", s.Config.App, hex.EncodeToString(h[:16]))
	sock := daemon.SocketPath(s.Config.ConfigDir, wd)
	cl, err := daemon.DialWithin(sock, time.Second, 3*time.Second)
	if err == nil {
		cl.Close()
		return sock, nil
	}
	if err = os.MkdirAll(wd, 0700); err != nil {
		return "", err
	}
	bin := s.Config.Binary
	if bin == "" {
		bin, err = exec.LookPath("magi")
		if err != nil {
			return "", errors.New("magi 실행 파일을 PATH에 설치해 주세요.")
		}
	}
	cmd := exec.CommandContext(ctx, bin, "--daemon", "--detach", "--no-update-check", "--permission", "ask", "--client-role", s.Config.App)
	cmd.Dir = wd
	cmd.Env = CompanionEnv(os.Environ(), s.Config.ConfigDir)
	if out, e := cmd.CombinedOutput(); e != nil {
		return "", fmt.Errorf("Magi를 시작하지 못했습니다: %s", strings.TrimSpace(string(out)))
	}
	return sock, nil
}
func (s *Server) Connect(ctx context.Context, target string) (Binding, error) {
	if strings.TrimSpace(target) == "" || len(target) > 512 {
		return Binding{}, errors.New("작업 대상 식별자가 필요합니다.")
	}
	s.connectMu.Lock()
	defer s.connectMu.Unlock()
	s.mu.Lock()
	var expired *Binding
	if id := s.targets[target]; id != "" {
		b := *s.bindings[id]
		if (s.Config.App == "figma" || s.Config.App == "browser") && time.Since(b.lastPoll) > time.Minute {
			expired = &b
			delete(s.bindings, id)
			delete(s.targets, target)
			s.failCallsLocked(id, "플러그인 연결이 만료되어 도구 요청이 취소되었습니다.")
		} else {
			s.mu.Unlock()
			if s.Config.App == "figma" || s.Config.App == "browser" {
				return Binding{}, errors.New("이 문서에는 다른 클라이언트이 연결되어 있습니다. 먼저 연결을 종료해 주세요.")
			}
			return b, nil
		}
	}
	s.mu.Unlock()
	if expired != nil && len(s.Config.Tools) > 0 {
		restore := func() {
			s.mu.Lock()
			s.bindings[expired.ID] = expired
			s.targets[target] = expired.ID
			s.mu.Unlock()
		}
		cl, err := daemon.DialWithin(expired.socket, time.Second, 3*time.Second)
		if err != nil {
			restore()
			return Binding{}, errors.New("만료된 도구 연결을 해제하지 못했습니다. 헬퍼와 컴패니언 연결을 확인해 주세요.")
		}
		_, err = cl.DetachMCP(expired.Session, s.Config.App)
		cl.Close()
		if err != nil {
			restore()
			return Binding{}, err
		}
	}
	sock, err := s.socket(ctx, target)
	if err != nil {
		return Binding{}, err
	}
	cl, err := daemon.DialWithin(sock, 3*time.Second, 15*time.Second)
	if err != nil {
		return Binding{}, err
	}
	defer cl.Close()
	sid := ""
	rows, err := cl.Sessions()
	if err != nil {
		return Binding{}, err
	}
	for _, r := range rows {
		if r.For == s.Config.App+":"+target {
			sid = r.ID
			break
		}
	}
	if sid == "" {
		sid, err = cl.NewSessionKeepingFor(s.Config.App + ":" + target)
		if err != nil {
			return Binding{}, err
		}
	}
	id, err := randomID()
	if err != nil {
		return Binding{}, err
	}
	b := &Binding{ID: id, Target: target, Session: sid, socket: sock, pending: make(chan *toolCall, 8), lastPoll: time.Now()}
	s.mu.Lock()
	s.bindings[id] = b
	s.mu.Unlock()
	if len(s.Config.Tools) > 0 {
		_, err = cl.AttachMCP(sid, s.Config.App, "http://"+s.Config.Addr+"/mcp/"+id+"/"+sid, map[string]string{"Authorization": "Bearer " + s.Token})
		if err != nil {
			s.mu.Lock()
			delete(s.bindings, id)
			s.mu.Unlock()
			return Binding{}, err
		}
	}
	if ctx.Err() != nil {
		if _, cleanupErr := cl.DetachMCP(sid, s.Config.App); cleanupErr != nil {
			log.Print("appbridge: cancelled connection tool cleanup failed")
		}
		s.mu.Lock()
		delete(s.bindings, id)
		s.mu.Unlock()
		return Binding{}, ctx.Err()
	}
	s.mu.Lock()
	s.targets[target] = id
	s.mu.Unlock()
	return *b, nil
}
func (s *Server) connectHTTP(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Target string `json:"target"`
	}
	if !decode(w, r, &p) {
		return
	}
	b, e := s.Connect(r.Context(), p.Target)
	if e != nil {
		fail(w, e)
		return
	}
	if r.Context().Err() != nil {
		if cleanupErr := s.Disconnect(context.Background(), b.ID); cleanupErr != nil {
			log.Print("appbridge: cancelled connection cleanup failed")
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(b); err != nil && (s.Config.App == "figma" || s.Config.App == "browser") {
		if cleanupErr := s.Disconnect(context.Background(), b.ID); cleanupErr != nil {
			log.Print("appbridge: undelivered connection cleanup failed")
		}
	}
}
func (s *Server) Request(ctx context.Context, id string, p Request) (json.RawMessage, error) {
	b, err := s.binding(id)
	if err != nil {
		return nil, err
	}
	method := p.Method
	switch method {
	case "say", "prompt":
		method = "submit"
	case "new":
		method = "session-new"
	case "model":
		method = "set-model"
	case "reply":
		method = "answer"
	case "answer":
		if p.Decision != "" {
			method = "permission"
		}
	case "permission", "interrupt", "status", "models", "context", "compact", "cursor":
	default:
		return nil, errors.New("지원하지 않는 클라이언트 명령입니다.")
	}
	cl, err := daemon.DialWithin(b.socket, 3*time.Second, 15*time.Second)
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	if method == "cursor" {
		events, e := cl.History(b.Session)
		if e != nil {
			return nil, e
		}
		var seq int64
		for _, ev := range events {
			if ev.Seq > seq {
				seq = ev.Seq
			}
		}
		return json.Marshal(map[string]any{"ok": true, "cursor": seq})
	}
	if method == "session-new" {
		s.connectMu.Lock()
		defer s.connectMu.Unlock()
		current, e := s.binding(id)
		if e != nil {
			return nil, e
		}
		if current.Session != b.Session {
			return nil, errors.New("세션이 이미 변경되었습니다. 다시 요청해 주세요.")
		}
		sid, e := cl.NewSessionKeepingFor(s.Config.App + ":" + b.Target)
		if e != nil {
			return nil, e
		}
		// Retire old calls before new attach; old queued polls must never execute.
		s.mu.Lock()
		s.bindings[id].Session = sid
		s.failCallsLocked(id, "세션이 변경되어 도구 요청이 취소되었습니다.")
		s.mu.Unlock()
		if len(s.Config.Tools) > 0 {
			_, e = cl.DetachMCP(b.Session, s.Config.App)
			if e == nil {
				_, e = cl.AttachMCP(sid, s.Config.App, "http://"+s.Config.Addr+"/mcp/"+id+"/"+sid, map[string]string{"Authorization": "Bearer " + s.Token})
			}
			if e != nil {
				s.mu.Lock()
				s.bindings[id].Session = b.Session
				s.mu.Unlock()
				_, restore := cl.AttachMCP(b.Session, s.Config.App, "http://"+s.Config.Addr+"/mcp/"+id+"/"+b.Session, map[string]string{"Authorization": "Bearer " + s.Token})
				if restore != nil {
					return nil, errors.New("새 세션 연결과 기존 도구 복구에 실패했습니다. 연결을 종료하고 다시 연결해 주세요.")
				}
				return nil, e
			}
		}
		return json.Marshal(map[string]any{"ok": true, "session": sid})
	}
	if method == "permission" && p.Decision != "allow" && p.Decision != "deny" && p.Decision != "always" {
		return nil, errors.New("승인 응답이 올바르지 않습니다.")
	}
	req := daemon.Request{Method: method, Session: b.Session, Text: p.Text, Name: p.Name, CallID: p.CallID, Decision: p.Decision, Answer: p.Answer}
	raw, _ := json.Marshal(req)
	out, err := cl.Raw(raw)
	if err != nil {
		return nil, err
	}
	var reply struct {
		OK  bool   `json:"ok"`
		Err string `json:"err"`
	}
	if json.Unmarshal(out, &reply) != nil {
		return nil, errors.New("Magi 응답이 올바르지 않습니다.")
	}
	if !reply.OK {
		return nil, errors.New(reply.Err)
	}
	return out, nil
}
func (s *Server) requestHTTP(w http.ResponseWriter, r *http.Request) {
	var p Request
	if !decode(w, r, &p) {
		return
	}
	out, e := s.Request(r.Context(), p.Binding, p)
	if e != nil {
		fail(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}
func (s *Server) Events(ctx context.Context, id string, since int64, each func(event.Event) bool) error {
	b, err := s.binding(id)
	if err != nil {
		return err
	}
	cl, err := daemon.DialWithin(b.socket, 3*time.Second, 0)
	if err != nil {
		return err
	}
	defer cl.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			cl.Close()
		case <-done:
		}
	}()
	return cl.Follow(b.Session, since, daemon.Tail{Each: func(e event.Event) bool {
		current, err := s.binding(id)
		if err != nil || current.Session != b.Session {
			return false
		}
		return each(e)
	}})
}
func (s *Server) eventsHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "GET required", 405)
		return
	}
	id := r.URL.Query().Get("binding")
	if _, e := s.binding(id); e != nil {
		fail(w, e)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unavailable", 500)
		return
	}
	since := int64(0)
	if raw := r.URL.Query().Get("since"); raw != "" {
		var err error
		since, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || since < 0 {
			http.Error(w, "잘못된 이벤트 커서입니다.", 400)
			return
		}
	}
	// Flush before subscribing: an idle session must not make fetch wait for its first event.
	f.Flush()
	err := s.Events(r.Context(), id, since, func(e event.Event) bool {
		data, _ := json.Marshal(e)
		_, err := fmt.Fprintf(w, "data: %s\n\n", data)
		f.Flush()
		return err == nil
	})
	if err != nil && r.Context().Err() == nil {
		data, _ := json.Marshal(map[string]any{"type": "connection.error", "message": "Magi 연결이 끊겼습니다. 다시 연결해 주세요."})
		fmt.Fprintf(w, "data: %s\n\n", data)
		f.Flush()
	}
}
func (s *Server) pollEventsHTTP(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Binding string `json:"binding"`
		Since   int64  `json:"since"`
	}
	if !decode(w, r, &p) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	events := []event.Event{}
	err := s.Events(ctx, p.Binding, p.Since, func(e event.Event) bool { events = append(events, e); return len(events) < 100 && e.Seq > 0 })
	if err != nil && ctx.Err() == nil {
		fail(w, errors.New("Magi 이벤트 연결에 실패했습니다. 다시 연결해 주세요."))
		return
	}
	write(w, map[string]any{"events": events})
}
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{Addr: s.Config.Addr, Handler: s.Mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	err := srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
