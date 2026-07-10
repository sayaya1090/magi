package app

import (
	"strings"
	"testing"
)

// On Windows the bash tool runs via PowerShell, so the environment block the model
// sees must report "powershell" (not "sh") and steer it away from Linux/GNU syntax —
// reporting "sh" there made models emit commands that fail on the host.
func TestBuildEnvInfoWindowsReportsPowerShell(t *testing.T) {
	got := buildEnvInfo("windows", "amd64", "", `C:\work`, "2026-07-10", "")
	if !strings.Contains(got, "- OS: windows (amd64)") {
		t.Fatalf("missing OS line: %q", got)
	}
	if !strings.Contains(got, "- Shell: powershell") {
		t.Fatalf("Windows shell should be powershell, got: %q", got)
	}
	if strings.Contains(got, "- Shell: sh") {
		t.Fatalf("Windows must not report an sh shell: %q", got)
	}
	if !strings.Contains(got, "PowerShell") || !strings.Contains(got, "NOT Linux/GNU") {
		t.Fatalf("Windows note should steer to PowerShell syntax: %q", got)
	}
}

// A stray $SHELL must not override the Windows PowerShell reality.
func TestBuildEnvInfoWindowsIgnoresShellEnv(t *testing.T) {
	got := buildEnvInfo("windows", "amd64", "/usr/bin/bash", `C:\work`, "2026-07-10", "")
	if !strings.Contains(got, "- Shell: powershell") {
		t.Fatalf("Windows must ignore $SHELL and report powershell, got: %q", got)
	}
}

// Non-Windows honors $SHELL (basename), falling back to sh, and adds no Windows note.
func TestBuildEnvInfoUnix(t *testing.T) {
	got := buildEnvInfo("linux", "arm64", "/usr/bin/zsh", "/home/u/p", "2026-07-10", "")
	if !strings.Contains(got, "- Shell: zsh") {
		t.Fatalf("expected zsh from $SHELL basename, got: %q", got)
	}
	if strings.Contains(got, "PowerShell") {
		t.Fatalf("non-Windows must not carry the PowerShell note: %q", got)
	}

	fallback := buildEnvInfo("darwin", "arm64", "", "/home/u/p", "2026-07-10", "")
	if !strings.Contains(fallback, "- Shell: sh") {
		t.Fatalf("empty $SHELL should fall back to sh, got: %q", fallback)
	}
}

// macOS steers to Homebrew (no system apt/yum) and honors $SHELL; no PowerShell/distro lines.
func TestBuildEnvInfoDarwinHomebrew(t *testing.T) {
	got := buildEnvInfo("darwin", "arm64", "/bin/zsh", "/Users/u/p", "2026-07-10", "")
	if !strings.Contains(got, "- Shell: zsh") {
		t.Fatalf("darwin should honor $SHELL, got: %q", got)
	}
	if !strings.Contains(got, "- Package manager: brew") || !strings.Contains(got, "brew install") {
		t.Fatalf("darwin should steer to Homebrew, got: %q", got)
	}
	if strings.Contains(got, "PowerShell") || strings.Contains(got, "Distro:") {
		t.Fatalf("darwin must carry neither PowerShell nor distro lines: %q", got)
	}
}

// On Linux the environment block carries the distro and its package manager (parsed
// from /etc/os-release) so the model uses the right install command instead of guessing.
func TestBuildEnvInfoLinuxDistroAndPackageManager(t *testing.T) {
	ubuntu := `NAME="Ubuntu"
VERSION_ID="22.04"
PRETTY_NAME="Ubuntu 22.04.3 LTS"
ID=ubuntu
ID_LIKE=debian`
	got := buildEnvInfo("linux", "amd64", "/bin/bash", "/app", "2026-07-10", ubuntu)
	if !strings.Contains(got, "- Distro: Ubuntu 22.04.3 LTS") {
		t.Fatalf("expected PRETTY_NAME distro line, got: %q", got)
	}
	if !strings.Contains(got, "- Package manager: apt") || !strings.Contains(got, "apt-get install -y") {
		t.Fatalf("Ubuntu should resolve to apt, got: %q", got)
	}

	// Alpine → apk (the case models most often get wrong by assuming apt).
	alpine := "PRETTY_NAME=\"Alpine Linux v3.19\"\nID=alpine\n"
	if g := buildEnvInfo("linux", "amd64", "", "/app", "2026-07-10", alpine); !strings.Contains(g, "- Package manager: apk") {
		t.Fatalf("Alpine should resolve to apk, got: %q", g)
	}

	// Missing/unreadable os-release → no distro line, no crash.
	if g := buildEnvInfo("linux", "amd64", "", "/app", "2026-07-10", ""); strings.Contains(g, "Distro:") {
		t.Fatalf("no os-release should yield no distro line, got: %q", g)
	}
}

// ID_LIKE resolves derivatives that have no direct entry (Rocky→rhel, Mint→ubuntu→debian).
func TestLinuxPackageManagerViaIDLike(t *testing.T) {
	cases := []struct{ id, idLike, want string }{
		{"fedora", "", "dnf"},
		{"rocky", "rhel centos fedora", "dnf"},
		{"linuxmint", "ubuntu debian", "apt"},
		{"manjaro", "arch", "pacman"},
		{"opensuse-leap", "suse opensuse", "zypper"},
		{"gentoo", "", ""}, // unknown → empty
	}
	for _, c := range cases {
		if got := linuxPackageManager(c.id, c.idLike); got != c.want {
			t.Errorf("linuxPackageManager(%q,%q)=%q, want %q", c.id, c.idLike, got, c.want)
		}
	}
}
