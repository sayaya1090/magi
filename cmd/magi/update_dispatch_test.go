package main

import "testing"

// runUpdateCmd routes -update/-update-core/-update-plugins/-plugin-install to the
// right sub-actions. Swap the action seams so we assert routing without touching
// the network, git, or the real config dir.
func TestRunUpdateCmdRouting(t *testing.T) {
	var (
		core, plugins int
		install       string
	)
	reset := func() { core, plugins, install = 0, 0, "" }
	restore := func() {
		coreUpdateFn, pluginUpdateFn, pluginInstallFn = runCoreUpdate, runPluginUpdates, runPluginInstall
	}
	defer restore()
	coreUpdateFn = func() int { core++; return 0 }
	pluginUpdateFn = func(string) int { plugins++; return 0 }
	pluginInstallFn = func(url, pin string) int { install = url; return 0 }

	cases := []struct {
		name              string
		opts              updateOpts
		wantCore, wantPlg int
		wantInstall       string
	}{
		{"update (both)", updateOpts{core: true, plugins: true}, 1, 1, ""},
		{"core only", updateOpts{core: true}, 1, 0, ""},
		{"plugins only", updateOpts{plugins: true}, 0, 1, ""},
		{"install standalone wins", updateOpts{core: true, plugins: true, install: "git://x"}, 0, 0, "git://x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reset()
			if rc := runUpdateCmd(c.opts); rc != 0 {
				t.Fatalf("rc = %d, want 0", rc)
			}
			if core != c.wantCore || plugins != c.wantPlg || install != c.wantInstall {
				t.Errorf("routing = core%d plugins%d install%q, want core%d plugins%d install%q",
					core, plugins, install, c.wantCore, c.wantPlg, c.wantInstall)
			}
		})
	}
}

// A failing sub-action must surface a non-zero exit code.
func TestRunUpdateCmdPropagatesFailure(t *testing.T) {
	restore := func() { coreUpdateFn, pluginUpdateFn = runCoreUpdate, runPluginUpdates }
	defer restore()
	coreUpdateFn = func() int { return 3 }
	pluginUpdateFn = func(string) int { return 0 }
	if rc := runUpdateCmd(updateOpts{core: true, plugins: true}); rc != 3 {
		t.Fatalf("rc = %d, want 3 (core failure propagated)", rc)
	}
}
