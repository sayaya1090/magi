//go:build windows

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/shortdir"
	"github.com/sayaya1090/magi/internal/update"
)

// 갱신의 **앞 절반** — 확인 → 다운로드 → 체크섬 → 아카이브에서 꺼내기 → 프리플라이트 → 교체 — 을
// 진짜 HTTP 서버와 진짜 아카이브로 잰다. #193 이 「지금 시험은 후보를 디스크에 직접 세운다」고 적은
// 그 자리이고, 이제 릴리스 출처를 런타임에 말할 수 있어서(`MAGI_RELEASE_API_BASE`, `fb707e96`) 태울
// 수 있다.
//
// 윈도우에 두는 이유는 이 절반의 위험이 여기 살기 때문이다: 갓 내려받아 **갓 쓴 실행 파일을 곧바로
// 실행**하고(프리플라이트), 그 자리에 **도는 이미지를 갈아 끼운다**. 같은 시험을 POSIX 에서 돌리는
// 것은 값이 있지만 그쪽 레인의 일이라, 여기서는 이 플랫폼의 모양만 붙든다.
//
// magi 를 두 번 짓는다 — 지금 것과 「새 릴리스」 — 그래서 -short 에서 건너뛴다.

// releaseServer serves one release the way the GitHub API does: a listing, the archive, and the
// checksums table. Nothing here is a fake of the updater's own code — it is the wire.
type releaseServer struct {
	*httptest.Server
	tag    string
	asset  string
	sums   string // the checksums.txt body, so a test can publish a WRONG digest on purpose
	archiv []byte
}

func serveRelease(t *testing.T, tag string, binary []byte, digestOf func([]byte) string) *releaseServer {
	t.Helper()
	rs := &releaseServer{tag: tag, asset: update.AssetName() + ".tar.gz", archiv: tarGz(t, binary)}
	rs.sums = fmt.Sprintf("%s  %s\n", digestOf(rs.archiv), rs.asset)
	mux := http.NewServeMux()
	rs.Server = httptest.NewServer(mux)
	// 기본 owner/repo 그대로다 — 환경 문은 **주소**만 바꾼다(`MAGI_RELEASE_REPO` 같은 것은 없다).
	mux.HandleFunc("/repos/sayaya1090/magi/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		type asset struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": rs.tag,
			"assets": []asset{
				{Name: rs.asset, URL: rs.URL + "/dl/" + rs.asset},
				{Name: "checksums.txt", URL: rs.URL + "/dl/checksums.txt"},
			},
		})
	})
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(rs.sums))
	})
	mux.HandleFunc("/dl/"+rs.asset, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(rs.archiv)
	})
	t.Cleanup(rs.Close)
	return rs
}

// tarGz wraps the binary the way a release does: the executable at the archive root, beside the
// attribution files the real archives carry (so "skips everything else" is measured, not assumed).
func tarGz(t *testing.T, binary []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	add := func(name string, body []byte, mode int64) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(body)), Mode: mode}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	add("LICENSE", []byte("not the binary\n"), 0o644)
	add("magi.exe", binary, 0o755)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func trueDigest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

// fetchWorld is an install (a real magi), a config directory, and a "new release" build that names
// itself so the version it publishes says which binary is in place.
type fetchWorld struct {
	cfg, exe string
	newBin   []byte
}

// 판번호는 **정확한 태그**여야 한다 — 숫자 셋, 접미사 없음. `update.SelfUpdatable` 이 그렇게 읽고,
// 그러지 않으면 데몬의 문이 「소스 빌드는 자기갱신을 안 한다」며 거절한다(의도된 안전장치이고,
// 실측으로 배웠다: `dev` 도 `v0.0.1-fetchbase` 도 그 거절에 걸려 이 시험의 첫 두 판이 빨갰다).
const (
	baseTag = "v0.0.1"
	liveTag = "v9.9.9"
)

