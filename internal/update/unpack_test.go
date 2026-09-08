package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// tarGz builds a release-shaped tarball: the binary plus the attribution files goreleaser adds.
func tarGz(t *testing.T, names map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, body := range names {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipOf(t *testing.T, names map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range names {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// What a release serves is an archive, and the binary has to come out of it.
//
// This step did not exist: the downloaded .tar.gz went straight to Commit, so a gzip stream was
// written over the running program. Measured 2026-09-08 — `magi --update-core` ended in
// "the new binary failed --version: … exec format error" every single time, and the pre-flight
// rollback put the old binary back. The update had therefore never once succeeded, and the tidy
// rollback made each attempt read as a bad build rather than a broken updater.
func TestTheBinaryComesOutOfTheReleaseArchive(t *testing.T) {
	const bin = "\x7fELF...the binary..."
	files := map[string]string{
		"LICENSE": "MIT", "NOTICE": "notices", "README.md": "# magi",
		"THIRD_PARTY_LICENSES": "deps", "magi": bin,
	}
	for _, c := range []struct {
		what string
		blob []byte
	}{
		{"tar.gz (every platform but Windows)", tarGz(t, files)},
		{"zip (Windows)", zipOf(t, map[string]string{"LICENSE": "MIT", "magi.exe": bin})},
		// goreleaser can be told to wrap the archive in a directory. Ours does not, but a reader
		// should not have to know that to know this works.
		{"wrapped in a directory", tarGz(t, map[string]string{"magi_darwin_arm64/magi": bin})},
	} {
		got, err := binaryFromArchive(c.blob)
		if err != nil {
			t.Errorf("%s: %v", c.what, err)
			continue
		}
		if string(got) != bin {
			t.Errorf("%s: got %d bytes, want the binary — an archive was written over the "+
				"running program", c.what, len(got))
		}
	}
}

// A bare binary passes through. A Source that serves one is not made to start shipping archives.
func TestSomethingThatIsNotAnArchivePassesThrough(t *testing.T) {
	raw := []byte("\x7fELFnot an archive")
	got, err := binaryFromArchive(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Error("a bare binary was not passed through unchanged")
	}
}

// An archive without the binary is an error, not an empty write.
//
// Silently committing zero bytes would replace the running program with nothing, and the
// pre-flight's report would be about a truncated binary rather than about an archive that never
// held one.
func TestAnArchiveWithNoBinaryIsRefused(t *testing.T) {
	_, err := binaryFromArchive(tarGz(t, map[string]string{"LICENSE": "MIT", "README.md": "# magi"}))
	if err == nil {
		t.Fatal("an archive with no magi in it was accepted")
	}
	if !strings.Contains(err.Error(), "no magi binary") {
		t.Errorf("the error does not say what is wrong: %v", err)
	}
}

// Nothing several directories deep is taken for the binary.
//
// An archive is untrusted input — this one comes over TLS from a release, but the function cannot
// tell that — and "any file called magi anywhere in here" is a wider rule than the layout needs.
func TestOnlyTheRootOrOneWrapperCounts(t *testing.T) {
	_, err := binaryFromArchive(tarGz(t, map[string]string{"a/b/c/magi": "deep"}))
	if err == nil {
		t.Error("a file three directories down was taken as the release binary")
	}
}

// End to end: an update from a real-shaped release archive SUCCEEDS.
//
// The three tests above call binaryFromArchive directly, and none of them noticed when the call was
// taken out of RunCommit — which is the shipped defect itself.
//
// Asserting on the file afterwards does not catch it either, and the reason is worth writing down:
// when the archive is committed, the pre-flight refuses it and rolls back, so what is on disk is
// the ORIGINAL binary, unharmed. Every trace of the mistake is tidied away by the safety net. That
// is exactly why this survived — each attempt read as a bad release rather than a broken updater.
//
// So this asserts the update WORKS. The "binary" in the archive is a shell script that answers
// --version, which is what Verify runs.
//
// The digest is of the ARCHIVE, as checksums.txt lists it. Verifying after unpacking would compare
// the wrong bytes and nothing would ever install, so the order is pinned here too.
func TestAnUpdateFromARealReleaseArchiveSucceeds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in binary is a shell script")
	}
	const newBin = "#!/bin/sh\necho 'magi 9.9.9'\n"
	archive := tarGz(t, map[string]string{
		"LICENSE": "MIT", "NOTICE": "notices", "README.md": "# magi",
		"THIRD_PARTY_LICENSES": "deps", "magi": newBin,
	})
	sum := sha256.Sum256(archive)

	dir := t.TempDir()
	target := filepath.Join(dir, "magi")
	if err := os.WriteFile(target, []byte("#!/bin/sh\necho 'magi 0.1.0'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := fakeSource{
		rel: Release{Version: "v9.9.9", URL: "x", SHA256: hex.EncodeToString(sum[:])},
		bin: archive,
	}
	res, err := RunCommit(context.Background(), src, "v0.1.0", target)
	if err != nil {
		t.Fatalf("the update failed: %v\n\nThis is the shipped defect if it says \"exec format "+
			"error\": the archive was written over the binary and the pre-flight rolled it back.", err)
	}
	if !res.Updated {
		t.Errorf("RunCommit reported %+v", res)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != newBin {
		t.Errorf("what landed is not the binary from the archive: %.30q", got)
	}
}
