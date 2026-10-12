// Package browser provides one small, stable tool for on-demand browser access.
package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

type accessLease struct {
	record  Runtime
	id      string
	socket  string
	expires time.Time
}
type accessState struct {
	mu        sync.Mutex
	lease     accessLease
	connected bool
	users     int
}
type Access struct {
	ConfigDir string
	Socket    func(port.ToolEnv) string
	mu        sync.Mutex
	states    map[string]*accessState
}

func (*Access) Name() string { return "browser" }
func (*Access) Description() string {
	return "Use a browser on demand without loading its full tool catalog. connect returns tool schemas; call forwards a listed tool and its original result; release ends access. Default headless is for isolated tests. extension uses explicitly user-granted Chrome tabs. Test delegation may use hand_off to a browser companion. Never switch modes silently. Examples: browser({action:\"connect\"}); browser({action:\"call\",tool:\"read_page\",args:{tabId:1}}); browser({action:\"release\"}). action is connect/call/release, tool is the returned backend name, never browser. Put backend arguments directly in args, without payload or method_name wrappers."
}
func (*Access) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["connect","call","release"],"description":"connect to fetch schemas; call to execute a backend tool; release to close access"},"mode":{"type":"string","enum":["headless","extension"]},"tool":{"type":"string","description":"For action=call: returned tool name, e.g. read_page, fill or click. Never browser."},"args":{"type":"object","description":"Backend arguments directly, e.g. {tabId:1}. No method_name/payload wrapper."}},"required":["action"],"additionalProperties":false}`)
}
func browserResult(value any, err error) (session.ToolResult, error) {
	if err != nil {
		data, _ := json.Marshal(err.Error())
		return session.ToolResult{Content: data, IsError: true}, nil
	}
	data, e := json.Marshal(value)
	if e != nil {
		return browserResult(nil, errors.New("cannot encode browser response"))
	}
	return session.ToolResult{Content: data}, nil
}
func post(ctx context.Context, r Runtime, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	method := "POST"
	if path == "/api/browser/available" {
		method = "GET"
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(r.URL, "/")+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.Token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 45 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	res, err := client.Do(req)
	if err != nil {
		return errors.New("browser helper connection failed or was interrupted; verify the page before retrying")
	}
	defer res.Body.Close()
	data, err = io.ReadAll(io.LimitReader(res.Body, 65537))
	if err != nil || len(data) > 65536 {
		return errors.New("browser response exceeded its limit")
	}
	if res.StatusCode != 200 {
		message := strings.ReplaceAll(string(data), r.Token, "[redacted]")
		if len(message) > 4000 {
			message = message[:4000]
		}
		return errors.New("browser helper: " + strings.TrimSpace(message))
	}
	if out == nil {
		return nil
	}
	if json.Unmarshal(data, out) != nil {
		return errors.New("invalid browser helper response")
	}
	return nil
}
func (a *Access) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var in struct {
		Action, Mode, Tool string
		Args               json.RawMessage
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil {
		return browserResult(nil, errors.New("invalid browser arguments"))
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return browserResult(nil, errors.New("one browser request is required"))
	}
	if in.Mode == "" {
		in.Mode = "headless"
	}
	if _, err := runtimePath(a.ConfigDir, in.Mode); err != nil {
		return browserResult(nil, err)
	}
	if env.SessionID == "" {
		return browserResult(nil, errors.New("browser access needs an owning session"))
	}
	key := string(env.SessionID) + "\x00" + in.Mode
	// A stalled Chrome confirmation must not hold other sessions behind this tool.
	a.mu.Lock()
	for existing, idle := range a.states {
		if idle.users == 0 && (!idle.connected || time.Now().After(idle.lease.expires)) {
			delete(a.states, existing)
		}
	}
	if a.states == nil {
		a.states = map[string]*accessState{}
	}
	state := a.states[key]
	if state == nil {
		state = &accessState{}
		a.states[key] = state
	}
	state.users++
	a.mu.Unlock()
	state.mu.Lock()
	defer func() {
		a.mu.Lock()
		state.users--
		if state.users == 0 && (!state.connected || time.Now().After(state.lease.expires)) {
			delete(a.states, key)
		}
		a.mu.Unlock()
		state.mu.Unlock()
	}()
	lease, connected := state.lease, state.connected
	if connected && time.Now().After(lease.expires) {
		state.connected = false
		connected = false
	}
	switch in.Action {
	case "connect":
		if connected {
			current, e := readRuntime(a.ConfigDir, in.Mode)
			if e != nil || current != lease.record {
				state.connected = false
				connected = false
			}
		}
		if connected {
			return browserResult(nil, errors.New("browser already connected; use call, or release before reconnecting"))
		}
		record, err := readRuntime(a.ConfigDir, in.Mode)
		if err != nil {
			return browserResult(nil, err)
		}
		var available struct {
			Bindings []struct {
				ID string `json:"binding"`
			} `json:"bindings"`
		}
		if err = post(ctx, record, "/api/browser/available", map[string]any{}, &available); err != nil {
			return browserResult(nil, err)
		}
		if len(available.Bindings) == 0 {
			return browserResult(nil, errors.New("open the Chrome panel and connect before requesting extension tools"))
		}
		if len(available.Bindings) != 1 {
			return browserResult(nil, errors.New("multiple browser panels are connected; close unused panels before connecting"))
		}
		socket := daemon.SocketPath(a.ConfigDir, env.Workdir)
		if a.Socket != nil {
			socket = a.Socket(env)
		}
		var attached struct {
			Lease     string          `json:"lease"`
			Tools     json.RawMessage `json:"tools"`
			ExpiresAt string          `json:"expiresAt"`
		}
		err = post(ctx, record, "/api/browser/attach", map[string]any{"binding": available.Bindings[0].ID, "socket": socket, "session": env.SessionID, "proxy": true}, &attached)
		if err != nil {
			return browserResult(nil, err)
		}
		if attached.Lease == "" {
			return browserResult(nil, errors.New("browser helper returned no lease"))
		}
		expires, parseErr := time.Parse(time.RFC3339Nano, attached.ExpiresAt)
		if parseErr != nil {
			_ = post(ctx, record, "/api/browser/release", map[string]any{"lease": attached.Lease}, nil)
			return browserResult(nil, errors.New("browser helper returned no valid expiry"))
		}
		state.lease = accessLease{record: record, id: attached.Lease, socket: socket, expires: expires}
		state.connected = true
		return browserResult(map[string]any{"mode": in.Mode, "tools": attached.Tools, "expiresAt": attached.ExpiresAt}, nil)
	case "release":
		if !connected {
			return browserResult(map[string]bool{"released": false}, nil)
		}
		err := post(ctx, lease.record, "/api/browser/release", map[string]any{"lease": lease.id}, nil)
		// Retire local access even if the helper expired or restarted.
		state.connected = false
		state.lease = accessLease{}
		return browserResult(map[string]bool{"released": err == nil}, err)
	case "call":
		if !connected {
			return browserResult(nil, errors.New("call browser connect first to obtain tool schemas"))
		}
		if in.Tool == "" || len(in.Args) == 0 || bytes.Equal(in.Args, []byte("null")) {
			return browserResult(nil, errors.New("tool and object args are required"))
		}
		var args map[string]any
		if json.Unmarshal(in.Args, &args) != nil {
			return browserResult(nil, errors.New("browser args must be an object"))
		}
		var out struct {
			Result    json.RawMessage `json:"result"`
			Error     string          `json:"error"`
			ExpiresAt string          `json:"expiresAt"`
		}
		err := post(ctx, lease.record, "/api/browser/call", map[string]any{"lease": lease.id, "session": env.SessionID, "socket": lease.socket, "name": in.Tool, "args": in.Args}, &out)
		if err != nil {
			state.connected = false
			return browserResult(nil, errors.New(err.Error()+"; access was retired locally, reconnect and verify before retrying"))
		}
		if out.ExpiresAt != "" {
			if expires, e := time.Parse(time.RFC3339Nano, out.ExpiresAt); e == nil {
				state.lease.expires = expires
			}
		}
		if out.Error != "" {
			return browserResult(nil, errors.New(out.Error))
		}
		if len(out.Result) == 0 {
			return browserResult(nil, errors.New("browser helper returned no result"))
		}
		return session.ToolResult{Content: out.Result}, nil
	default:
		return browserResult(nil, errors.New("browser action must be connect, call or release. To read a page use {action:\"call\",tool:\"read_page\",args:{tabId:1}}"))
	}
}
