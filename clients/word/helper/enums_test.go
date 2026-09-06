package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// 열거형 밖의 값은 **어느 칸·어느 값·받는 값**을 대고 거절한다 — 호스트의 `InvalidArgument` 는 셋 다 안 말한다.
func TestEnumOutsideListIsRefusedByName(t *testing.T) {
	cases := []struct{ tool, raw, where, oneOf string }{
		{"format_paragraph", `{"from":1,"align":"middle"}`, "align", "Centered"},
		{"format_text", `{"from":1,"bold":"yes"}`, "", ""}, // 타입은 checkType 이 잡는다
		{"format_text", `{"from":1,"underline":"wavy"}`, "underline", "Single"},
		{"set_style", `{"from":1,"builtin":"Heading 1"}`, "builtin", "Heading1"},
		{"review_changes", `{"what":"everything"}`, "what", "accept"},
		{"insert_table", `{"values":[["a"]],"table_style":"fancy"}`, "table_style", "TableGrid"},
	}
	for _, c := range cases {
		_, err := validateArgs(toolNamed(t, c.tool), json.RawMessage(c.raw))
		if err == nil {
			t.Fatalf("%s: 열거형 밖의 값이 통과했다: %s", c.tool, c.raw)
		}
		if c.where == "" {
			continue
		}
		for _, want := range []string{c.tool + ":", c.where, c.oneOf, "did not run"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("%s: 거절문에 %q 가 없다: %s", c.tool, want, err)
			}
		}
	}
}

func TestEnumInsideListPasses(t *testing.T) {
	cases := []struct{ tool, raw string }{
		{"format_text", `{"from":1,"to":3,"underline":"Single","highlight":"Yellow","size":12,"color":"#FF0000"}`},
		{"format_paragraph", `{"from":1,"align":"Justified","space_after":6}`},
		{"insert_list", `{"items":["a","b"],"kind":"numbered","after":2}`},
		{"insert_break", `{"paragraph":1,"kind":"section"}`},
		{"set_track_changes", `{"mode":"TrackAll"}`},
		{"set_header_footer", `{"which":"footer","text":"p","kind":"FirstPage"}`},
		{"insert_table", `{"values":[["a","b"]],"table_style":"GridTable4_Accent1"}`},
		// suggest 의 what 은 문장이다 — review_changes 의 what 열거와 이름만 같다(엑셀 판 실물에서 겪은 자리).
		{"suggest", `{"what":"제목을 Heading 1 로","fix":{"tool":"set_style","args":{"from":1,"builtin":"Heading1"}}}`},
	}
	for _, c := range cases {
		if _, err := validateArgs(toolNamed(t, c.tool), json.RawMessage(c.raw)); err != nil {
			t.Fatalf("%s: 정상 값이 거절당했다: %v", c.tool, err)
		}
	}
	if n := len(tableStyles); n != 7+2*7*7 {
		t.Fatalf("표 스타일은 Plain·Grid 7 + Grid/List Table 7×7 = 105개인데 여기는 %d", n)
	}
}

// 열거형은 스키마 `enum` 으로 **광고**된다 — 모델이 첫 호출에 맞는 값을 고를 수 있어야 한다.
func TestEnumsAdvertisedInSchema(t *testing.T) {
	want := map[string]map[string]int{
		"format_text":       {"underline": 7, "highlight": 16},
		"format_paragraph":  {"align": 4},
		"set_style":         {"builtin": len(builtinStyles)},
		"review_changes":    {"what": 2},
		"insert_table":      {"table_style": len(tableStyles)},
		"set_track_changes": {"mode": 3},
	}
	for name, props := range want {
		var schema struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(schemaOf(toolNamed(t, name)), &schema); err != nil {
			t.Fatal(err)
		}
		for p, n := range props {
			if got := len(schema.Properties[p].Enum); got != n {
				t.Fatalf("%s.%s: enum %d개 광고 — %d개여야 한다", name, p, got, n)
			}
		}
	}
}
