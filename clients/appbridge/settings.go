package appbridge

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/atomicfile"
	"github.com/sayaya1090/magi/internal/config"
)

var settingsMu sync.Mutex

func settingsStatus(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	write(w, body)
}
func (s *Server) registerSettingsRoutes() {
	for path, handler := range map[string]http.HandlerFunc{
		"/api/provider-profiles": s.providerProfilesHTTP,
		"/api/instructions":      s.instructionsHTTP,
		"/api/guides":            s.guidesHTTP,
		"/api/guide":             s.guideHTTP,
		"/api/council":           s.councilHTTP,
	} {
		s.Mux.Handle(path, s.Auth(handler))
	}
}

func (s *Server) settingsWorkspace(id string) (string, error) {
	b, err := s.binding(id)
	if err != nil {
		return "", err
	}
	// External attachments belong to another workspace and must not write settings here.
	if b.parent != "" {
		return "", errors.New("외부 연결에서는 이 설정을 바꿀 수 없습니다.")
	}
	h := sha256.Sum256([]byte(b.Target))
	wd := filepath.Join(s.Config.ConfigDir, "clients", s.Config.App, hex.EncodeToString(h[:16]))
	for _, p := range []string{wd, filepath.Join(wd, ".magi"), filepath.Join(wd, ".magi", "skills"), filepath.Join(wd, ".magi", "skills", "off"), filepath.Join(wd, "AGENTS.md")} {
		if fi, e := os.Lstat(p); e == nil && fi.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("심볼릭 링크 설정 경로는 사용할 수 없습니다.")
		}
	}
	return wd, nil
}

func (s *Server) instructionsHTTP(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Binding string `json:"binding"`
		Text    string `json:"text"`
	}
	in.Binding = r.URL.Query().Get("binding")
	if r.Method != "GET" && !decode(w, r, &in) {
		return
	}
	wd, err := s.settingsWorkspace(in.Binding)
	if err != nil {
		fail(w, err)
		return
	}
	path := filepath.Join(wd, "AGENTS.md")
	settingsMu.Lock()
	defer settingsMu.Unlock()
	if r.Method == "POST" {
		if !utf8.ValidString(in.Text) || utf8.RuneCountInString(in.Text) > 8000 {
			fail(w, errors.New("지시는 유효한 텍스트 8,000자 이내로 작성해 주세요."))
			return
		}
		if err = os.MkdirAll(wd, 0700); err == nil {
			err = atomicfile.Write(path, []byte(in.Text), 0600)
		}
		if err != nil {
			fail(w, errors.New("지시를 저장하지 못했습니다."))
			return
		}
	}
	body, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		fail(w, errors.New("지시를 읽지 못했습니다."))
		return
	}
	write(w, map[string]any{"text": string(body), "note": "저장한 지시는 다음 요청부터 반영됩니다."})
}

type settingGuide struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Chars       int    `json:"chars"`
}

