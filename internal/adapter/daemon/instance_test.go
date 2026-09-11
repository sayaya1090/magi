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

// The owning lineage survives a replacement; the instance does not. That difference is the point.
//
// A self-update re-executes the daemon, and the window that started it never stopped owning it — so
// the owner id has to cross that boundary while the instance id must not. A client reading both
// then tells "my daemon updated itself" from "my daemon is gone", which is the pair of facts a PID
// alone cannot separate (docs/CLIENT_LIFECYCLE §4).
func TestTheOwningLineageCrossesAReplacementAndTheInstanceDoesNot(t *testing.T) {
	// ⚠ Nobody owns a daemon started from a terminal, and claiming a lineage there would be the
	// same defect as advertising a feature that is not built.
	if OwnerID() != "" {
		t.Fatalf("아무도 안 부른 채 계보가 있다: %q", OwnerID())
	}

	t.Setenv(OwnerEnv, "")
	minted := AdoptOwner()
	if minted == "" {
		t.Fatal("계보를 못 만들었다")
	}
	// Exported, or the lineage ends at the first replacement: graceful.Reexec hands the successor
	// os.Environ() and nothing else.
	if os.Getenv(OwnerEnv) != minted {
		t.Errorf("환경에 안 실었다 (%q) — 후계가 물려받을 길이 없다", os.Getenv(OwnerEnv))
	}
	if OwnerID() != minted {
		t.Errorf("OwnerID = %q, 방금 만든 것은 %q", OwnerID(), minted)
	}
	// And it is NOT the instance: one is inherited, the other never is.
	if minted == InstanceID() {
		t.Error("계보와 프로세스가 같은 값이다 — 교체를 연속과 못 가른다")
	}
}

// A successor adopts the lineage it inherited, and passes it on again.
//
// Run in a child so the package-level once is fresh: the adoption happens at startup exactly once,
// and a test that called it twice in one process would measure the cache, not the rule.
func TestASuccessorKeepsTheLineageItInherited(t *testing.T) {
	if os.Getenv(instanceProbeEnv) != "" {
		return
	}
	const handed = "abc123handedlineage"
	cmd := exec.Command(os.Args[0], "-test.run=TestOwnerHelperProcess")
	cmd.Env = append(os.Environ(), instanceProbeEnv+"=1", OwnerEnv+"="+handed)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper: %v\n%s", err, out)
	}
	_, got, ok := strings.Cut(string(out), ownerProbeMark)
	if !ok {
		t.Fatalf("helper 가 계보를 안 찍었다:\n%s", out)
	}
	got, _, _ = strings.Cut(got, "\n")
	if strings.TrimSpace(got) != handed {
		t.Errorf("물려받은 계보를 버렸다: %q, 준 것은 %q", strings.TrimSpace(got), handed)
	}
}

const ownerProbeMark = "owner-id="

// TestOwnerHelperProcess is not a test — see TestInstanceIDHelperProcess.
func TestOwnerHelperProcess(t *testing.T) {
	if os.Getenv(instanceProbeEnv) == "" {
		t.Skip("helper process")
	}
	fmt.Printf("%s%s\n", ownerProbeMark, AdoptOwner())
}
