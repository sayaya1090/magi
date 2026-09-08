package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The socket directory can be split from the config directory. A unix address holds 100 bytes;
// Windows' %AppData%\magi under a long user name is past it, and moving the whole config tree to a
// short path cost the Office companions the config.toml and plugins the person's usual magi uses.
// Only the sockets move now, and every lister follows.
func TestSocketDirCanBeSplitFromTheConfigDir(t *testing.T) {
	cfg, socks := t.TempDir(), t.TempDir()
	if got := SocketPath(cfg, "/w/proj"); filepath.Dir(got) != cfg {
		t.Fatalf("without MAGI_SOCKET_DIR the socket lives beside the config: %s", got)
	}
	t.Setenv("MAGI_SOCKET_DIR", socks)
	got := SocketPath(cfg, "/w/proj")
	if filepath.Dir(got) != socks {
		t.Fatalf("with MAGI_SOCKET_DIR the socket must move there: %s", got)
	}
	if SessionFile(got) != got+".session" {
		t.Errorf("the session file rides the socket path: %s", SessionFile(got))
	}
	if SocketDir(cfg) != socks {
		t.Errorf("listers must read the same directory: %s", SocketDir(cfg))
	}
	// And the listing that every fleet view is built on reads there too — a companion published
	// under the socket dir is found by a lister that was handed the CONFIG dir.
	sock := filepath.Join(socks, "daemon-proj-deadbeef.sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(SessionFile(sock), []byte(`{"socket":"`+sock+`","workdir":"/w/proj","session":"s_1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	infos, err := List(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Workdir != "/w/proj" {
		t.Errorf("List(configDir) did not read the socket dir: %+v", infos)
	}
}

// The way out of a too-long socket path is MAGI_SOCKET_DIR, and the sentence has to say so.
//
// It said MAGI_CONFIG_DIR for a long time, in four places, and nothing pinned it — so it aged
// silently into advice that causes the incident SocketDir exists because of: moving the whole config
// tree somewhere short leaves the companions reading a config.toml the person's usual magi never
// wrote. A person following this sentence would do exactly that.
func TestTheWayOutOfALongPathIsTheSocketDir(t *testing.T) {
	err := TooLong(strings.Repeat("x", maxSocketPath+1))
	if err == nil {
		t.Fatal("a path past the limit was accepted")
	}
	if !strings.Contains(err.Error(), "MAGI_SOCKET_DIR") {
		t.Errorf("the way out is not named: %v", err)
	}
	// And it must not send somebody to the one that breaks things — except to say not to.
	if i := strings.Index(err.Error(), "MAGI_CONFIG_DIR"); i >= 0 &&
		!strings.Contains(err.Error(), "not MAGI_CONFIG_DIR") {
		t.Errorf("the sentence still points at MAGI_CONFIG_DIR: %v", err)
	}
	if TooLong(strings.Repeat("x", maxSocketPath)) != nil {
		t.Error("a path at the limit was refused")
	}
}
