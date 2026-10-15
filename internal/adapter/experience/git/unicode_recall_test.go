package git

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestRetrieveKoreanWithoutEmbedding(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir())
	if err := s.Propose(ctx, port.Contribution{Memories: []port.Memory{
		{Text: "설정창 너비를 줄여도 드롭다운이 줄어들지 않는다"},
		{Text: "슬라이드 노트 읽기의 토큰 사용량을 제한한다"},
		{Text: strings.Repeat("설정창의 ", 100) + "색상 기록"},
	}, Skills: []port.Skill{{Name: "panel-width", Description: "설정창 너비 문제", Body: "드롭다운의 최소 폭을 확인한다"}}}); err != nil {
		t.Fatal(err)
	}
	mem, skills, err := s.Retrieve(ctx, "설정창의 너비", nil)
	if err != nil || len(mem) == 0 || !strings.Contains(mem[0].Text, "드롭다운") || len(skills) != 1 {
		t.Fatalf("Korean retrieval: %+v %+v %v", mem, skills, err)
	}
	for _, m := range mem {
		if strings.Contains(m.Text, "슬라이드") {
			t.Fatalf("unrelated memory included: %+v", mem)
		}
	}
	mem, _, err = s.Retrieve(ctx, "양자컴퓨터", nil)
	if err != nil || len(mem) != 0 {
		t.Fatalf("unrelated Korean query: %+v %v", mem, err)
	}
}
