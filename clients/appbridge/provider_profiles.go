package appbridge

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/config"
)

type providerProfile struct {
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	Model   string `json:"model"`
	HasKey  bool   `json:"hasKey"`
}

func (s *Server) providerProfilesHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		settingsStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "GET 또는 POST 요청을 사용해 주세요."})
		return
	}
	note := ""
	if r.Method == http.MethodPost {
		var in struct {
			Binding  string `json:"binding"`
			Op       string `json:"op"`
			Name     string `json:"name"`
			BaseURL  string `json:"baseUrl"`
			Model    string `json:"model"`
			APIKey   string `json:"apiKey"`
			ClearKey bool   `json:"clearKey"`
		}
		if !decode(w, r, &in) {
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		if !config.BareName(in.Name) {
			settingsStatus(w, 400, map[string]any{"error": "프로필 이름은 영문, 숫자, 하이픈, 밑줄만 사용할 수 있습니다."})
			return
		}
		section := "llm.profiles." + in.Name
		path := filepath.Join(s.Config.ConfigDir, "config.toml")
		var err error
		switch in.Op {
		case "apply":
			if _, e := s.Request(r.Context(), in.Binding, Request{Method: "use-profile", Name: in.Name}); e != nil {
				settingsStatus(w, 409, map[string]any{"error": "프로필을 적용하지 못했습니다. 대화 연결과 진행 중인 응답을 확인해 주세요."})
				return
			}
			note = "현재 대화에 적용했습니다. 다음 요청부터 저장된 엔드포인트, 키, 모델을 함께 사용합니다."
		case "delete":
			settingsMu.Lock()
			if _, loadErr := config.Load(s.Config.ConfigDir); loadErr != nil {
				settingsMu.Unlock()
				settingsStatus(w, 500, map[string]any{"error": "설정 파일을 읽지 못했습니다. 파일 형식을 확인해 주세요."})
				return
			}
			err = config.RemoveSection(path, section)
			settingsMu.Unlock()
			note = "프로필을 삭제했습니다. 이미 적용한 대화는 유지됩니다."
		case "save":
			endpoint := strings.TrimSpace(in.BaseURL)
			u, e := url.Parse(endpoint)
			if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.ForceQuery {
				settingsStatus(w, 400, map[string]any{"error": "엔드포인트에 사용자 정보, 쿼리 또는 # 조각이 없는 HTTP 또는 HTTPS 주소를 입력해 주세요."})
				return
			}
			model := strings.TrimSpace(in.Model)
			if model == "" {
				settingsStatus(w, 400, map[string]any{"error": "모델 이름을 입력해 주세요."})
				return
			}
			values := map[string]string{"base_url": endpoint, "model": model}
			if in.ClearKey {
				values["api_key"] = ""
			} else if key := strings.TrimSpace(in.APIKey); key != "" {
				values["api_key"] = key
			}
			settingsMu.Lock()
			err = config.SetKeysChecked(path, section, values)
			settingsMu.Unlock()
			note = "프로필을 저장했습니다. 프로바이더 목록에서 선택할 수 있습니다."
			if err == nil && in.Binding != "" {
				raw, e := s.Request(r.Context(), in.Binding, Request{Method: "status"})
				var st struct {
					Profile string `json:"profile"`
				}
				if e == nil && json.Unmarshal(raw, &st) == nil && st.Profile == in.Name {
					if _, e = s.Request(r.Context(), in.Binding, Request{Method: "use-profile", Name: in.Name}); e != nil {
						note = "프로필은 저장했지만 현재 대화에는 반영하지 못했습니다. 응답이 끝난 뒤 같은 프로필을 다시 선택해 주세요."
					} else {
						note = "프로필을 저장하고 현재 대화에 반영했습니다. 다음 요청부터 변경한 설정을 사용합니다."
					}
				}
			}
		default:
			settingsStatus(w, 400, map[string]any{"error": "저장 또는 삭제 작업을 지정해 주세요."})
			return
		}
		if err != nil {
			settingsStatus(w, 500, map[string]any{"error": "프로필을 저장하지 못했습니다. 설정 파일과 쓰기 권한을 확인해 주세요."})
			return
		}
	}
	cfg, err := config.Load(s.Config.ConfigDir)
	if err != nil {
		settingsStatus(w, 500, map[string]any{"error": "설정 파일을 읽지 못했습니다. 파일 형식을 확인해 주세요."})
		return
	}
	profiles := make([]providerProfile, 0, len(cfg.LLM.Profiles))
	for name, p := range cfg.LLM.Profiles {
		profiles = append(profiles, providerProfile{Name: name, BaseURL: p.BaseURL, Model: p.Model, HasKey: strings.TrimSpace(p.APIKey) != ""})
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	write(w, map[string]any{"profiles": profiles, "applicability": "session-apply", "note": note})
}
