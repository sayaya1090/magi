package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The record and the socket must name the SAME process, and a client checks both.
//
// docs/CLIENT_LIFECYCLE §4 asks for exactly this before a client calls its daemon ready: the child
// it spawned, the workspace it resolved, and the instance id in the published record and in `about`
// all agreeing. The record is a file written a moment ago and the handshake comes from the process
// that is listening right now — two sources, and the check is worth making only because they can
// disagree.
func TestTheRecordAndTheHandshakeNameOneProcess(t *testing.T) {
	path, _ := serveOn(t, &omniEngine{})
	c := dialWhenUp(t, path)
	defer c.Close()

	p, err := c.Hello()
	if err != nil {
		t.Fatal(err)
	}
	if p.Instance == "" {
		t.Fatal("handshake 가 어느 프로세스인지 말하지 않는다 — 준비 확인이 PID 하나에 매달린다")
	}

	dir := t.TempDir()
	rec := filepath.Join(dir, "rec.sock")
	stop, err := Publish(rec, dir, "s1", Identity{Name: "probe"})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	in, err := Published(rec)
	if err != nil {
		t.Fatal(err)
	}
	if in.Instance != p.Instance {
		t.Errorf("기록은 %q, 문은 %q — 한 프로세스가 두 가지로 자기를 말한다", in.Instance, p.Instance)
	}
	if in.PID != os.Getpid() {
		t.Errorf("PID = %d, 이 프로세스는 %d", in.PID, os.Getpid())
	}
}

// ⚠ **It must be fresh per process, or it says the opposite of what it is for.**
//
// The whole point is telling an update's replacement from a PID that got reused. A successor that
// carried its predecessor's id would report a replacement as a continuation — the client would
// believe the process it started is still there when it is gone. So the id is generated and read
// from nowhere: no environment variable, no file.
//
// Measured by running the real binary twice and comparing, which is the only way to observe "per
// process" from inside one process.
func TestTwoProcessesNeverShareAnInstanceID(t *testing.T) {
	// ⚠ **The child has to PRINT its id, or this measures nothing.** The first version of this test
	// ran `magi ide-bridge --features`, which never calls InstanceID at all — so a mutation that
	// made the id inherit `MAGI_INSTANCE_ID` survived: the output being checked could not have
	// contained the id either way. The helper-process pattern puts the real generator in the child.
	if os.Getenv(instanceProbeEnv) != "" {
		return // the child's work is in TestMain-free helper mode below
	}
	say := func(env ...string) string {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=TestInstanceIDHelperProcess")
		cmd.Env = append(append(os.Environ(), instanceProbeEnv+"=1"), env...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("helper: %v\n%s", err, out)
		}
		_, id, ok := strings.Cut(string(out), instanceProbeMark)
		if !ok {
			t.Fatalf("helper 가 id 를 안 찍었다:\n%s", out)
		}
		id, _, _ = strings.Cut(id, "\n")
		return strings.TrimSpace(id)
	}

	first, second := say(), say()
	if first == "" || second == "" {
		t.Fatal("자식이 빈 id 를 말한다")
	}
	if first == second {
		t.Errorf("두 프로세스가 같은 id 다 (%q) — 교체와 PID 재사용을 가르지 못한다", first)
	}
	// Every plausible spelling of "inherit your id". If any of these ever reaches the generator,
	// a successor would report a replacement as a continuation — the client believes the process
	// it started is still there when it is gone.
	for _, k := range []string{"MAGI_INSTANCE_ID", "MAGI_INSTANCE", "MAGI_OWNER_ID"} {
		if got := say(k + "=inherited"); got == "inherited" {
			t.Errorf("%s 에서 id 를 물려받는다", k)
		}
	}

	// And within one process it is stable: a client compares what `about` says now against what the
	// record said a moment ago, so an id that moved between two reads would fail every check.
	mine := InstanceID()
	if mine == "" {
		t.Fatal("id 가 비었다")
	}
	if InstanceID() != mine {
		t.Error("한 프로세스 안에서 id 가 바뀐다 — 두 번 읽는 확인이 언제나 실패한다")
	}
}

const (
	instanceProbeEnv  = "MAGI_INSTANCE_PROBE"
	instanceProbeMark = "instance-id="
)

// TestInstanceIDHelperProcess is not a test. It is the child [TestTwoProcessesNeverShareAnInstanceID]
// runs so the real generator executes in a real second process; it does nothing when run normally.
func TestInstanceIDHelperProcess(t *testing.T) {
	if os.Getenv(instanceProbeEnv) == "" {
		t.Skip("helper process")
	}
	fmt.Printf("%s%s\n", instanceProbeMark, InstanceID())
}
