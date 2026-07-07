// Package update implements self-update: checking for a newer release,
// verifying its checksum, and atomically replacing the running binary across
// platforms (Windows can't overwrite a running .exe, so it renames it aside).
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Release describes the latest available release for the current platform.
type Release struct {
	Version string // e.g. "v0.3.1"
	URL     string // download URL of the platform asset
	SHA256  string // expected checksum (hex), optional
}

// Source provides release metadata and downloads assets. GitHubSource is the
// production implementation; tests use a fake.
type Source interface {
	Latest(ctx context.Context) (Release, error)
	Download(ctx context.Context, url string) ([]byte, error)
}

// Result reports what an update run did.
type Result struct {
	Updated  bool
	From, To string
	Skipped  string // reason if not updated
}

// Run checks for a newer release and, if found, downloads, verifies, and
// installs it over execPath.
func Run(ctx context.Context, src Source, currentVersion, execPath string) (Result, error) {
	rel, err := src.Latest(ctx)
	if err != nil {
		return Result{}, err
	}
	if !IsNewer(currentVersion, rel.Version) {
		return Result{Skipped: "already up to date", From: currentVersion, To: rel.Version}, nil
	}
	bin, err := src.Download(ctx, rel.URL)
	if err != nil {
		return Result{}, err
	}
	if rel.SHA256 != "" {
		if err := verifySHA256(bin, rel.SHA256); err != nil {
			return Result{}, err
		}
	}
	if err := Apply(bin, execPath); err != nil {
		return Result{}, err
	}
	return Result{Updated: true, From: currentVersion, To: rel.Version}, nil
}

// Apply writes newBin over target atomically. It writes to a temp file in the
// same directory (so rename is atomic), then swaps it in. On Windows the running
// binary is moved aside first because it cannot be overwritten while executing.
func Apply(newBin []byte, target string) error {
	target, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".magi-update-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(newBin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		old := target + ".old"
		_ = os.Remove(old)
		if err := os.Rename(target, old); err != nil {
			return err
		}
		if err := os.Rename(tmpName, target); err != nil {
			_ = os.Rename(old, target) // rollback
			return err
		}
		return nil
	}
	return os.Rename(tmpName, target)
}

// verifySHA256 checks data against an expected hex digest.
func verifySHA256(data []byte, expected string) error {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, strings.TrimSpace(expected)) {
		return fmt.Errorf("checksum mismatch: got %s want %s", got, expected)
	}
	return nil
}

// IsNewer reports whether latest is a strictly newer semantic version than
// current. A non-release current (e.g. "dev") is always considered older so a
// dev build can update to any tagged release.
func IsNewer(current, latest string) bool {
	lv, ok := parseSemver(latest)
	if !ok {
		return false
	}
	cv, ok := parseSemver(current)
	if !ok {
		return true // current is "dev"/unparseable → allow update
	}
	for i := 0; i < 3; i++ {
		if lv[i] != cv[i] {
			return lv[i] > cv[i]
		}
	}
	return false
}

// Policy is what the interactive auto-check should do about an available release,
// derived purely from which semver component advanced.
type Policy int

const (
	PolicyNone   Policy = iota // no newer release, or versions unparseable → do nothing
	PolicyNotify               // patch bump (x.y.Z): non-intrusive banner only
	PolicyForce                // minor or major bump (x.Y.z / X.y.z): auto-install
)

// UpdatePolicy decides notify-vs-force from the version delta: a patch-only bump
// is Notify, a minor OR major bump is Force ("중간 이상 = 강제"). Anything that
// isn't a strictly-newer, fully-parseable pair is PolicyNone, so a dev build or a
// malformed tag never triggers a forced install (fail-safe: we don't force).
func UpdatePolicy(current, latest string) Policy {
	lv, ok := parseSemver(latest)
	if !ok {
		return PolicyNone
	}
	cv, ok := parseSemver(current)
	if !ok {
		// current is "dev"/unparseable: offer the update, but never force it.
		return PolicyNotify
	}
	if lv[0] != cv[0] {
		if lv[0] > cv[0] {
			return PolicyForce
		}
		return PolicyNone // latest older (major)
	}
	if lv[1] != cv[1] {
		if lv[1] > cv[1] {
			return PolicyForce
		}
		return PolicyNone // latest older (minor)
	}
	if lv[2] > cv[2] {
		return PolicyNotify
	}
	return PolicyNone // same or older
}

// parseSemver parses "vX.Y.Z" / "X.Y.Z" (ignoring any pre-release suffix).
func parseSemver(s string) ([3]int, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

// AssetName returns the release-archive base name for the current platform. It
// MUST match the goreleaser archives name_template ("magi_{{ .Os }}_{{ .Arch }}"),
// which is lowercase — e.g. magi_darwin_arm64, magi_windows_amd64.
func AssetName() string {
	return fmt.Sprintf("magi_%s_%s", runtime.GOOS, runtime.GOARCH)
}
