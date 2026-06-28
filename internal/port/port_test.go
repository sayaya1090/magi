package port

import "testing"

func TestSandboxConfined(t *testing.T) {
	if (SandboxSpec{}).Confined() {
		t.Error("zero-value spec must not be confined")
	}
	if !(SandboxSpec{Mode: "read-only"}).Confined() || !(SandboxSpec{Mode: "workspace-write"}).Confined() {
		t.Error("read-only and workspace-write must be confined")
	}
	if (SandboxSpec{Mode: "full"}).Confined() {
		t.Error("full mode is unconfined")
	}
}
