package main

import (
	"strings"
	"testing"
)

// 그림을 주면서 **볼 방법을 안 주면** 모델은 셸로 간다. 실물에서 그 길을 봤다(2026-09-04):
// render_chart → 캐시 파일 read(거부) → 인자 이름 바꿔 재시도(거부) → PIL 있는지 bash.
// 승인기가 거기서 판을 세웠고, 사람이 본 것은 멈춘 판이다.
func TestImageBlockCarriesTheNoteAboutNotOpeningTheFile(t *testing.T) {
	block, note, ok := imageBlock(map[string]any{"image_base64": "AAAA"})
	if !ok {
		t.Fatal("그림이 있는데 블록을 안 만들었다")
	}
	if block["type"] != "image" || block["data"] != "AAAA" {
		t.Fatalf("블록 모양이 다르다: %#v", block)
	}
	// mime 을 안 줘도 png 로 세운다 — 안 세우면 블록이 종류 없는 그림이 된다.
	if block["mimeType"] != "image/png" {
		t.Errorf("기본 mime 이 안 섰다: %v", block["mimeType"])
	}
	for _, must := range []string{"이미지로 붙여", "파일로 열려고 하지 마세요", "read_range"} {
		if !strings.Contains(note, must) {
			t.Errorf("안내에 %q 가 없다 — 모델은 다시 파일을 열려 한다: %s", must, note)
		}
	}

	// 그림이 없으면 **안내도 없다.** 모든 답에 붙이면 그 문장은 곧 안 읽히는 배경이 된다.
	if _, n, ok := imageBlock(map[string]any{}); ok || n != "" {
		t.Errorf("그림 없는 답에 안내가 붙었다: %q", n)
	}
	if _, _, ok := imageBlock(map[string]any{"image_base64": ""}); ok {
		t.Error("빈 그림을 그림으로 셌다")
	}
}
