package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 죽은 컴패니언을 다시 마련해 달라고 묻는 길이 세 판에 다 있는가.
//
// ⚠ **한 판에만 없으면 증상이 「죽었다고만 써 있다」이고, 아무도 아무것도 안 한다.** 헬퍼는
// `/api/own` 으로 물어야 다시 띄운다. 창이 열릴 때 한 번만 묻던 판은 데몬이 죽으면 상태만
// 세워 두고 끝이었고, 그것이 2026-09-07 에 실물에서 40초 넘게 재였다(768aa9f8). 고침은
// 파워포인트와 엑셀에 들어갔고 **워드에는 안 들어갔다** — 2026-09-11 실측: 워드의
// `helperApi.js` 에는 `own()` 이 있는데 `HelperPorts.js` 가 그것을 내놓지 않고,
// `WatchPrompt.js` 는 부르지 않는다. 문은 세 판 모두에 서 있다(API.Route 는 앱마다 돈다).
//
// 세 판이 같은 코드의 사본이라 이런 결함은 조용하다. 어느 파일도 없어지지 않고, 아무것도
// 실패하지 않으며, 갈라진 판만 복구를 잃는다. 그래서 셋을 서로에게 맞대 묻는다 — 어느 한
// 판을 기준으로 삼지 않고, **셋이 같은 답을 하는가**로.
func TestEveryAddinAsksForADeadCompanionBack(t *testing.T) {
	// 한 판이 이 길을 갖췄다고 말하는 세 자리. 층이 셋인 것이 요점이다: 워드는 첫째만 있고
	// 나머지 둘이 없어서, 하나만 봤다면 갖춘 것으로 읽혔다.
	layers := []struct{ file, needs, why string }{
		{"src/adapter/helperApi.js", "/api/own", "헬퍼에 물을 줄을 모른다"},
		{"src/adapter/HelperPorts.js", "own()", "물을 줄은 아는데 포트가 그것을 안 내놓는다"},
		{"src/usecase/WatchPrompt.js", "this.port.own", "포트에 있는데 죽음을 보고도 안 부른다"},
	}
	for _, app := range Apps {
		app := app
		t.Run(app.Key, func(t *testing.T) {
			for _, l := range layers {
				path := filepath.Join(addinDirOf(app), l.file)
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("%s 를 못 읽었다 — 옮겨졌으면 이 시험부터 고칠 것: %v", l.file, err)
				}
				if !strings.Contains(string(b), l.needs) {
					t.Errorf("%s 에 %q 가 없다 — %s", l.file, l.needs, l.why)
				}
			}
		})
	}
}

// 그리고 묻는 속도에 굴레가 있는가.
//
// 재시도에 굴레가 없으면 폴 한 번에 한 번씩 묻는다. 헬퍼는 그때마다 컴패니언을 마련하려 들고,
// 죽어 있는 동안 그것이 계속 쌓인다. 파워포인트 판이 15초를 두는 이유이고, 사본들이 그 숫자를
// 잃으면 복구가 있다는 사실만으로는 부족해진다.
func TestTheReaskIsRateLimited(t *testing.T) {
	for _, app := range Apps {
		app := app
		t.Run(app.Key, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(addinDirOf(app), "src/usecase/WatchPrompt.js"))
			if err != nil {
				t.Fatalf("WatchPrompt.js 를 못 읽었다: %v", err)
			}
			for _, must := range []string{"askedOwnAt", "15000"} {
				if !strings.Contains(string(b), must) {
					t.Errorf("WatchPrompt.js 에 %q 가 없다 — 다시 묻기에 굴레가 없으면 폴마다 묻는다", must)
				}
			}
		})
	}
}
