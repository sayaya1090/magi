package update

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 거절된 판의 두 얼굴 — **자동은 건너뛰고, 사람이 물으면 다시 한다**(§9.3, U08).
//
// 유닉스 쪽에도 같은 물음이 있지만(`journal_test.go`) 거기서는 설치 자리에 셸 스크립트를 세우므로 그
// 플랫폼에만 있습니다. 이 파일은 `lockscope_test.go` 의 방식을 씁니다 — 시험 바이너리 자신을 설치
// 자리에 복사해 어느 플랫폼에서나 **실행 가능한** 대역을 세우는 것. 프리플라이트가 그 파일을 진짜로
// 실행하므로 그것이 없으면 이 시험은 「빌드가 안 돈다」를 재게 됩니다.
//
// ⚠ 왜 이 규칙에 시험이 필요한가: 없으면 `--version` 을 통과하고 서비스는 못 하는 릴리스가 여섯
// 시간마다 내려받히고 설치되고 되돌려지기를 **영원히** 반복합니다.

// oneRelease is a Source that answers one release and counts what was asked of it — so "skipped"
// can be shown to mean "nothing was downloaded", not merely "nothing was installed".
type oneRelease struct {
	version  string
	bin      []byte
	latests  int
	download int
}

func (s *oneRelease) Latest(context.Context) (Release, error) {
	s.latests++
	return Release{Version: s.version}, nil
}

func (s *oneRelease) Download(context.Context, string) ([]byte, error) {
	s.download++
	return s.bin, nil
}

func TestARefusedBuildIsSkippedByTheAutomaticPathAndTakenByAnExplicitOne(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, exeName("magi-install"))
	was := selfAs(t, target)
	// The payload differs from what is installed, or Commit is idempotent and never writes anything.
	src := &oneRelease{version: "v2.0.0", bin: append(was, '\n')}

	// This install ran v2.0.0 once and could not keep it. Written through the journal's own door
	// rather than by hand: what is being measured is how RunCommit reads that state, and a
	// hand-made ledger measures my idea of the shape instead of the shape.
	// The backup Commit leaves beside a replacement — the file a rollback goes back to.
	if err := os.WriteFile(target+".prev", was, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Began(target, Versions{From: "v1.0.0", To: "v2.0.0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := SuccessorFailed(target, "v1.0.0", 0); err != nil {
		t.Fatal(err)
	}
	if r := Refused(target); r != "v2.0.0" {
		t.Fatalf("전제가 안 섰다 — 거절 기록이 %q", r)
	}

	// The automatic path: it asks what is latest, sees the version this install refused, and stops
	// there. Nothing is downloaded — the point is not to spend a network round trip every six hours
	// on a build that is already known to fall over.
	res, err := RunCommit(context.Background(), src, "v1.0.0", target)
	if err != nil {
		t.Fatal(err)
	}
	if res.Updated {
		t.Fatal("자동 경로가 거절된 판을 다시 설치했다")
	}
	if !strings.Contains(res.Skipped, "explicit") {
		t.Errorf("건너뛴 이유가 「사람이 물으면 된다」를 말하지 않는다: %q", res.Skipped)
	}
	if src.download != 0 {
		t.Errorf("건너뛰면서 내려받았다 (%d 번) — 여섯 시간마다 같은 아카이브를 받는다", src.download)
	}
	if on, rerr := os.ReadFile(target); rerr != nil || string(on) != string(was) {
		t.Error("건너뛴다면서 바이너리를 건드렸다")
	}

	// And the door a person walks through: `-update-core` and the console button both clear the
	// refusal first (cmd/magi: runCoreUpdate, the update door), which is this call.
	if err := Retry(target); err != nil {
		t.Fatal(err)
	}
	res, err = RunCommit(context.Background(), src, "v1.0.0", target)
	if err != nil {
		t.Fatalf("명시적 재시도인데 설치가 실패했다: %v", err)
	}
	if !res.Updated {
		t.Fatalf("사람이 물었는데도 안 했다: %+v", res)
	}
	if src.download != 1 {
		t.Errorf("내려받기를 %d 번 했다 — 재시도는 한 번이다", src.download)
	}
	on, rerr := os.ReadFile(target)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if len(on) != len(was)+1 {
		t.Error("자리에 앉은 것이 새 판이 아니다")
	}
	// The refusal is gone, and the new transaction is open in its place — the next generation is on
	// trial again rather than pre-forgiven.
	if r := Refused(target); r != "" {
		t.Errorf("재시도했는데 거절 기록이 남아 있다: %q", r)
	}
	if _, serr := os.Stat(target + ".prev"); serr != nil {
		t.Errorf("되돌릴 이전 빌드를 안 남겼다: %v", serr)
	}
}
