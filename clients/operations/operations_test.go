package operations

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestQueriesFixEnvironmentAndRejectScopeBypass(t *testing.T) {
	for _, runtime := range []string{"docker", "kubernetes"} {
		c := Config{Runtime: runtime, Context: "test", Namespace: "assigned", Resources: []string{"service"}}
		var calls int
		var got []string
		a := Adapter{Config: c, Run: func(_ context.Context, bin string, args ...string) ([]byte, error) {
			calls++
			got = append([]string{bin}, args...)
			return []byte("healthy"), nil
		}}
		if _, err := a.Call(context.Background(), c.Target(), "logs", json.RawMessage(`{"resource":"service"}`)); err != nil {
			t.Fatal(err)
		}
		prefix := []string{"docker", "--context", "test"}
		if runtime == "kubernetes" {
			prefix = []string{"kubectl", "--context", "test", "--namespace", "assigned"}
		}
		if !reflect.DeepEqual(got[:len(prefix)], prefix) {
			t.Fatal(got)
		}
		for _, args := range []string{`{"resource":"other"}`, `{"resource":"service","context":"prod"}`, `null`, `{"resource":"-exec"}`} {
			if _, err := a.Call(context.Background(), c.Target(), "logs", json.RawMessage(args)); err == nil {
				t.Fatal("scope bypass accepted", args)
			}
		}
		if _, err := a.Call(context.Background(), "foreign", "status", json.RawMessage(`{"resource":"service"}`)); err == nil {
			t.Fatal("foreign environment accepted")
		}
		if calls != 1 {
			t.Fatal("rejected request executed")
		}
	}
}
func TestOutputIsBoundedWithoutFailingChildWrites(t *testing.T) {
	var b boundedOutput
	p := make([]byte, 100000)
	n, err := b.Write(p)
	if err != nil || n != len(p) || len(b.data) != 65536 || !b.truncated {
		t.Fatal("output bound failed")
	}
}

func TestNativeKubernetesContextNamesRemainBoundArguments(t *testing.T) {
	c := Config{Runtime: "kubernetes", Context: "handbook-dev/api.example.com:6443/user", Namespace: "handbook-dev", Resources: []string{"pod"}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	a := Adapter{Config: c, Run: func(_ context.Context, bin string, args ...string) ([]byte, error) {
		if bin != "kubectl" || args[1] != c.Context || args[3] != c.Namespace {
			t.Fatal(args)
		}
		return []byte("Running"), nil
	}}
	if _, err := a.Call(context.Background(), c.Target(), "status", json.RawMessage(`{"resource":"pod"}`)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"--context=other", "context\nother", "context with spaces", "$(shell)"} {
		c.Context = bad
		if c.Validate() == nil {
			t.Fatal("unsafe context accepted", bad)
		}
	}
}
