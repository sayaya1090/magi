package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// A companion is findable even when the DIRECTORY it lives in is spelled like a glob pattern.
//
// The listing used `filepath.Glob(filepath.Join(dir, "daemon-*.sock"))`, and Glob reads the whole
// path as a pattern — the directory part included. A config or socket directory whose name holds a
// `[` is then a character class rather than a name, and the answer is an empty list with no error.
// Every companion the person is running becomes invisible, everywhere this is called.
//
// On Windows there is no way out of it: filepath disables escaping there, so the name cannot be
// quoted. Measured 2026-09-10 with a directory called `user[1]` — a socket sitting in it globbed to
// nothing.
//
// That directory is not exotic. MAGI_SOCKET_DIR exists so somebody can point the sockets somewhere
// of their own choosing, and an account name goes into the default one.
func TestACompanionIsFoundInADirectoryNamedLikeAPattern(t *testing.T) {
	for _, odd := range []string{"user[1]", "a*b", "q?", "plain"} {
		t.Run(odd, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), odd)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Skipf("이 파일시스템은 %q 라는 이름을 못 만든다: %v", odd, err)
			}
			sock := filepath.Join(dir, "daemon-x.sock")
			if err := os.WriteFile(sock, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			// Something that must NOT come back, so a listing that returns everything is not
			// mistaken for one that filters correctly.
			if err := os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0o600); err != nil {
				t.Fatal(err)
			}

			got, err := SocketsIn(dir, ".sock")
			if err != nil {
				t.Fatalf("SocketsIn: %v", err)
			}
			if len(got) != 1 || got[0] != sock {
				t.Errorf("%q 안의 소켓을 못 찾았다: %v", odd, got)
			}
		})
	}
}

// The suffix is what separates a socket from what rides beside it.
func TestSocketsInKeepsOnlyWhatTheSuffixNames(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"daemon-a.sock", "daemon-b.sock",
		"daemon-a.sock.load", "daemon-a.sock.lock", "daemon-a.sock.session",
		"notes.txt", "daemon-c.txt",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	socks, err := SocketsIn(dir, ".sock")
	if err != nil {
		t.Fatal(err)
	}
	if len(socks) != 2 {
		t.Errorf("소켓만 와야 하는데 %v", socks)
	}
	// Sorted, because Glob answered that way and the callers were written against it.
	if len(socks) == 2 && socks[0] > socks[1] {
		t.Errorf("정렬되지 않았다: %v", socks)
	}
	loads, err := SocketsIn(dir, ".sock.load")
	if err != nil {
		t.Fatal(err)
	}
	if len(loads) != 1 {
		t.Errorf("부하 파일만 와야 하는데 %v", loads)
	}
}

// "Nobody is running" and "could not look" are different answers, and List had a comment saying so.
//
// Glob could not tell them apart — its documentation is explicit that it ignores filesystem errors
// — so a directory that exists and cannot be read arrived as an empty list. A directory that is not
// there is still empty and no error: that one really is nobody running, and it is the ordinary case
// on a machine that has never started a daemon.
func TestADirectoryThatIsNotThereIsEmptyRatherThanAnError(t *testing.T) {
	got, err := SocketsIn(filepath.Join(t.TempDir(), "never-made"), ".sock")
	if err != nil {
		t.Errorf("없는 디렉토리는 「아무도 안 돈다」이지 에러가 아니다: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("없는 디렉토리에서 %v 가 나왔다", got)
	}
}
