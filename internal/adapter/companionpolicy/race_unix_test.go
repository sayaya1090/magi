//go:build unix

package companionpolicy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
)

func TestReadOperationRefusesFIFOAndSymlinkReplacement(t *testing.T) {
	cfg, wd, root := t.TempDir(), t.TempDir(), t.TempDir()
	writePolicy(t, cfg, wd, root, "")
	if err := syscall.Mkfifo(filepath.Join(root, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	res, err := Call(context.Background(), cfg, wd, "source", json.RawMessage(`{"path":"fifo"}`))
	if err == nil && !res.IsError {
		t.Fatal("FIFO accepted")
	}
	os.WriteFile(filepath.Join(wd, "secret"), []byte("PRIVATE"), 0600)
	os.WriteFile(filepath.Join(root, "safe"), []byte("PUBLIC"), 0600)
	link := filepath.Join(root, "swapping")
	os.Symlink("safe", link)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			os.Remove(link)
			os.Symlink(filepath.Join(wd, "secret"), link)
			os.Remove(link)
			os.Symlink("safe", link)
		}
	}()
	defer func() { close(stop); wg.Wait() }()
	for range 100 {
		r, err := Call(context.Background(), cfg, wd, "source", json.RawMessage(`{"path":"swapping"}`))
		if err == nil && !r.IsError {
			var data struct{ Lines []string }
			json.Unmarshal(r.Content, &data)
			for _, line := range data.Lines {
				if line != "PUBLIC" {
					t.Fatalf("escaped root: %s", line)
				}
			}
		}
	}
}
