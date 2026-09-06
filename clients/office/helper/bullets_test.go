package office

import (
	"encoding/json"
	"strings"
	"testing"
)

// 열거형 밖의 이름은 **어느 칸·어느 값**인지 이름을 대고 거절한다. 실물에서는 `bulletChromaDot`
// 하나가 슬라이드 7장짜리 add_slides 를 `InvalidArgument` 한 단어로 죽였다(2026-09-05).
func TestBulletStyleOutsideEnumIsRefusedByName(t *testing.T) {
	cases := []struct{ tool, raw, where string }{
		{"format_shape", `{"slide":1,"shape_id":"s1","bullet_style":"bulletChromaDot"}`, "bullet_style"},
		{"add_slides", `{"slides":[{"title":"a"},{"title":"b","bullet":true,"bullet_style":"bulletChromaDot"}]}`, "slides[1].bullet_style"},
		{"apply_style", `{"body":{"bullet":true,"bullet_style":"bulletChromaDot"}}`, "body.bullet_style"},
		{"format_shape", `{"slide":1,"shape_id":"s1","bullet_type":"Dot"}`, "bullet_type"},
	}
	for _, c := range cases {
		_, err := validateArgs(PPT, toolOf(t, PPT, c.tool), json.RawMessage(c.raw))
		if err == nil {
			t.Fatalf("%s: 열거형 밖의 값이 통과했다 — 호스트가 배치 전체를 InvalidArgument 로 돌려보낸다: %s", c.tool, c.raw)
		}
		msg := err.Error()
		for _, want := range []string{c.tool + ":", c.where, "did not run"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("%s: 거절문에 %q 가 없다 — 모델이 어느 칸을 고칠지 모른다: %s", c.tool, want, msg)
			}
		}
		if c.where != "bullet_type" && !strings.Contains(msg, "bullet:true") {
			t.Fatalf("거절문이 대신 쓸 것(bullet:true)을 안 말한다: %s", msg)
		}
	}
}

// 열거형 안의 이름은 어느 깊이에서든 그대로 지나간다 — 거르는 것이 정상 호출을 막으면 안 된다.
func TestBulletStyleInsideEnumPasses(t *testing.T) {
	cases := []struct{ tool, raw string }{
		{"format_shape", `{"slide":1,"shape_id":"s1","bullet_type":"Numbered","bullet_style":"ArabicNumeralPeriod"}`},
		{"add_slides", `{"slides":[{"title":"a","bullet_style":"RomanUppercasePeriod"}]}`},
		{"apply_style", `{"title":{"bullet":false},"all":{"bullet_type":"None"}}`},
	}
	for _, c := range cases {
		if _, err := validateArgs(PPT, toolOf(t, PPT, c.tool), json.RawMessage(c.raw)); err != nil {
			t.Fatalf("%s: 정상 값이 거절당했다: %v", c.tool, err)
		}
	}
	if len(pptBulletStyles) != 41 {
		t.Fatalf("BulletStyle 열거형은 1.10 판에서 41개다(Unsupported 제외), 여기는 %d", len(pptBulletStyles))
	}
}

// 목록은 **광고**돼야 한다 — 모델이 스키마에서 값을 고를 수 있어야 거절이 아니라 첫 호출이 맞는다.
func TestBulletEnumIsAdvertisedInSchema(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schemaOf(PPT, toolOf(t, PPT, "format_shape")), &schema); err != nil {
		t.Fatal(err)
	}
	if got := schema.Properties["bullet_style"].Enum; len(got) != len(pptBulletStyles) {
		t.Fatalf("bullet_style 의 enum 이 스키마에 %d개 — 목록은 %d개", len(got), len(pptBulletStyles))
	}
	if got := schema.Properties["bullet_type"].Enum; len(got) != 3 {
		t.Fatalf("bullet_type 의 enum 이 스키마에 %d개", len(got))
	}
	// add_slides 는 항목이 object 라 enum 을 못 싣는다 — 설명이 두 칸을 이름 대야 한다.
	desc := string(schemaOf(PPT, toolOf(t, PPT, "add_slides")))
	if !strings.Contains(desc, "bullet_style") {
		t.Fatalf("add_slides 가 받는 bullet_style 을 광고하지 않는다 — 받는데 안 적힌 계약이다")
	}
}
