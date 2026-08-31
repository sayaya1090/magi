package daemon

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Mine gossips only companions whose record was actually read. A live socket with NO record is a
// daemon mid-startup — the self-update restart unpublishes on the way down, and the successor is
// dialable for seconds before it republishes — and gossiping that window produced a SIGNED,
// identity-less member (no name, team, version) that overwrote peers' good rows for a whole gossip
// cycle and could re-elect a team's hub. The local List still shows it; the fleet does not hear it.
func TestMineDoesNotGossipARecordlessStartingDaemon(t *testing.T) {
	dir, err := os.MkdirTemp(shortRoot(), "magimine")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	// One healthy companion: record + live listener.
	good := publishFake(t, dir, "good", "s_1", acceptSilently)
	waitForSocket(t, good)
	// One mid-startup: a live listener and NO record (nothing published yet).
	// `filepath.Join` 이지 문자열 이어붙이기가 아니다 — 윈도우에서 `List` 는 `` 로 된 경로를
	// 돌려주는데 여기서 `/` 로 지으면 같은 파일이 다른 문자열이 되어 아래 비교가 통째로 헛돈다.
	bare := filepath.Join(dir, "daemon-bare.sock")
	ln, err := net.Listen("unix", bare)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go acceptSilently(ln)
	waitForSocket(t, bare)

	// The bare socket must be LIVE in the local list — that is what makes this test pin the
	// Session=="" branch and not the !Live one. If the probe ever starts requiring a protocol
	// answer, acceptSilently stops qualifying and this assertion says so instead of the test
	// silently passing for the wrong reason.
	found, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	var bareLive, bareListed bool
	for _, in := range found {
		if in.Socket == bare {
			bareListed, bareLive = true, in.Live
		}
	}
	if !bareListed {
		t.Fatal("the record-less socket is not in the local list at all — it should still show there")
	}
	if !bareLive {
		t.Fatal("the record-less socket is not Live — the gossip skip would fire on !Live, not on the " +
			"missing record, and this test would pin nothing")
	}

	ms := Mine(dir, time.Now())
	for _, m := range ms {
		if m.Socket == bare {
			t.Errorf("a record-less starting daemon was gossiped: %+v", m)
		}
	}
	var sawGood bool
	for _, m := range ms {
		if m.Socket == good {
			sawGood = true
			if m.Workdir == "" {
				t.Errorf("the healthy companion lost its identity: %+v", m)
			}
		}
	}
	if !sawGood {
		t.Error("the healthy companion was not gossiped at all")
	}
}
