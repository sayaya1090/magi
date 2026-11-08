package git

import (
	"context"
	"fmt"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// A lesson is a whole paragraph, and BM25 adds a little for every word it shares with a memory, so
// its score grows with its length. Measured on a real store of 60 memories, every one of them scored
// over 3000 against some other memory when used as the query.
func TestALongLessonIsNotARepeatOfAnUnrelatedMemory(t *testing.T) {
	const lesson = "오늘 저녁 메뉴로 김치찌개를 끓일 때 돼지고기를 먼저 볶으면 국물 맛이 깊어지고 두부는 마지막에 넣어야 부서지지 않는다"
	ctx := context.Background()
	s := New(t.TempDir())
	texts := []string{
		"magi 저장소에서 빌드할 때 테스트를 먼저 돌리고 실패하면 로그를 읽어 원인을 찾아 고친 뒤 마지막에 다시 실행한다",
		"문서를 고칠 때 한국어와 영어 쌍을 먼저 맞추고 마지막에 감사 도구를 돌려야 빠지지 않는다",
		"컨테이너를 띄울 때 런타임을 먼저 확인하고 포트는 마지막에 묶어야 충돌하지 않는다",
		"gradle wrapper distribution checksum verification",
		lesson,
	}
	for i, text := range texts[:4] {
		if _, err := s.ApplyExperience(ctx, port.ExperienceChange{
			ObjectID: fmt.Sprintf("mem_%d", i), OperationID: fmt.Sprintf("op_%d", i), ObservationID: fmt.Sprintf("obs_%d", i),
			Owner: "plugin:engram", Source: "plugin:engram", Kind: "memory", State: "active", HostOutcome: "verified",
			Memory: &port.Memory{Text: text},
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Filler keeps the words the lesson shares with the memories above rare, as in a real store.
	for i := 0; i < 40; i++ {
		if _, err := s.ApplyExperience(ctx, port.ExperienceChange{
			ObjectID: fmt.Sprintf("fill_%d", i), OperationID: fmt.Sprintf("fop_%d", i), ObservationID: fmt.Sprintf("fobs_%d", i),
			Owner: "plugin:engram", Source: "plugin:engram", Kind: "memory", State: "active", HostOutcome: "verified",
			Memory: &port.Memory{Text: fmt.Sprintf("gradle wrapper distribution checksum verification number %d", i)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, minSim := range []float64{0.65, 0.78, 0.82} {
		hits, err := s.SearchManagedExperience(ctx, lesson, "memory", minSim, 3)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 0 {
			t.Errorf("minSim %.2f: an unrelated lesson matched %s (score %d)", minSim, hits[0].ObjectID, hits[0].Score)
		}
		repeat, err := s.SearchManagedExperience(ctx, texts[0], "memory", minSim, 3)
		if err != nil {
			t.Fatal(err)
		}
		if len(repeat) == 0 || repeat[0].ObjectID != "mem_0" {
			t.Errorf("minSim %.2f: an exact repeat was not found: %v", minSim, repeat)
		}
	}
}
