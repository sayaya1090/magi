package builtin

import (
	"strings"
	"testing"
)

// The three tables here have to agree, and nothing made them. What went wrong was the quiet
// direction: prereqBinary knew how to probe for "rust" and "dotnet", and no hint ever declared
// either — a lookup with no producer, which is this codebase's most-repaired shape. In the other
// direction gopls declared "go" while prereqBootstrap had no entry for it, so a missing Go
// toolchain produced the bare `go install …` that comes back "command not found" — the dead end
// this file exists to prevent.
func TestTheInstallTablesAgree(t *testing.T) {
	// Server coverage and per-OS commands belong to TestInstallHintCompleteness, which drives the
	// real serverFor — stronger than re-deriving the server list here. This one only checks wiring.
	declared := map[string]bool{}
	for server, h := range lspInstallHints {
		if h.prereq == "" {
			continue
		}
		declared[h.prereq] = true
		if prereqBinary(h.prereq) == "" {
			t.Errorf("%s declares prereq %q, which prereqBinary cannot probe", server, h.prereq)
		}
		if _, ok := prereqBootstrap[h.prereq]; !ok {
			t.Errorf("%s declares prereq %q, which prereqBootstrap cannot install", server, h.prereq)
		}
	}
	// …and the other direction: nothing sits in those tables that no server asks for.
	for p := range prereqBootstrap {
		if !declared[p] {
			t.Errorf("prereqBootstrap has %q and no server declares it", p)
		}
	}
	if len(declared) == 0 {
		t.Fatal("no server declares a prereq, so this checked nothing")
	}
	// A bootstrap must answer on every platform, or the advice silently drops the line that says
	// how to get the runtime it just told you that you need.
	for p, h := range prereqBootstrap {
		for _, goos := range []string{"darwin", "linux", "windows"} {
			if h.forOS(goos) == "" {
				t.Errorf("prereq %s has no bootstrap for %s", p, goos)
			}
		}
	}
}

// A linux command that assumes a package manager says which one. The reader can translate an
// apt-get line to dnf if they know that is what they are reading, and cannot if it merely failed.
func TestLinuxCommandsNameTheirDistro(t *testing.T) {
	check := func(what, cmd string) {
		if !strings.Contains(cmd, "apt") {
			return
		}
		if !strings.Contains(cmd, "Debian") && !strings.Contains(cmd, "Ubuntu") {
			t.Errorf("%s: linux command uses apt without naming the distro: %q", what, cmd)
		}
	}
	for server, h := range lspInstallHints {
		check(server, h.forOS("linux"))
	}
	for p, h := range prereqBootstrap {
		check("prereq "+p, h.forOS("linux"))
	}
}

func TestServerInstallOSOverride(t *testing.T) {
	// clangd has per-OS overrides — each must select its own command.
	if cmd, _ := serverInstall("clangd", "darwin"); !strings.Contains(cmd, "brew") {
		t.Errorf("darwin clangd = %q, want brew", cmd)
	}
	if cmd, _ := serverInstall("clangd", "linux"); !strings.Contains(cmd, "apt") {
		t.Errorf("linux clangd = %q, want apt", cmd)
	}
	if cmd, _ := serverInstall("clangd", "windows"); !strings.Contains(cmd, "choco") {
		t.Errorf("windows clangd = %q, want choco", cmd)
	}
	// A generic-only server falls back regardless of OS.
	if cmd, _ := serverInstall("rust-analyzer", "windows"); !strings.Contains(cmd, "rustup") {
		t.Errorf("rust-analyzer = %q, want rustup", cmd)
	}
	// Unknown server → empty.
	if cmd, prereq := serverInstall("nope-ls", "linux"); cmd != "" || prereq != "" {
		t.Errorf("unknown server = (%q,%q), want empty", cmd, prereq)
	}
}

