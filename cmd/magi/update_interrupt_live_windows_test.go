//go:build windows

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/shortdir"
	"github.com/sayaya1090/magi/internal/update"
)

// U07 — **교체 도중에 끊겨도 다음 시작이 성한 판으로 온다.**
//
// 이 나무에 이미 Salvage 시험이 있지만(`update_rollback_live_windows_test.go`) 그것은 끊긴 상태를
// **세워 두고** 재는 것이다 — `.prev` 를 손으로 놓고 저널을 비우는 식. 여기서는 진짜로 끊는다: 교체가
// 진행되는 동안 그 프로세스를 죽이고, 그 뒤 이 설치가 어떤 상태인지 묻는다.
//
// ⚠ **전원 차단은 흉내낼 수 없다.** `TerminateProcess` 는 프로세스는 죽이지만 파일시스템 캐시는
// 비우지 않으므로, 여기서 재는 것은 「프로세스 중단」이고 「머신 중단」이 아니다. 그 차이를 적어 둔다 —
// 저널이 fsync 로 지키는 것이 후자이고, 그것을 재려면 이 시험이 아니라 전원 스위치가 필요하다.
//
// 무엇을 단언하나. 어느 순간에 끊겨도:
//
//  1. 설치 자리의 파일은 **실행된다** — 찢어진 바이너리가 남지 않는다.
//  2. 그 판은 **이전 판이거나 새 판**이고, 그 둘이 아닌 것은 없다.
//  3. 그 위로 데몬이 **뜨고 서비스한다** — 다음 시작이 저널과 파일을 대조해 마무리한다.

// slowRelease serves the archive in pieces, and says when the last piece has gone out — so a test can
// aim its interruption at the window right after the download, which is where the file work happens.
type slowRelease struct {
	*httptest.Server
	served chan struct{}
	sums   string
	asset  string
	blob   []byte
}

func serveSlowRelease(t *testing.T, tag string, binary []byte) *slowRelease {
	t.Helper()
	rs := &slowRelease{served: make(chan struct{}), asset: update.AssetName() + ".tar.gz"}
	rs.blob = tarGz(t, binary)
	rs.sums = fmt.Sprintf("%s  %s\n", trueDigest(rs.blob), rs.asset)
	mux := http.NewServeMux()
	rs.Server = httptest.NewServer(mux)
	mux.HandleFunc("/repos/sayaya1090/magi/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[{"name":%q,"browser_download_url":%q},`+
			`{"name":"checksums.txt","browser_download_url":%q}]}`,
			tag, rs.asset, rs.URL+"/dl/a", rs.URL+"/dl/sums")
	})
	mux.HandleFunc("/dl/sums", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(rs.sums)) })
	mux.HandleFunc("/dl/a", func(w http.ResponseWriter, _ *http.Request) {
		// In pieces with a small gap: the download becomes a window a test can see the end of, and
		// the bytes are real so the digest still has to match.
		const pieces = 8
		step := len(rs.blob) / pieces
		for i := 0; i < pieces; i++ {
			end := (i + 1) * step
			if i == pieces-1 {
				end = len(rs.blob)
			}
			if _, err := w.Write(rs.blob[i*step : end]); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(20 * time.Millisecond)
		}
		select {
		case <-rs.served:
		default:
			close(rs.served)
		}
	})
	t.Cleanup(rs.Close)
	return rs
}

