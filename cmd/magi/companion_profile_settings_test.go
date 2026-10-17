package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/companionpolicy"
	"github.com/sayaya1090/magi/internal/config"
)

func TestCompanionAutomaticDescriptionSwitch(t *testing.T) {
	d, dir, wd := settingsEngine(t)
	if !companionProfileNeeded(dir, wd) || !companionProfileUnpublished(dir, wd) {
		t.Fatal("default must enable missing descriptions and introduction")
	}
	if _, err := d.ConfigSet(context.Background(), "companion_auto_describe", "off", "companion"); err != nil {
		t.Fatal(err)
	}
	if companionProfileNeeded(dir, wd) || companionProfileUnpublished(dir, wd) {
		t.Fatal("off must disable extraction and introduction without restarting")
	}
	if _, err := d.ConfigSet(context.Background(), "companion_auto_describe", "yes", "companion"); err == nil {
		t.Fatal("invalid mode accepted")
	}
	if _, err := d.ConfigSet(context.Background(), "companion_auto_describe", "on", "companion"); err != nil {
		t.Fatal(err)
	}
	if !companionProfileNeeded(dir, wd) {
		t.Fatal("on must become effective")
	}
	if err := config.SetKey(filepath.Join(dir, "config.toml"), "", "companion_auto_describe", "off"); err != nil {
		t.Fatal(err)
	}
	if !companionProfileNeeded(dir, wd) {
		t.Fatal("companion layer must win")
	}
}

func TestProfileUIEditsPreserveAuthorityAndRejectBadWrites(t *testing.T) {
	d, dir, wd := settingsEngine(t)
	policy := `version = 1
purpose = "old"
[[access]]
resource = "workspace"
mode = "read"
[[tasks]]
id = "old"
description = "old work"
`
	if companionPolicyCommand(dir, wd, true, strings.NewReader(policy), os.Stdout, os.Stderr) != 0 {
		t.Fatal("seed")
	}
	value := `{"purpose":"new","responsibilities":["sources"],"tasks":[{"id":"build","description":"build source and report results","constraints":["local"]}]}`
	item, err := d.ConfigSet(context.Background(), "companion.profile", value, "")
	if err != nil {
		t.Fatal(err)
	}
	var p editableCompanionProfile
	if err = json.Unmarshal([]byte(item.Value), &p); err != nil || p.Purpose != "new" {
		t.Fatalf("readback: %s %v", item.Value, err)
	}
	c, err := companionpolicy.Load(dir, wd)
	if err != nil || len(c.Access) != 1 || c.Access[0].Mode != "read" {
		t.Fatalf("authority changed: %+v %v", c, err)
	}
	if companionProfileNeeded(dir, wd) || companionProfileUnpublished(dir, wd) {
		t.Fatal("existing description must prevent paid extraction and introduction")
	}
	path, _ := companionpolicy.Path(dir, wd)
	before, _ := os.ReadFile(path)
	for _, bad := range []string{`null`, `{"purpose":"x","access":[]}`, `{} {}`, `{"purpose":"x","tasks":[{"id":"bad id","description":"x"}]}`} {
		if _, err = d.setProfileSetting(bad); err == nil {
			t.Errorf("accepted %s", bad)
		}
		after, _ := os.ReadFile(path)
		if string(before) != string(after) {
			t.Fatal("invalid edit changed file")
		}
	}
}
