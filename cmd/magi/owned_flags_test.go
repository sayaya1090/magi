package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// `--client-owned` says WHO this daemon belongs to, and both refusals are about that sentence
// being true.
//
// Neither is caught by anything else: both spellings parse, both start a process, and the damage
// shows up later as a lifetime nobody asked for — a daemon that outlives the window that owns it,
// or one that dies when a person closes a terminal they did not know was load-bearing.
// docs/CLIENT_LIFECYCLE §4 names the first ("몰래 detached 모드로 실행하지 않습니다").
func TestTheOwnedFlagRefusesWhatWouldMakeItALie(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	exe := filepath.Join(t.TempDir(), "magi")
	if out, err := exec.Command("go", "build", "-o", exe, "github.com/sayaya1090/magi/cmd/magi").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, c := range []struct {
		name, want string
		args       []string
	}{
		{"소유 모드는 데몬을 여는 방식이지 그 자체가 모드가 아니다", "use it with --daemon",
			[]string{"--client-owned"}},
		{"떼어내기와 소유는 반대말이다", "opposite things",
			[]string{"--daemon", "--client-owned", "--detach"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			cmd := exec.Command(exe, c.args...)
			cmd.Env = append(os.Environ(), "MAGI_CONFIG_DIR="+t.TempDir())
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("받아들였다 — %v\n%s", c.args, out)
			}
			if code := cmd.ProcessState.ExitCode(); code != 2 {
				t.Errorf("exit %d, want 2 (쓰임새 오류)", code)
			}
			if !strings.Contains(string(out), c.want) {
				t.Errorf("사유를 안 말한다 (%q 를 기대):\n%s", c.want, out)
			}
		})
	}
}