// aCopyOf stages a fresh install of the base build in its own config directory, so each interruption
// starts from the same clean state without paying for another `go build`.
func aCopyOf(t *testing.T, base []byte) (cfg, exe string) {
	t.Helper()
	cfg, err := shortdir.Make("mgi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(cfg) })
	exe = filepath.Join(cfg, "magi.exe")
	if err := os.WriteFile(exe, base, 0o755); err != nil {
		t.Fatal(err)
	}
	return cfg, exe
}

func TestAnInterruptedReplacementLeavesAnInstallThatStillRuns(t *testing.T) {
	w := fetchSetup(t) // 판 둘을 한 번만 짓는다
	base, err := os.ReadFile(w.exe)
	if err != nil {
		t.Fatal(err)
	}

	// 다운로드가 끝난 뒤부터 파일 작업(백업 → 교체 → 프리플라이트 → 저널)이 일어난다. 그 구간을
	// 훑는다 — 한 지점만 찌르면 지나간 자리만 재고, 여러 지점이면 「어디서 끊겨도」에 가까워진다.
	for _, after := range []time.Duration{0, 40 * time.Millisecond, 120 * time.Millisecond,
		300 * time.Millisecond, 700 * time.Millisecond,
		1100 * time.Millisecond, 1600 * time.Millisecond} {
		t.Run(fmt.Sprintf("다운로드 뒤 %v 에 끊는다", after), func(t *testing.T) {
			cfg, exe := aCopyOf(t, base)
			rs := serveSlowRelease(t, liveTag, w.newBin)
			ws, err := shortdir.Make("mgj")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(ws) })
			t.Cleanup(func() {
				rows, _ := daemon.List(cfg)
				for _, r := range rows {
					if r.PID != 0 {
						if p, ferr := os.FindProcess(r.PID); ferr == nil {
							_ = p.Kill()
						}
					}
				}
			})

			up := exec.Command(exe, "-update-core")
			up.Env = append(os.Environ(),
				"MAGI_CONFIG_DIR="+cfg, "MAGI_SOCKET_DIR="+cfg, "MAGI_RELEASE_API_BASE="+rs.URL)
			if err := up.Start(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-rs.served:
			case <-time.After(60 * time.Second):
				_ = up.Process.Kill()
				t.Skip("아카이브가 다 나가지 않았다 — 이 회차는 겨눌 창이 없다")
			}
			time.Sleep(after)
			_ = up.Process.Kill() // 프로세스 중단. 전원 차단은 아니다 — 위 머리말을 보라.
			_ = up.Wait()

			// 1·2. 남은 파일이 실행되고, 그 판은 둘 중 하나다.
			out, verr := exec.Command(exe, "--version").CombinedOutput()
			if verr != nil {
				t.Fatalf("끊긴 뒤 설치 자리의 파일이 실행되지 않는다: %v\n%s", verr, out)
			}
			said := strings.TrimSpace(string(out))
			if !strings.Contains(said, baseTag) && !strings.Contains(said, liveTag) {
				t.Fatalf("이전 판도 새 판도 아닌 것이 자리에 있다: %q", said)
			}
			// 이 회차가 **무엇을 실제로 찔렀는지** 적는다. 다 초록인 훑기는 「어디서 끊겨도 괜찮다」로
			// 읽히기 쉽지만, 모든 회차가 교체 완료 뒤에 끊겼다면 그것은 「완료된 갱신은 멀쩡하다」를
			// 다섯 번 잰 것이다. 끊긴 순간의 흔적이 그 구분을 말해 준다.
			had := func(suffix string) bool { _, serr := os.Stat(exe + suffix); return serr == nil }
			hadPrev, hadJournal := had(".prev"), had(".update.json")
			stage := "교체 전 — 이전 판 그대로"
			switch {
			case hadPrev && !hadJournal:
				stage = "교체 후·저널 전 — 아무도 기록하지 않은 교체(Salvage 의 자리)"
			case hadPrev && hadJournal:
				stage = "기록까지 끝난 뒤 — 정상적인 미확정 트랜잭션"
			}
			t.Logf("끊긴 순간: %s · 디스크: %s", stage, said)

			// 3. 그 위로 데몬이 뜨고 서비스한다 — 다음 시작이 저널과 파일을 대조해 마무리한다.
			logPath := filepath.Join(cfg, "after.log")
			log, cerr := os.Create(logPath)
			if cerr != nil {
				t.Fatal(cerr)
			}
			defer log.Close()
			d := exec.Command(exe, "--daemon", "--no-update-check")
			d.Dir = ws
			d.Env = append(os.Environ(), "MAGI_CONFIG_DIR="+cfg, "MAGI_SOCKET_DIR="+cfg)
			d.Stdout, d.Stderr = log, log
			if serr := d.Start(); serr != nil {
				t.Fatal(serr)
			}
			sock := daemon.SocketPath(cfg, ws)
			deadline := time.Now().Add(60 * time.Second)
			var up2 bool
			for time.Now().Before(deadline) {
				if _, ok := serving(sock); ok {
					up2 = true
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
			if !up2 {
				t.Fatalf("끊긴 설치 위로 데몬이 못 떴다 (디스크: %s):\n%s", said, readLog(logPath))
			}
			// 그리고 **끊긴 자리에 맞는 복구**를 했는지 묻는다. 「떴다」만으로는 부족하다 — 이 나무가
			// 되풀이해 값을 치른 모양이 「초록인데 안 재고 있다」이고, 아래 없이는 이 시험이 세
			// 갈래를 한 문장으로 뭉갠다.
			after, aerr := exec.Command(exe, "--version").CombinedOutput()
			if aerr != nil {
				t.Fatalf("데몬이 뜬 뒤 설치 자리가 실행되지 않는다: %v\n%s", aerr, after)
			}
			now := strings.TrimSpace(string(after))
			switch {
			case hadPrev && !hadJournal:
				// 아무도 기록하지 않은 교체다. 그 파일이 프리플라이트를 통과했는지 아무도 모르므로,
				// 다음 시작은 **백업을 되돌려 놓아야** 한다(update.Salvage 의 계약).
				if !strings.Contains(now, baseTag) {
					t.Errorf("기록 없는 교체를 그대로 안고 떴다 (%s) — 프리플라이트를 통과했는지 "+
						"아무도 모르는 파일이다:\n%s", now, readLog(logPath))
				}
				if had(".prev") {
					t.Error("되돌린 뒤에도 백업이 남았다 — 다음 시작이 또 같은 판정을 내린다")
				}
				t.Logf("복구: 기록 없는 교체를 되돌렸다 → %s", now)
			case hadPrev && hadJournal:
				// 기록된 미확정 트랜잭션이다. 이것은 되돌릴 일이 아니라 **재판을 받을 일**이다 —
				// 새 판으로 뜨고 창을 버티면 확정된다.
				if !strings.Contains(now, liveTag) {
					t.Errorf("기록된 트랜잭션을 근거 없이 되돌렸다 (%s):\n%s", now, readLog(logPath))
				}
				t.Logf("복구: 미확정 트랜잭션을 안고 떴다(재판 중) → %s", now)
			default:
				if !strings.Contains(now, baseTag) {
					t.Errorf("교체 전에 끊겼는데 판이 바뀌었다: %s", now)
				}
				t.Logf("복구: 건드린 것이 없다 → %s", now)
			}
		})
	}
}
