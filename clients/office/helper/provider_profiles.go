package office

import (
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

func (a *API) providerProfiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		writeStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "GET 또는 POST 요청을 사용해 주세요."})
		return
	}
	note := ""
	if r.Method == http.MethodPost {
		var in struct {
			Op       string `json:"op"`
			Name     string `json:"name"`
			BaseURL  string `json:"baseUrl"`
			Model    string `json:"model"`
			APIKey   string `json:"apiKey"`
			ClearKey bool   `json:"clearKey"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		if !config.BareName(in.Name) {
			writeStatus(w, 400, map[string]any{"error": "프로필 이름은 영문, 숫자, 하이픈, 밑줄만 사용할 수 있습니다."})
			return
		}
		section := "llm.profiles." + in.Name
		path := filepath.Join(a.ConfigDir, "config.toml")
		var err error
		switch in.Op {
		case "delete":
			if _, loadErr := config.Load(a.ConfigDir); loadErr != nil {
				writeStatus(w, 500, map[string]any{"error": "설정 파일을 읽지 못했습니다. 파일 형식을 확인해 주세요."})
				return
			}
			err = config.RemoveSection(path, section)
			note = "프로필을 삭제했습니다. 실행 중인 대화에는 적용되지 않으며, 다음 데몬 시작부터 반영됩니다."
		case "save":
			endpoint := strings.TrimSpace(in.BaseURL)
			u, e := url.Parse(endpoint)
			if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.ForceQuery {
				writeStatus(w, 400, map[string]any{"error": "엔드포인트에 사용자 정보, 쿼리 또는 # 조각이 없는 HTTP 또는 HTTPS 주소를 입력해 주세요."})
				return
			}
			model := strings.TrimSpace(in.Model)
			if model == "" {
				writeStatus(w, 400, map[string]any{"error": "모델 이름을 입력해 주세요."})
				return
			}
			values := map[string]string{"base_url": endpoint, "model": model}
			if in.ClearKey {
				values["api_key"] = ""
			} else if key := strings.TrimSpace(in.APIKey); key != "" {
				values["api_key"] = key
			}
			err = config.SetKeysChecked(path, section, values)
			note = "프로필을 저장했습니다. 실행 중인 모델은 바뀌지 않습니다. 다음 데몬 시작부터 사용할 수 있습니다."
		default:
			writeStatus(w, 400, map[string]any{"error": "저장 또는 삭제 작업을 지정해 주세요."})
			return
		}
		if err != nil {
			writeStatus(w, 500, map[string]any{"error": "프로필을 저장하지 못했습니다. 설정 파일과 쓰기 권한을 확인해 주세요."})
			return
		}
	}
	cfg, err := config.Load(a.ConfigDir)
	if err != nil {
		writeStatus(w, 500, map[string]any{"error": "설정 파일을 읽지 못했습니다. 파일 형식을 확인해 주세요."})
		return
	}
	profiles := make([]providerProfile, 0, len(cfg.LLM.Profiles))
	for name, p := range cfg.LLM.Profiles {
		profiles = append(profiles, providerProfile{Name: name, BaseURL: p.BaseURL, Model: p.Model, HasKey: strings.TrimSpace(p.APIKey) != ""})
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	writeJSON(w, map[string]any{"profiles": profiles, "applicability": "next-start", "note": note})
}
