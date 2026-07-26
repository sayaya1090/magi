package app

import "testing"

// The trouble check must read a status however the worker phrased it: the costliest mistake
// available is treating a blocked worker as done, and a model that appends its reason, bolds the
// line, or drops the space after the colon has still plainly reported failure.
func TestReportStatusClaimTolerantVsStrict(t *testing.T) {
	trouble := []string{
		"STATUS: FAILED",
		"STATUS: FAILED — could not install the toolchain",
		"**STATUS: FAILED**",
		"STATUS:FAILED",
		"status: failed",
		"  ## STATUS: BLOCKED — needs a credential",
	}
	for _, line := range trouble {
		got := reportStatusClaim(line)
		if got != "FAILED" && got != "BLOCKED" {
			t.Errorf("reportStatusClaim(%q) = %q, want FAILED or BLOCKED", line, got)
		}
	}
	// Not the frame at all — must stay empty so nothing is misread as a status.
	for _, line := range []string{"STATUS_OK", "echo STATUS_FAIL", "", "the status is fine", "STATUS"} {
		if got := reportStatusClaim(line); got != "" {
			t.Errorf("reportStatusClaim(%q) = %q, want empty", line, got)
		}
	}
	// The STRICT reader is unchanged: it still recognizes only the exact two-field frame, because
	// stripReportStatus uses it to drop OUR frame and must not eat a real work item.
	if reportStatusWord("STATUS: DONE") != "DONE" {
		t.Error("the strict reader must still accept the exact frame")
	}
	for _, line := range []string{"STATUS: FAILED — could not install", "**STATUS: DONE**"} {
		if got := reportStatusWord(line); got != "" {
			t.Errorf("the strict reader must stay strict for %q, got %q", line, got)
		}
	}
}