func fetchSetup(t *testing.T) fetchWorld {
	t.Helper()
	if testing.Short() {
		t.Skip("builds the binary twice")
	}
	cfg, err := shortdir.Make("mgf")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(cfg) })
	exe := buildMagi(t, cfg,
		"-ldflags=-X github.com/sayaya1090/magi/internal/version.Version="+baseTag)
	other, err := shortdir.Make("mgn")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(other) })
	next := buildMagi(t, other,
		"-ldflags=-X github.com/sayaya1090/magi/internal/version.Version="+liveTag)
	b, err := os.ReadFile(next)
	if err != nil {
		t.Fatal(err)
	}
	return fetchWorld{cfg: cfg, exe: exe, newBin: b}
}

// update runs the manual path — the one a person reaches for, and the one the console button ends up
// in — against the server, and gives back everything it said.
func (w fetchWorld) update(t *testing.T, base string) (string, int) {
	t.Helper()
	cmd := exec.Command(w.exe, "-update-core")
	cmd.Env = append(os.Environ(),
		"MAGI_CONFIG_DIR="+w.cfg,
		"MAGI_SOCKET_DIR="+w.cfg,
		"MAGI_RELEASE_API_BASE="+base)
	out, _ := cmd.CombinedOutput()
	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	return string(out), code
}

