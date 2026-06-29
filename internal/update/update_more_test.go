package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// errSource lets a test inject failures at the Latest or Download step.
type errSource struct {
	rel    Release
	relErr error
	bin    []byte
	dlErr  error
}

func (s errSource) Latest(context.Context) (Release, error) { return s.rel, s.relErr }
func (s errSource) Download(context.Context, string) ([]byte, error) {
	return s.bin, s.dlErr
}

// A failure fetching release metadata must surface, not silently no-op.
func TestRunPropagatesLatestError(t *testing.T) {
	want := errors.New("network down")
	_, err := Run(context.Background(), errSource{relErr: want}, "v0.1.0", "/nonexistent")
	if !errors.Is(err, want) {
		t.Fatalf("Run error = %v, want %v", err, want)
	}
}

// A download failure must surface AND must not have touched the target binary.
func TestRunPropagatesDownloadErrorLeavesBinary(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "magi")
	if err := os.WriteFile(target, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := errSource{rel: Release{Version: "v9.9.9", URL: "x"}, dlErr: errors.New("boom")}
	if _, err := Run(context.Background(), src, "v0.1.0", target); err == nil {
		t.Fatal("expected the download error to surface")
	}
	if got, _ := os.ReadFile(target); string(got) != "OLD" {
		t.Errorf("binary must be untouched on download failure, got %q", got)
	}
}

// Version compare must be NUMERIC per component, not lexical — the classic
// "0.10.0 < 0.9.0" string-compare bug.
func TestIsNewerNumericPerComponent(t *testing.T) {
	if !IsNewer("v0.9.0", "v0.10.0") {
		t.Error("0.10.0 must be newer than 0.9.0 (numeric, not lexical)")
	}
	if IsNewer("v0.10.0", "v0.9.0") {
		t.Error("0.9.0 must NOT be newer than 0.10.0")
	}
	if !IsNewer("v1.2.9", "v1.3.0") {
		t.Error("minor bump should be newer")
	}
}

// parseSemver strips a v prefix and any -prerelease/+build suffix, and rejects
// shapes that aren't exactly X.Y.Z of integers.
func TestParseSemver(t *testing.T) {
	ok := map[string][3]int{
		"v1.2.3":        {1, 2, 3},
		"1.2.3":         {1, 2, 3},
		" v1.2.3 ":      {1, 2, 3},
		"v1.2.3-rc.1":   {1, 2, 3},
		"1.2.3+build.5": {1, 2, 3},
		"v0.10.0":       {0, 10, 0},
	}
	for in, want := range ok {
		got, valid := parseSemver(in)
		if !valid || got != want {
			t.Errorf("parseSemver(%q) = %v,%v want %v,true", in, got, valid, want)
		}
	}
	for _, bad := range []string{"1.2", "1.2.3.4", "1.x.3", "", "v", "dev", "latest"} {
		if _, valid := parseSemver(bad); valid {
			t.Errorf("parseSemver(%q) should be invalid", bad)
		}
	}
}

// Checksum compare is case-insensitive and whitespace-tolerant (digests come
// from files/headers), but a real mismatch must fail.
func TestVerifySHA256(t *testing.T) {
	data := []byte("payload")
	sum := sha256.Sum256(data)
	hexsum := hex.EncodeToString(sum[:])

	if err := verifySHA256(data, hexsum); err != nil {
		t.Errorf("matching lowercase digest should pass: %v", err)
	}
	if err := verifySHA256(data, strings.ToUpper(hexsum)); err != nil {
		t.Errorf("matching UPPERCASE digest should pass (case-insensitive): %v", err)
	}
	if err := verifySHA256(data, "  "+hexsum+"\n"); err != nil {
		t.Errorf("surrounding whitespace should be tolerated: %v", err)
	}
	if err := verifySHA256(data, "deadbeef"); err == nil {
		t.Error("a wrong digest must fail")
	}
}

// Apply must fail cleanly (no panic) when the target's directory doesn't exist,
// rather than appearing to succeed.
func TestApplyFailsWhenDirMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-dir", "magi")
	if err := Apply([]byte("x"), missing); err == nil {
		t.Fatal("Apply should fail when the target directory does not exist")
	}
}
