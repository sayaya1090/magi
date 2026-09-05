package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// 열거형 밖의 값은 **어느 칸·어느 값·받는 값**을 대고 거절한다 — 호스트의 `InvalidArgument` 는 셋 다 안 말한다.
func TestEnumOutsideListIsRefusedByName(t *testing.T) {
	cases := []struct{ tool, raw, where, oneOf string }{
		{"format_shape", `{"slide":1,"shape_id":"s","valign":"center"}`, "valign", "Middle"},
		{"format_shape", `{"slide":1,"shape_id":"s","underline":true}`, "", ""}, // 타입은 checkType 이 잡는다
		{"move_shape", `{"slide":1,"shape_id":"s","z_order":"front"}`, "z_order", "BringToFront"},
		{"add_shape", `{"slide":1,"kind":"line","connector":"bent"}`, "connector", "Elbow"},
		{"add_shape", `{"slide":1,"kind":"rectangle","line_dash":"dotted"}`, "line_dash", "RoundDot"},
		{"format_shape", `{"slide":1,"shape_id":"s","autosize":"fit"}`, "autosize", "AutoSizeShapeToFitText"},
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
		{"format_shape", `{"slide":1,"shape_id":"s","valign":"MiddleCentered","underline":"Single","autosize":"AutoSizeTextToFitShape","line_dash":"Dash","line":"none","transparency":0.5}`},
		{"move_shape", `{"slide":1,"shape_id":"s","z_order":"SendToBack"}`},
		{"add_shape", `{"slide":1,"kind":"line","connector":"Curve","line_weight":2}`},
	}
	for _, c := range cases {
		if _, err := validateArgs(toolNamed(t, c.tool), json.RawMessage(c.raw)); err != nil {
			t.Fatalf("%s: 정상 값이 거절당했다: %v", c.tool, err)
		}
	}
	if n := len(tableStyles); n != 74 {
		t.Fatalf("TableStyle 은 1.9 판에서 74개인데 여기는 %d", n)
	}
}

// 열거형은 스키마 `enum` 으로 **광고**된다 — 모델이 첫 호출에 맞는 값을 고를 수 있어야 한다.
func TestEnumsAdvertisedInSchema(t *testing.T) {
	want := map[string]map[string]int{
		"format_shape": {"valign": 6, "autosize": 3, "underline": 17, "line_dash": 12},
		"move_shape":   {"z_order": 4},
		"add_shape":    {"connector": 3, "valign": 6},
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
