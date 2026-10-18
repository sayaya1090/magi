package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestMaintenanceRemainsEnabledAfterInitialDescription(t *testing.T) {
	d, dir, wd := settingsEngine(t)
	var out, why bytes.Buffer
	if companionProfileCommand(dir, wd, strings.NewReader(discoveredProfile), &out, &why) != 0 {
		t.Fatal(why.String())
	}
	if companionProfileNeeded(dir, wd) {
		t.Fatal("initial introduction should be complete")
	}
	if !companionMaintenanceEnabled(dir, wd) {
		t.Fatal("populated description disabled future updates")
	}
	raw, err := d.ProfileMaintenanceStatus(context.Background(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	var status map[string]any
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		t.Fatal(err)
	}
	if status["enabled"] != true || status["id"] == "" || status["running"] != false {
		t.Fatal(status)
	}
	if _, err := d.ConfigSet(context.Background(), "companion_auto_describe", "off", "companion"); err != nil {
		t.Fatal(err)
	}
	if companionMaintenanceEnabled(dir, wd) {
		t.Fatal("off ignored")
	}
	if _, err := d.ProfileMaintenanceStatus(context.Background(), "source", true); err == nil {
		t.Fatal("disabled refresh accepted")
	}
}
