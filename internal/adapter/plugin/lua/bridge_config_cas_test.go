package lua

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestPluginStateCASRejectsStaleWriterAndPreservesCorruption(t *testing.T) {
	dir := t.TempDir()
	p := &plugin{name: "engram", host: &Host{dataDir: dir}}
	q := &plugin{name: "engram", host: &Host{dataDir: dir}}
	empty := ""
	if err := p.writeStoreValue("queue", "initial", &empty); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, writer := range []*plugin{p, q} {
		wg.Add(1)
		go func(writer *plugin) {
			defer wg.Done()
			<-start
			expected := "initial"
			results <- writer.writeStoreValue("queue", "changed", &expected)
		}(writer)
	}
	close(start)
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("expected one CAS winner, got %d", wins)
	}
	path := filepath.Join(dir, "plugin-data", "engram.json")
	if err := os.WriteFile(path, []byte("broken store"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.writeStoreValue("queue", "new", nil); err == nil {
		t.Fatal("corrupt store replaced")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "broken store" {
		t.Fatalf("corruption evidence lost: %q", b)
	}
}