func guidePaths(wd, name string) (string, string) {
	dir := filepath.Join(wd, ".magi", "skills")
	return filepath.Join(dir, name+".md"), filepath.Join(dir, "off", name+".md")
}
func guideList(wd string) ([]settingGuide, error) {
	out := []settingGuide{}
	for _, enabled := range []bool{true, false} {
		dir := filepath.Join(wd, ".magi", "skills")
		if !enabled {
			dir = filepath.Join(dir, "off")
		}
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				return nil, err
			}
			text := string(body)
			description := settingsGuideDescription(text)
			out = append(out, settingGuide{Name: strings.TrimSuffix(entry.Name(), ".md"), Description: description, Enabled: enabled, Chars: utf8.RuneCountInString(text)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (s *Server) guidesHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		settingsStatus(w, 405, map[string]string{"error": "GET 요청을 사용해 주세요."})
		return
	}
	wd, err := s.settingsWorkspace(r.URL.Query().Get("binding"))
	if err != nil {
		fail(w, err)
		return
	}
	settingsMu.Lock()
	defer settingsMu.Unlock()
	guides, err := guideList(wd)
	if err != nil {
		fail(w, errors.New("스킬 목록을 읽지 못했습니다."))
		return
	}
	write(w, map[string]any{"guides": guides, "note": "스킬 변경은 다음 요청부터 반영됩니다."})
}
func (s *Server) guideHTTP(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Binding string `json:"binding"`
		Name    string `json:"name"`
		Op      string `json:"op"`
		Body    string `json:"body"`
	}
	in.Binding = r.URL.Query().Get("binding")
	in.Name = r.URL.Query().Get("name")
	if r.Method != "GET" && !decode(w, r, &in) {
		return
	}
	if !config.BareName(in.Name) || len(in.Name) > 60 || in.Name == "off" {
		fail(w, errors.New("스킬 이름은 영문, 숫자, 하이픈, 밑줄로 60자 이내로 작성해 주세요."))
		return
	}
	wd, err := s.settingsWorkspace(in.Binding)
	if err != nil {
		fail(w, err)
		return
	}
	on, off := guidePaths(wd, in.Name)
	settingsMu.Lock()
	defer settingsMu.Unlock()
	path := on
	enabled := true
	if _, e := os.Stat(on); os.IsNotExist(e) {
		if _, e = os.Stat(off); e == nil {
			path = off
			enabled = false
		}
	}
	// User-controlled filesystem aliases must not redirect settings out of the owned workspace.
	for _, p := range []string{on, off} {
		if fi, e := os.Lstat(p); e == nil && fi.Mode()&os.ModeSymlink != 0 {
			fail(w, errors.New("심볼릭 링크 스킬은 수정할 수 없습니다."))
			return
		}
	}
	if r.Method == "POST" {
		switch in.Op {
		case "save":
			if !utf8.ValidString(in.Body) || utf8.RuneCountInString(in.Body) > 40000 {
				fail(w, errors.New("스킬은 유효한 텍스트 40,000자 이내로 작성해 주세요."))
				return
			}
			if err = os.MkdirAll(filepath.Dir(path), 0700); err == nil {
				err = atomicfile.Write(path, []byte(in.Body), 0600)
			}
		case "delete":
			err = os.Remove(path)
			if os.IsNotExist(err) {
				err = nil
			}
		case "enable", "disable":
			dest := on
			if in.Op == "disable" {
				dest = off
			}
			if path != dest {
				if _, e := os.Stat(dest); e == nil {
					fail(w, errors.New("동일한 이름의 스킬이 이미 있습니다."))
					return
				}
				if err = os.MkdirAll(filepath.Dir(dest), 0700); err == nil {
					err = os.Rename(path, dest)
				}
				path = dest
				enabled = in.Op == "enable"
			}
		default:
			fail(w, errors.New("스킬 작업이 올바르지 않습니다."))
			return
		}
		if err != nil {
			fail(w, errors.New("스킬을 저장하지 못했습니다."))
			return
		}
	}
	body, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		fail(w, errors.New("스킬을 읽지 못했습니다."))
		return
	}
	write(w, map[string]any{"name": in.Name, "body": string(body), "enabled": enabled, "note": "스킬 변경은 다음 요청부터 반영됩니다."})
}
func (s *Server) councilHTTP(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Binding string `json:"binding"`
		Enabled *bool  `json:"enabled"`
	}
	in.Binding = r.URL.Query().Get("binding")
	if r.Method != "GET" && !decode(w, r, &in) {
		return
	}
	wd, err := s.settingsWorkspace(in.Binding)
	if err != nil {
		fail(w, err)
		return
	}
	dir := config.CompanionDir(s.Config.ConfigDir, daemon.WorkspaceKey(wd))
	path := filepath.Join(dir, "config.toml")
	settingsMu.Lock()
	defer settingsMu.Unlock()
	if r.Method == "POST" {
		if in.Enabled == nil {
			fail(w, errors.New("카운슬 활성 여부를 지정해 주세요."))
			return
		}
		raw := "false"
		if *in.Enabled {
			raw = "true"
		}
		if err = config.SetRawKey(path, "council", "enabled", raw); err != nil {
			fail(w, errors.New("카운슬 설정을 저장하지 못했습니다."))
			return
		}
	}
	enabled := true
	for _, layer := range []string{s.Config.ConfigDir, filepath.Join(wd, ".magi"), dir} {
		cfg, e := config.Load(layer)
		if e != nil {
			fail(w, errors.New("카운슬 설정을 읽지 못했습니다."))
			return
		}
		if cfg.Council.Enabled != nil {
			enabled = *cfg.Council.Enabled
		}
	}
	write(w, map[string]any{"enabled": enabled, "applicability": "next-start", "note": "카운슬 설정은 다음 데몬 시작부터 반영됩니다. 실행 중인 대화는 바뀌지 않습니다."})
}

func settingsGuideDescription(text string) string {
	body := strings.TrimSpace(text)
	if strings.HasPrefix(body, "---") {
		if end := strings.Index(body[3:], "\n---"); end >= 0 {
			for _, line := range strings.Split(body[3:3+end], "\n") {
				if value, ok := strings.CutPrefix(strings.TrimSpace(line), "description:"); ok {
					return strings.Trim(strings.TrimSpace(value), "\"'")
				}
			}
		}
	}
	return strings.SplitN(body, "\n", 2)[0]
}
