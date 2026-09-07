package daemon

import (
	"os"
	"path/filepath"
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
