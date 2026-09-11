package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ⚠ **A name with nothing behind it is worse than no name.**
//
// A client reads `--features`, believes it, starts the mode, and the failure it gets is
// indistinguishable from a broken install — docs/CLIENT_LIFECYCLE §4 says so and this is the floor
// that holds it. Most feature names are derived from the implementation inside the bridge package
// (a flag that is registered), but a feature of the BINARY — `--daemon --client-owned` — lives in
// `cmd/magi` and the bridge cannot ask about it. So the command names it, and this test ties the
// name to the behaviour by starting the real binary.
//
// It builds magi, so it is skipped under -short.
func TestEveryAdvertisedFeatureIsOneThisBinaryActuallyHas(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	exe := filepath.Join(t.TempDir(), "magi")
	if out, err := exec.Command("go", "build", "-o", exe, "github.com/sayaya1090/magi/cmd/magi").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	line, err := exec.Command(exe, "ide-bridge", "--features").Output()
	if err != nil {
		t.Fatalf("--features: %v", err)
	}
	var got struct {
		Features []string `json:"features"`
	}
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatalf("not JSON (%v): %s", err, line)
	}
	if len(got.Features) == 0 {
		t.Fatal("아무 기능도 안 알린다 — 아래 검사가 잴 것이 없다")
	}

	// ⚠ **And the other direction.** The check below only says "everything named is real"; a build
	// that named NOTHING would pass it while every client concluded the features are absent and fell
	// back. Measured: dropping the caller's names on the floor in the bridge survived until this
	// was here. Both modes this binary has are floors.
	for _, must := range []string{"raw-socket-v1", "owned-daemon-v1"} {
		found := false
		for _, n := range got.Features {
			if n == must {
				found = true
			}
		}
		if !found {
			t.Errorf("%q 가 이 바이너리에 있는데 안 알린다 — 클라이언트는 없다고 읽고 물러선다: %v",
				must, got.Features)
		}
	}

	// What each name PROMISES, asked of the binary itself. A name with no entry here fails: adding
	// one without saying how to check it is how a list starts drifting from the thing again.
	proves := map[string]func(t *testing.T){
		"raw-socket-v1": func(t *testing.T) {
			// The relay refuses a socket that is not there, rather than not knowing the option.
			out, _ := exec.Command(exe, "ide-bridge", "--raw-socket", filepath.Join(t.TempDir(), "nope.sock")).CombinedOutput()
			if strings.Contains(string(out), "flag provided but not defined") {
				t.Errorf("--raw-socket 를 알린다면서 그 옵션을 모른다: %s", out)
			}
		},
		"owned-daemon-v1": func(t *testing.T) {
			// Registered, and it knows what it is for: alone it says to use it with --daemon.
			cmd := exec.Command(exe, "--client-owned")
			cmd.Env = append(os.Environ(), "MAGI_CONFIG_DIR="+t.TempDir())
			out, _ := cmd.CombinedOutput()
			if strings.Contains(string(out), "flag provided but not defined") {
				t.Errorf("--client-owned 를 알린다면서 그 옵션을 모른다: %s", out)
			}
			if !strings.Contains(string(out), "use it with --daemon") {
				t.Errorf("소유 모드가 제 쓰임새를 모른다: %s", out)
			}
		},
	}
	for _, name := range got.Features {
		check, ok := proves[name]
		if !ok {
			t.Errorf("%q 를 알리는데 그것이 진짜 있는지 확인할 길이 여기 없다 — "+
				"이름을 더할 때 무엇으로 확인하는지도 같이 적을 것", name)
			continue
		}
		t.Run(name, check)
	}
}
