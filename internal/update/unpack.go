package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// maxBinary caps what is read out of an archive. The binary is ~23MB; a hundred is room to grow and
// still refuses an archive whose header claims a size no release has.
const maxBinary = 100 << 20

// binaryFromArchive pulls the magi executable out of a release archive.
//
// # Why this exists
//
// Releases ship archives, not bare binaries: goreleaser builds magi_<os>_<arch>.tar.gz everywhere
// and .zip on Windows, each holding the binary plus LICENSE, NOTICE, README and
// THIRD_PARTY_LICENSES. The updater downloaded that archive and handed the bytes straight to
// Commit, so what got written over the running program was a gzip stream. Measured 2026-09-08:
//
//	magi --update-core
//	→ update rolled back, the previous build is restored: the new binary failed --version:
//	  fork/exec /private/tmp/magi-r1: exec format error
//
// The checksum passed, because it is the checksum OF THE ARCHIVE and the archive is what arrived
// intact. The pre-flight then refused to leave a non-executable in place and put the old binary
// back — which is why nobody lost a working install, and also why this went unnoticed: every
// attempt ended in a tidy rollback that read like a bad build rather than a broken updater.
//
// # What it accepts
//
// A file named "magi" or "magi.exe", at the archive root or one directory down (goreleaser can be
// told to wrap; ours does not, and a reader should not have to know which). Anything else in there
// is attribution and is skipped.
func binaryFromArchive(blob []byte) ([]byte, error) {
	switch {
	case len(blob) >= 2 && blob[0] == 0x1f && blob[1] == 0x8b:
		return fromTarGz(blob)
	case len(blob) >= 4 && string(blob[:4]) == "PK\x03\x04":
		return fromZip(blob)
	}
	// Not an archive. A source that serves the binary itself is fine and is what the tests here
	// used to assume; passing it through keeps that working rather than making this a new
	// requirement on every Source implementation.
	return blob, nil
}

// wantedName reports whether a path inside an archive is the executable.
func wantedName(name string) bool {
	name = strings.TrimPrefix(path.Clean(strings.ReplaceAll(name, "\\", "/")), "./")
	if strings.Count(name, "/") > 1 {
		return false // deeper than one wrapper directory
	}
	base := path.Base(name)
	return base == "magi" || base == "magi.exe"
}

func fromTarGz(blob []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(blob))
	if err != nil {
		return nil, fmt.Errorf("update: reading the archive: %w", err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("update: reading the archive: %w", err)
		}
		if h.Typeflag != tar.TypeReg || !wantedName(h.Name) {
			continue
		}
		return readCapped(tr)
	}
	return nil, errors.New("update: the release archive has no magi binary in it")
}

func fromZip(blob []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		return nil, fmt.Errorf("update: reading the archive: %w", err)
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !wantedName(f.Name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("update: reading the archive: %w", err)
		}
		defer rc.Close()
		return readCapped(rc)
	}
	return nil, errors.New("update: the release archive has no magi binary in it")
}

// readCapped reads at most maxBinary+1 bytes and refuses anything that hits the cap, so a crafted
// or corrupt archive cannot decompress into memory without bound.
func readCapped(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxBinary+1))
	if err != nil {
		return nil, fmt.Errorf("update: reading the archive: %w", err)
	}
	if len(b) > maxBinary {
		return nil, fmt.Errorf("update: the binary in the archive is larger than %d bytes", maxBinary)
	}
	return b, nil
}
