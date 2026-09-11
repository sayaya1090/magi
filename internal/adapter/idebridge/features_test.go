package idebridge

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A client has to be able to ask this binary what it can do BEFORE it trusts it with anything.
//
// docs/CLIENT_LIFECYCLE §4: one line of JSON, no daemon connection, done inside five seconds. The
// reason it cannot dial is that the caller is often asking precisely because nothing is running —
// it is deciding whether to start something — so a probe that waits on a socket answers the wrong
// question slowly.
func TestFeaturesAnswersOneLineWithoutADaemon(t *testing.T) {
	// A config directory with nothing in it: if this probe dialled, this is where it would look and
	// find nothing, and the answer would be a failure rather than a description of the binary.
	t.Setenv("MAGI_CONFIG_DIR", t.TempDir())
	t.Setenv("MAGI_SOCKET_DIR", t.TempDir())

	var out, errs bytes.Buffer
	start := time.Now()
	code := Run([]string{"--features"}, strings.NewReader(""), &out, &errs)
	took := time.Since(start)

	if code != 0 {
		t.Fatalf("exit %d, stderr = %q", code, errs.String())
	}
	if took > 5*time.Second {
		t.Errorf("%v 걸렸다 — 계약은 5초 이내다", took)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("줄이 %d 개다 — 한 줄로 읽는 클라이언트가 어디서 멈출지 모른다: %q", len(lines), out.String())
	}

	var got struct {
		Protocol int      `json:"protocol"`
		Features []string `json:"features"`
		Version  string   `json:"version"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("JSON 이 아니다 (%v): %q", err, lines[0])
	}
	if got.Protocol != featuresProtocol {
		t.Errorf("protocol = %d, want %d", got.Protocol, featuresProtocol)
	}
	if got.Version == "" {
		t.Error("version 이 비었다 — 인수 기록이 어느 바이너리였는지 못 적는다")
	}
	// The Windows transport is the whole reason this probe exists: an extension that cannot dial
	// AF_UNIX has to know whether this binary can relay before it spawns one and watches it fail.
	if !has(got.Features, "raw-socket-v1") {
		t.Errorf("raw-socket-v1 이 안 실렸다 — 릴레이가 배선돼 있는데도: %v", got.Features)
	}
	// ⚠ A floor. `owned-daemon-v1` is named in the design and is NOT built; advertising it would
	// make a client start `--daemon --client-owned`, which this binary does not understand, and
	// read the failure as a broken install.
	if has(got.Features, "owned-daemon-v1") {
		t.Error("owned-daemon-v1 을 알리는데 그 모드가 없다 — 클라이언트가 없는 문을 두드린다")
	}
	// `null` and `[]` are different answers: the first cannot be told apart from "this field is not
	// implemented in this build".
	if !strings.Contains(lines[0], `"features":[`) {
		t.Errorf("features 가 배열로 안 나갔다: %q", lines[0])
	}
}

// The list is DERIVED, and that is the only reason it can be believed.
//
// A hand-kept list drifts one way: the name lands, the thing does not, and the client calls a door
// that is not there. So each entry asks the implementation, and a name whose predicate says no is
// dropped rather than printed.
func TestAFeatureNobodyWiredIsNotAdvertised(t *testing.T) {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	// Nothing registered: every predicate that looks at this build's flags must answer no.
	if got := featuresOf(fs); len(got) != 0 {
		t.Errorf("아무것도 배선 안 한 판이 %v 를 알린다 — 목록이 유도된 것이 아니다", got)
	}
	fs.String("raw-socket", "", "")
	if got := featuresOf(fs); !has(got, "raw-socket-v1") {
		t.Errorf("릴레이를 배선했는데 안 알린다: %v", got)
	}
	// A floor on the floor: if the table is ever emptied, the two checks above both pass by having
	// nothing to measure.
	if len(features) == 0 {
		t.Fatal("기능 표가 비었다 — 위의 두 검사가 잴 것이 없어서 초록이다")
	}

	// ⚠ **`[]` and `null` are different answers, and only the empty case can tell them apart.**
	// Measured: a mutation turning the slice into `var out []string` survived every check above,
	// because this build always has one feature and a non-empty slice marshals as an array either
	// way. A client reading `null` cannot tell "no features" from "this field is not implemented
	// in this build" — which is the one thing this whole probe exists to make unambiguous.
	none, err := json.Marshal(map[string]any{"features": featuresOf(flag.NewFlagSet("bare", flag.ContinueOnError))})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(none), `"features":[]`) {
		t.Errorf("기능이 하나도 없을 때 %s — `null` 은 「미구현」과 구별이 안 된다", none)
	}
}

// An older binary's answer is the flag package refusing the argument, and a client must read THAT.
//
// docs/CLIENT_LIFECYCLE §4 is explicit: "출력 문구 검색으로 지원을 추측하지 않습니다." The exit
// code is the contract, so it is pinned here.
func TestAnUnknownOptionIsRefusedWithACodeAClientCanRead(t *testing.T) {
	var out, errs bytes.Buffer
	if code := Run([]string{"--no-such-option"}, strings.NewReader(""), &out, &errs); code != 2 {
		t.Errorf("exit %d, want 2 — 구형 바이너리가 --features 를 거절하는 그 코드다", code)
	}
	if out.Len() != 0 {
		t.Errorf("거절인데 stdout 에 무엇이 나갔다: %q", out.String())
	}
}

// And the probe leaves nothing behind: a caller may run it on a machine that has never started
// magi, and creating a config tree as a side effect of asking a question is a side effect nobody
// asked for.
func TestFeaturesWritesNothingToDisk(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "cfg")
	t.Setenv("MAGI_CONFIG_DIR", cfg)
	var out, errs bytes.Buffer
	if code := Run([]string{"--features"}, strings.NewReader(""), &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if _, err := os.Stat(cfg); err == nil {
		t.Errorf("물어봤을 뿐인데 %s 를 만들었다", cfg)
	}
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
