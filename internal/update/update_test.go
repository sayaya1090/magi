package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		cur, latest string
		want        bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.2.0", "v0.2.1", true},
		{"1.0.0", "1.0.0", false},
		{"v0.3.0", "v0.2.9", false},
		{"dev", "v0.1.0", true},         // dev updates to any release
		{"v0.1.0", "garbage", false},    // unparseable latest → no update
		{"v1.2.3-rc1", "v1.2.3", false}, // same base version
	}
	for _, c := range cases {
		if got := IsNewer(c.cur, c.latest); got != c.want {
			t.Errorf("IsNewer(%q,%q)=%v want %v", c.cur, c.latest, got, c.want)
		}
	}
}

func TestApplyReplacesBinary(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "magi")
	if err := os.WriteFile(target, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Apply([]byte("NEW-BINARY"), target); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "NEW-BINARY" {
		t.Errorf("content=%q want NEW-BINARY", got)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(target)
		if info.Mode()&0o100 == 0 {
			t.Errorf("updated binary should be executable, mode=%v", info.Mode())
		}
	}
}

// fakeSource serves a scripted release + binary, optionally with a checksum.
type fakeSource struct {
	rel Release
	bin []byte
}

func (f fakeSource) Latest(context.Context) (Release, error)          { return f.rel, nil }
func (f fakeSource) Download(context.Context, string) ([]byte, error) { return f.bin, nil }

func TestRunUpdatesWhenNewer(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "magi")
	os.WriteFile(target, []byte("OLD"), 0o755)

	bin := []byte("BRAND-NEW")
	sum := sha256.Sum256(bin)
	src := fakeSource{rel: Release{Version: "v9.9.9", URL: "x", SHA256: hex.EncodeToString(sum[:])}, bin: bin}

	res, err := Run(context.Background(), src, "v0.1.0", target)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Updated || res.To != "v9.9.9" {
		t.Errorf("result=%+v want updated to v9.9.9", res)
	}
	if got, _ := os.ReadFile(target); string(got) != "BRAND-NEW" {
		t.Errorf("binary not replaced: %q", got)
	}
}

func TestRunSkipsWhenCurrent(t *testing.T) {
	src := fakeSource{rel: Release{Version: "v0.1.0", URL: "x"}, bin: []byte("x")}
	res, err := Run(context.Background(), src, "v0.1.0", "/nonexistent")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Updated {
		t.Errorf("should not update when current == latest")
	}
}

func TestRunRejectsBadChecksum(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "magi")
	os.WriteFile(target, []byte("OLD"), 0o755)

	src := fakeSource{rel: Release{Version: "v9.9.9", URL: "x", SHA256: "deadbeef"}, bin: []byte("NEW")}
	if _, err := Run(context.Background(), src, "v0.1.0", target); err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if got, _ := os.ReadFile(target); string(got) != "OLD" {
		t.Errorf("binary must not change on checksum failure, got %q", got)
	}
}

// AssetName must match the lowercase goreleaser archive name for this platform.
func TestAssetName(t *testing.T) {
	want := "magi_" + runtime.GOOS + "_" + runtime.GOARCH
	if got := AssetName(); got != want {
		t.Errorf("AssetName() = %q, want %q", got, want)
	}
	if AssetName() != strings.ToLower(AssetName()) {
		t.Errorf("AssetName() must be lowercase: %q", AssetName())
	}
}
