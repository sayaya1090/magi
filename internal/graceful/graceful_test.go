package graceful

import (
	"os"
	"testing"
)

// reexec on a binary that cannot be started must RETURN the error, with this process still running —
// an update that cannot relaunch leaves the old build serving, it does not take the daemon down. (We
// test the failure path deliberately: the success path replaces the process image, which would end
// the test binary itself.)
func TestReexecOnAMissingBinaryReturnsWithoutReplacingTheProcess(t *testing.T) {
	err := reexec("/nonexistent/definitely/not/a/real/binary", []string{"magi"}, os.Environ())
	if err == nil {
		t.Fatal("reexec on a missing binary returned nil — a failed relaunch must surface as an error, " +
			"not silently succeed")
	}
	// Reaching here at all is half the point: the process was not replaced.
}