func TestComposeInstallAdvice(t *testing.T) {
	// (a) No prerequisite → server command only, no bootstrap. clangd installs from the OS package
	// manager and needs nothing first. (This case used rust-analyzer until rust-analyzer got the
	// prereq it always needed: `rustup component add` cannot run without rustup.)
	adv := composeInstallAdvice("clangd", "darwin", true)
	if !strings.Contains(adv, "brew install llvm") {
		t.Errorf("clangd advice missing command: %q", adv)
	}
	if strings.Contains(adv, "first install") {
		t.Errorf("clangd has no prereq, must not bootstrap: %q", adv)
	}
	// …and the one that DOES now declare it bootstraps.
	adv = composeInstallAdvice("rust-analyzer", "darwin", true)
	if !strings.Contains(adv, "first install rust") || !strings.Contains(adv, "rustup component add rust-analyzer") {
		t.Errorf("rust-analyzer should bootstrap rustup then install: %q", adv)
	}

	// (b) Prerequisite present (missing=false) → server command only.
	adv = composeInstallAdvice("typescript-language-server", "linux", false)
	if strings.Contains(adv, "first install") {
		t.Errorf("prereq present must not show bootstrap: %q", adv)
	}
	if !strings.Contains(adv, "npm install -g typescript-language-server") {
		t.Errorf("missing server command: %q", adv)
	}

	// (c) Prerequisite missing → bootstrap BEFORE server command, ordered.
	adv = composeInstallAdvice("typescript-language-server", "linux", true)
	iBoot := strings.Index(adv, "first install node")
	iCmd := strings.Index(adv, "npm install -g typescript-language-server")
	if iBoot < 0 || iCmd < 0 || iBoot > iCmd {
		t.Errorf("bootstrap must precede server command: boot=%d cmd=%d in %q", iBoot, iCmd, adv)
	}
	if !strings.Contains(adv, "nodejs") { // linux node bootstrap
		t.Errorf("linux node bootstrap missing: %q", adv)
	}

	// (d) Unknown server → empty.
	if adv := composeInstallAdvice("nope-ls", "linux", true); adv != "" {
		t.Errorf("unknown server advice = %q, want empty", adv)
	}
}

func TestPrereqBinaryMapping(t *testing.T) {
	cases := map[string]string{
		"node": "npm", "java": "java",
		"go": "go", "rust": "rustup", "ruby": "gem", "dotnet": "dotnet",
		"": "", "unknown": "",
	}
	for prereq, want := range cases {
		if got := prereqBinary(prereq); got != want {
			t.Errorf("prereqBinary(%q) = %q, want %q", prereq, got, want)
		}
	}
	// python was a prereq label no server ever declared; it went out with the tables it orphaned.
	if got := prereqBinary("python"); got != "" {
		t.Errorf("prereqBinary(\"python\") = %q — nothing declares it, so it should be gone", got)
	}
	// Every bootstrap runtime must resolve to a probe binary.
	for prereq := range prereqBootstrap {
		if prereqBinary(prereq) == "" {
			t.Errorf("prereqBootstrap has %q but prereqBinary can't probe it", prereq)
		}
	}
}

// TestInstallHintCompleteness asserts every server serverFor can emit has an
// install hint with a non-empty command on each target OS — so adding a new
// language server without an install hint fails the build's tests.
func TestInstallHintCompleteness(t *testing.T) {
	// One representative file per extension family serverFor recognizes.
	exts := []string{
		".ts", ".tsx", ".js", ".jsx", ".mts", ".cts", ".mjs", ".cjs",
		".py", ".pyi", ".rs", ".c", ".h", ".cc", ".cpp", ".hpp", ".m", ".mm",
		".java", ".kt", ".kts", ".scala", ".sc", ".groovy", ".gradle",
		".cs", ".fs", ".fsx", ".rb", ".php", ".lua", ".sh", ".bash", ".pl", ".pm",
		".swift", ".zig", ".hs", ".ml", ".mli", ".dart", ".ex", ".exs", ".jl",
		".vue", ".svelte", ".html", ".htm", ".css", ".scss", ".less",
		".json", ".jsonc", ".yaml", ".yml", ".toml", ".tf", ".tfvars", ".md", ".markdown",
	}
	for _, os := range []string{"darwin", "linux", "windows"} {
		for _, ext := range exts {
			srv, ok := serverFor("file" + ext)
			if !ok {
				t.Errorf("serverFor(%q) not recognized — test list out of sync", ext)
				continue
			}
			cmd, _ := serverInstall(srv.argv[0], os)
			if cmd == "" {
				t.Errorf("%s (%s): no install hint for server %q", ext, os, srv.argv[0])
			}
		}
	}
	// gopls is not in serverFor (Go uses the gopls CLI path) but shares the table.
	if cmd, _ := serverInstall("gopls", "linux"); cmd == "" {
		t.Errorf("gopls install hint missing")
	}
}
