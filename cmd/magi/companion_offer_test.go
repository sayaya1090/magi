package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/companionpolicy"
	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/mcpserve"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
)

func TestUserOfferedTasksDriveAdvertisementAndRefreshWithoutRestart(t *testing.T) {
	cfg, wd := shortTempDir(t), t.TempDir()
	sock := filepath.Join(cfg, "offers.sock")
	stop, err := daemon.Publish(sock, wd, "s-original", daemon.Identity{Name: "gwt", Role: "old", Can: 4, Does: []string{"learned-release"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	body := fmt.Sprintf("version=1\npurpose='Develop GWT plugins'\nresponsibilities=['Maintain the plugin']\ninstructions=['No publishing without request']\n[[tasks]]\nid='inspect-build'\ndescription='Diagnose GWT build failures'\nconstraints=['Report evidence']\n[[access]]\nresource=%q\nmode='read'\ndescription='Consumer source inspection'\n", filepath.ToSlash(wd))
	var out, errs bytes.Buffer
	if code := companionPolicyCommand(cfg, wd, true, strings.NewReader(body), &out, &errs); code != 0 {
		t.Fatal(code, errs.String())
	}
	if err = refreshCompanionAdvertisement(cfg, wd, sock, "legacy role"); err != nil {
		t.Fatal(err)
	}
	info, err := daemon.Published(sock)
	if err != nil {
		t.Fatal(err)
	}
	if info.Role != "Develop GWT plugins" || info.Can != 1 || strings.Join(info.Does, ",") != "task:inspect-build" || info.Session != "s-original" {
		t.Fatal(info)
	}
	st, _ := jsonl.New(t.TempDir())
	n, names := countCan(st, wd, cfg)
	if n != 1 || names[0] != "task:inspect-build" {
		t.Fatal(n, names)
	}
	d := daemonEngine{configDir: cfg, workdir: wd, card: func() mcpserve.Card { return mcpserve.Card{Name: "gwt"} }}
	about := d.About()
	for _, want := range []string{"Develop GWT plugins", "Maintain the plugin", "Diagnose GWT build failures", "Report evidence", "Consumer source inspection"} {
		if !strings.Contains(about, want) {
			t.Fatalf("missing %q: %s", want, about)
		}
	}
	c, err := companionpolicy.Load(cfg, wd)
	if err != nil {
		t.Fatal(err)
	}
	guide := companionpolicy.Guide(c)
	if !strings.Contains(guide, "inspect-build") || !strings.Contains(guide, "Consumer source inspection") {
		t.Fatal(guide)
	}
	path, _ := companionpolicy.Path(cfg, wd)
	before, _ := os.ReadFile(path)
	if code := companionPolicyCommand(cfg, wd, true, strings.NewReader("version=99"), &out, &errs); code == 0 {
		t.Fatal("invalid declaration saved")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid write destroyed existing declaration")
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = refreshCompanionAdvertisement(cfg, wd, sock, "legacy role"); err != nil {
		t.Fatal(err)
	}
	info, _ = daemon.Published(sock)
	if info.Can != 0 || len(info.Does) != 0 {
		t.Fatal("revoked tasks still advertised", info)
	}
}