func (w fetchWorld) versionOnDisk(t *testing.T) string {
	t.Helper()
	out, err := exec.Command(w.exe, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("설치된 바이너리를 실행할 수 없다: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// The whole front half, end to end: the listing is read, the checksums table is fetched, the archive
// is downloaded and checked, the executable is taken OUT of it, run once, and put in place.
func TestAnUpdateInstallsFromARealReleaseServer(t *testing.T) {
	w := fetchSetup(t)
	rs := serveRelease(t, liveTag, w.newBin, trueDigest)

	said, code := w.update(t, rs.URL)
	if code != 0 {
		t.Fatalf("갱신이 %d 로 끝났다:\n%s", code, said)
	}
	// Condition ① of the env door (`fb707e96`): a non-default source is said out loud, and it names
	// the address. A switch that changes where software comes from must not be silent.
	if !strings.Contains(said, rs.URL) {
		t.Errorf("기본이 아닌 출처인데 그 주소를 말하지 않았다:\n%s", said)
	}
	if got := w.versionOnDisk(t); !strings.Contains(got, liveTag) {
		t.Fatalf("디스크의 바이너리가 새 판이 아니다: %q\n%s", got, said)
	}
	// The transaction is OPEN, not committed: the build it replaced is beside it and the journal says
	// so. Confirming is the next generation's job after its window (§9.3).
	if _, err := os.Stat(w.exe + ".prev"); err != nil {
		t.Errorf("갈아치운 이전 빌드를 안 남겼다 (%v) — 되돌릴 것이 없다", err)
	}
	if _, err := os.Stat(w.exe + ".update.json"); err != nil {
		t.Errorf("트랜잭션을 안 적었다 (%v) — 아무도 되돌릴 수 없다", err)
	}
}

// ⚠ The digest is the whole of the trust here, and the env door must not have weakened it. A server
// that publishes the WRONG digest gets nothing installed — condition ② of that door, measured
// against a non-default base rather than argued from the code.
func TestAWrongDigestFromANonDefaultSourceInstallsNothing(t *testing.T) {
	w := fetchSetup(t)
	was := w.versionOnDisk(t)
	rs := serveRelease(t, liveTag, w.newBin, func([]byte) string {
		return strings.Repeat("0", 64) // a digest of nothing at all
	})

	said, code := w.update(t, rs.URL)
	if code == 0 {
		t.Errorf("체크섬이 안 맞는데 성공으로 끝났다:\n%s", said)
	}
	if !strings.Contains(said, "checksum") && !strings.Contains(said, "mismatch") {
		t.Errorf("거절의 이유가 체크섬이라고 말하지 않는다:\n%s", said)
	}
	if got := w.versionOnDisk(t); got != was {
		t.Errorf("거절했는데 바이너리가 바뀌었다: %q → %q", was, got)
	}
	if _, err := os.Stat(w.exe + ".prev"); err == nil {
		t.Error("설치도 안 한 교체의 백업이 남았다 — 다음 기동이 그것을 끊긴 교체로 읽는다")
	}
}

// A source that publishes no checksums table at all is refused too: "install it by hand if you mean
// to". Same door, the other half of condition ②.
func TestASourceWithNoChecksumsIsRefused(t *testing.T) {
	w := fetchSetup(t)
	rs := serveRelease(t, liveTag, w.newBin, trueDigest)
	rs.sums = "" // served, but empty: the asset is listed nowhere in it
	said, code := w.update(t, rs.URL)
	if code == 0 {
		t.Errorf("체크섬 없이 설치했다:\n%s", said)
	}
	if got := w.versionOnDisk(t); strings.Contains(got, liveTag) {
		t.Error("아무도 보증하지 않은 바이너리가 자리에 앉았다")
	}
}

// U08 — 거절된 판을, 사람이 물으면 다시 받는다.
//
// 앞의 시험들이 「받아서 설치한다」까지이고, 이것은 그 다음에 오는 사실이다: 설치된 판이 **못
// 버텨서 되돌려지면** 자동 경로는 그것을 다시 집지 않고, `-update-core` 를 손으로 치는 것은
// §9.3 이 말하는 **명시적 재시도**라서 다시 집는다. 규칙 자체는 단위로 재고(`internal/update` 의
// refused_test), 여기서 재는 것은 그 규칙이 **진짜 서버에서 받은 진짜 판**에도 그대로 붙는가다.
func TestARefusedReleaseIsTakenAgainWhenAPersonAsks(t *testing.T) {
	w := fetchSetup(t)
	rs := serveRelease(t, liveTag, w.newBin, trueDigest)

	// 한 번 받아 설치한다.
	if said, code := w.update(t, rs.URL); code != 0 {
		t.Fatalf("첫 설치가 %d 로 끝났다:\n%s", code, said)
	}
	// 그 판이 뜨고 **못 버틴다**: 세대 하나가 감시를 가져간 뒤 죽으면, 다음 기동이 그것을 증거로
	// 읽어 되돌리고 그 판을 거절한다. 기동에는 워크스페이스가 필요하다.
	ws, err := shortdir.Make("mgw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(ws) })
	first := exec.Command(w.exe, "--daemon", "--no-update-check")
	first.Dir = ws
	first.Env = append(os.Environ(), "MAGI_CONFIG_DIR="+w.cfg, "MAGI_SOCKET_DIR="+w.cfg)
	log, err := os.Create(w.cfg + string(os.PathSeparator) + "trial.log")
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	first.Stdout, first.Stderr = log, log
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	waitServing(t, daemon.SocketPath(w.cfg, ws), first.Process.Pid, w.cfg+string(os.PathSeparator)+"trial.log")
	if err := first.Process.Kill(); err != nil { // 넘어진 빌드는 이렇게 생겼다
		t.Fatal(err)
	}
	_ = first.Wait()

	// 다음 기동이 되돌리고 거절을 적는다(그 경로는 update_rollback_live_windows_test 가 잰다).
	second := exec.Command(w.exe, "--daemon", "--no-update-check")
	second.Dir = ws
	second.Env = first.Env
	second.Stdout, second.Stderr = log, log
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) && update.Refused(w.exe) == "" {
		time.Sleep(200 * time.Millisecond)
	}
	_ = second.Process.Kill()
	_ = second.Wait()
	if r := update.Refused(w.exe); r != liveTag {
		t.Fatalf("거절이 기록되지 않았다: %q\n%s", r, readLog(w.cfg+string(os.PathSeparator)+"trial.log"))
	}

	// 그리고 사람이 묻는다. 같은 서버·같은 판인데 이번에는 집는다.
	said, code := w.update(t, rs.URL)
	if code != 0 {
		t.Fatalf("명시적 재시도가 %d 로 끝났다:\n%s", code, said)
	}
	if got := w.versionOnDisk(t); !strings.Contains(got, liveTag) {
		t.Fatalf("사람이 물었는데 거절된 판을 다시 안 집었다: %q\n%s", got, said)
	}
	if r := update.Refused(w.exe); r != "" {
		t.Errorf("재시도 뒤에도 거절 기록이 남았다: %q", r)
	}
}

// 콘솔 단추 — 받아서 설치하고, **안 바쁠 때** 그 위로 재기동한다.
//
// `-update-core` 는 프로세스 하나가 설치만 하고 끝나는 경로다. 이 문은 **도는 데몬**이 제 자리에서
// 하는 일이라, 앞의 시험들이 못 보는 뒷 절반이 여기 붙는다: 교체한 뒤 스스로 재기동하고, 그
// 후계가 준비될 때까지 이전 세대가 기다리고(`graceful`), 새 판이 실제로 **서비스한다.**
//
// `when=idle` 은 「아무것도 안 돈다」를 찾은 **그 단계에서** 접수를 막고 재기동한다(§9.3 의 원자적
// 안전 시점). 이 시험의 데몬은 아무 일도 안 하고 있으므로 그 자리를 즉시 지나간다 — 턴이 도는 중의
// 보류(U02)는 모델 백엔드가 있어야 재는 것이고 단위(`internal/app` 의 hold)가 그쪽을 붙든다.
func TestTheConsoleButtonInstallsAndComesUpOnTheNewBuild(t *testing.T) {
	w := fetchSetup(t)
	rs := serveRelease(t, liveTag, w.newBin, trueDigest)
	ws, err := shortdir.Make("mgb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(ws) })
	t.Cleanup(func() {
		rows, _ := daemon.List(w.cfg)
		for _, r := range rows {
			if r.PID != 0 {
				if p, ferr := os.FindProcess(r.PID); ferr == nil {
					_ = p.Kill()
				}
			}
		}
	})

	logPath := w.cfg + string(os.PathSeparator) + "button.log"
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	// 루프는 끈다 — 첫 확인이 최대 1.5시간 지터라 이 시험과 섞일 일이 없지만, 재는 것이 **문**이지
	// 루프가 아니라는 것을 환경에도 적어 둔다.
	cmd := exec.Command(w.exe, "--daemon", "--no-update-check")
	cmd.Dir = ws
	cmd.Env = append(os.Environ(),
		"MAGI_CONFIG_DIR="+w.cfg,
		"MAGI_SOCKET_DIR="+w.cfg,
		"MAGI_RELEASE_API_BASE="+rs.URL)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	sock := daemon.SocketPath(w.cfg, ws)
	waitServing(t, sock, cmd.Process.Pid, logPath)

	c, err := daemon.DialWithin(sock, 2*time.Second, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.UpdateWhen("idle")
	c.Close()
	if err != nil {
		t.Fatalf("문이 거절했다: %v\n%s", err, readLog(logPath))
	}
	if !strings.Contains(out, liveTag) {
		t.Errorf("답이 무엇으로 갔는지 말하지 않는다: %q", out)
	}

	// 그리고 뒷 절반: 새 판이 **서비스한다.** 세대가 바뀌었고(PID 도 판번호도), 붙어서 확인한다.
	rec := waitVersion(t, sock, liveTag, readLog(logPath))
	if rec.PID == 0 {
		t.Fatalf("새 판으로 서비스하는 데몬이 없다:\n%s", readLog(logPath))
	}
	if rec.PID == cmd.Process.Pid {
		t.Error("재기동 없이 판번호가 바뀌었다고 한다 — 기록이 이전 세대를 가리킨다")
	}
	if _, err := os.Stat(w.exe + ".prev"); err != nil {
		t.Errorf("되돌릴 이전 빌드를 안 남겼다: %v", err)
	}
}
