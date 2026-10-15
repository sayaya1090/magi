package layered

import (
	"context"
	"github.com/sayaya1090/magi/internal/port"
	"strings"
	"testing"
)

func TestKoreanRankingUsesCombinedCorpusAcrossScopes(t *testing.T) {
	ctx := context.Background()
	s := New(t.TempDir(), t.TempDir(), t.TempDir())
	for _, c := range []port.Contribution{
		{Scope: "project", Memories: []port.Memory{{Text: strings.Repeat("설정창의 ", 100) + "색상 기록"}}},
		{Scope: "team", Memories: []port.Memory{{Text: "설정창 너비 문제는 드롭다운 최소 폭을 확인한다"}}},
		{Scope: "global", Memories: []port.Memory{{Text: "슬라이드의 발표자 노트를 읽는다"}}},
	} {
		if err := s.Propose(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	mem, _, err := s.Retrieve(ctx, "설정창의 너비", nil)
	if err != nil || len(mem) == 0 || !strings.HasPrefix(mem[0].Text, "[team]") {
		t.Fatalf("team's specific answer must outrank generic project record: %+v %v", mem, err)
	}
}
