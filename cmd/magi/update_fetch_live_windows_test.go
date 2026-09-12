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

const liveTag = "v9.9.9-fetchlive"

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
	exe := buildMagi(t, cfg)
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
