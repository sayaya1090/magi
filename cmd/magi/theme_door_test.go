package main

import (
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/config"
)

func TestTheDaemonEngineTellsItsConfiguredTheme(t *testing.T) {
	var eng any = daemonEngine{theme: config.ThemeConfig{Dark: map[string]string{"primary": "#010203"}}}
	tt, ok := eng.(daemon.ThemeTeller)
	if !ok {
		t.Fatal("daemonEngine does not tell its theme")
	}
	dark, light := tt.ThemePalettes()
	if dark["primary"] != "#010203" || light["primary"] == "" || dark["surfaceContainer"] == "" {
		t.Errorf("dark %v light primary %q", dark["primary"], light["primary"])
	}
}
