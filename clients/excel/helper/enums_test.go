package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// 열거형 밖의 값은 **어느 칸·어느 값·받는 값**을 대고 거절한다 — 호스트의 `InvalidArgument` 는 셋 다 안 말한다.
func TestEnumOutsideListIsRefusedByName(t *testing.T) {
	cases := []struct{ tool, raw, where, oneOf string }{
		{"format_range", `{"address":"B2","valign":"middle"}`, "valign", "Center"},
		{"format_range", `{"address":"B2","underline":"yes"}`, "", ""}, // 타입은 checkType 이 잡는다
		{"add_chart", `{"source":"A1:B2","chart_type":"bar"}`, "chart_type", "ColumnClustered"},
		{"clear_range", `{"address":"A1","what":"everything"}`, "what", "contents"},
		{"format_range", `{"address":"B2","border_style":"dotted"}`, "border_style", "Dot"},
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
		{"format_range", `{"address":"B2","valign":"Center","align":"Right","border_style":"Dash","borders":"none","column_width":12}`},
		{"add_chart", `{"source":"A1:B2","chart_type":"Line","series_by":"Rows"}`},
		{"insert_cells", `{"address":"3:3","shift":"down"}`},
	}
	for _, c := range cases {
		if _, err := validateArgs(toolNamed(t, c.tool), json.RawMessage(c.raw)); err != nil {
			t.Fatalf("%s: 정상 값이 거절당했다: %v", c.tool, err)
		}
	}
	if n := len(tableStyles); n != 60 {
		t.Fatalf("BuiltInTableStyle 은 Light 21·Medium 28·Dark 11 = 60개인데 여기는 %d", n)
	}
}

// 열거형은 스키마 `enum` 으로 **광고**된다 — 모델이 첫 호출에 맞는 값을 고를 수 있어야 한다.
func TestEnumsAdvertisedInSchema(t *testing.T) {
	want := map[string]map[string]int{
		"format_range": {"valign": 5, "align": 8, "border_style": 8},
		"add_chart":    {"chart_type": len(chartTypes), "series_by": 2},
		"clear_range":  {"what": 4},
		"autofit":      {"what": 3},
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
