package filelock

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestKernelLockSurvivesContentionAndReleasesOnProcessDeath(t *testing.T) {
	if path := os.Getenv("MAGI_TEST_FILELOCK"); path != "" {
		release, err := Acquire(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		fmt.Println("locked")
		bufio.NewReader(os.Stdin).ReadString('\n')
		return
	}
	path := filepath.Join(t.TempDir(), "writer.lock")
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestKernelLockSurvivesContentionAndReleasesOnProcessDeath$")
	cmd.Env = append(os.Environ(), "MAGI_TEST_FILELOCK="+path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatalf("child not ready: %q %v", line, err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	held, err := tryLock(f)
	f.Close()
	if err != nil || held {
		t.Fatalf("second process acquired held lock: %v %v", held, err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	release, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Acquire(ctx, path); err != context.Canceled {
		t.Fatalf("cancellation ignored: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file must not be unlinked: %v", err)
	}
}
