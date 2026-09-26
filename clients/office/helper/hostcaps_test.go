package office

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 요구 표는 손의 소스에서 온다 — 손이 새 요구를 걸면 이 표도 같이 가야 한다. 인자 하나에 걸린 요구(`edit_table{merge}`)는
// 도구를 숨기는 근거가 아니므로 표에 없다.
func TestToolNeedsAreTheHandsOwn(t *testing.T) {
	for _, c := range []struct {
		src  string
		want map[string]apiNeed
	}{
		{"../../word/addin/src/adapter/WordHand.js", wordToolNeeds},
		{"../../excel/addin/src/adapter/ExcelHand.js", xlToolNeeds},
	} {
		b, err := os.ReadFile(c.src)
		if err != nil {
			t.Fatalf("손의 소스를 못 읽었다(%v)", err)
		}
		got := map[string]apiNeed{}
		for _, m := range regexp.MustCompile(`need\('([A-Za-z]+)', '([0-9.]+)', '([a-z_]+)'\)`).FindAllStringSubmatch(string(b), -1) {
			got[m[3]] = apiNeed{m[1], m[2]}
		}
		var diff []string
		for k, v := range got {
			if c.want[k] != v {
				diff = append(diff, "손에만/다름: "+k+" "+v.Set+" "+v.Version)
			}
		}
		for k := range c.want {
			if _, ok := got[k]; !ok {
				diff = append(diff, "표에만: "+k)
			}
		}
		sort.Strings(diff)
		if len(diff) > 0 {
			t.Errorf("%s 와 요구 표가 갈렸다:\n  %s", c.src, strings.Join(diff, "\n  "))
		}
	}
}

func listed(s *MCPServer) map[string]bool {
	out := map[string]bool{}
	for _, d := range s.toolDefs() {
		out[d["name"].(string)] = true
	}
	return out
}

// 2021 의 Word(1.3)에서 헬퍼가 대신 못 하는 곳이면 1.4 이상 도구는 목록에 없다. 대신할 수 있으면(Windows COM) 있다.
// 안 쟀으면 아무것도 숨기지 않는다. 인자 하나에 걸린 요구는 도구를 숨기지 않는다.
func TestToolsTheHostCannotDoAreNotAdvertised(t *testing.T) {
	was := wordComOnThisOS
	t.Cleanup(func() { wordComOnThisOS = was })
	word2021 := map[string]any{"measured": true, "sets": []map[string]any{
		{"name": "WordApi", "version": "1.3", "ok": true},
		{"name": "WordApi", "version": "1.4", "ok": false},
		{"name": "WordApi", "version": "1.5", "ok": false},
		{"name": "WordApiDesktop", "version": "1.2", "ok": false},
	}}
	s := &MCPServer{App: Word, HostCaps: func() map[string]any { return word2021 }}

	wordComOnThisOS = false // macOS 의 2021 — 대신할 길이 없다
	got := listed(s)
	for _, n := range []string{"add_comment", "insert_footnote", "insert_shape"} {
		if got[n] {
			t.Errorf("이 호스트가 못 하고 대신할 길도 없는 %s 를 광고했다", n)
		}
	}
	for _, n := range []string{"insert_paragraphs", "edit_table", "list_paragraphs"} {
		if !got[n] {
			t.Errorf("되는 도구 %s 를 숨겼다", n)
		}
	}

	wordComOnThisOS = true // Windows — COM 이 대신한다
	if got := listed(s); !got["add_comment"] || !got["insert_shape"] || !got["suggest"] {
		t.Error("헬퍼가 대신하는 도구를 숨겼다")
	}

	wordComOnThisOS = false
	unmeasured := &MCPServer{App: Word, HostCaps: func() map[string]any { return map[string]any{"measured": false} }}
	if got := listed(unmeasured); !got["add_comment"] {
		t.Error("안 잰 호스트에서 도구를 숨겼다 — 모르는 것을 못 한다로 읽었다")
	}
	if got := listed(&MCPServer{App: Word}); !got["add_comment"] {
		t.Error("잰 값이 없는데 도구를 숨겼다")
	}
	all := len(listed(&MCPServer{App: Word}))
	if hidden := all - len(listed(s)); hidden != 19 {
		t.Errorf("1.4·1.5·Desktop 1.2 가 없는 호스트에서 숨긴 수 %d — 1.6·Desktop 1.1 은 목록에 없으니 안 잰 것이다(기대 19 = 1.4 열 · 1.5 다섯 · Desktop 1.2 넷)", hidden)
	}
}

// PowerPoint 는 거르지 않는다 — 2021 에서 손은 작업창이 아니라 COM 어댑터다.
func TestPowerPointIsNotFilteredByThePanesCaps(t *testing.T) {
	nothing := map[string]any{"measured": true, "sets": []map[string]any{{"name": "PowerPointApi", "version": "1.2", "ok": false}}}
	s := &MCPServer{App: PPT, HostCaps: func() map[string]any { return nothing }}
	if len(listed(s)) != len(listed(&MCPServer{App: PPT})) {
		t.Error("파워포인트의 도구를 창의 잰 값으로 숨겼다")
	}
}
