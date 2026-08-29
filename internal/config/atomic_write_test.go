package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// A reader landing mid-edit sees the old config or the new one, never a blank: the write used to
// truncate-then-write, and a torn read decoded as an errorless zero Config — no model, no
// permission, empty allow — which is worse than an error (hunted with a 4×3000 probe: 324 blank
// reads before the fix).
func TestConcurrentEditNeverServesABlankConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("model = \"seed\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if err := SetKey(path, "", "model", "m"+string(rune('a'+i%26))); err != nil {
				t.Errorf("set: %v", err)
				return
			}
		}
	}()
	blank := 0
	for i := 0; i < 3000; i++ {
		b, err := os.ReadFile(path)
		if err != nil || len(b) == 0 {
			blank++
		}
	}
	close(stop)
	wg.Wait()
	if blank > 0 {
		t.Fatalf("%d of 3000 reads landed on a blank or missing config", blank)
	}
}
