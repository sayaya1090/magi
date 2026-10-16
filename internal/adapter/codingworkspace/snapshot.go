package codingworkspace

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sayaya1090/magi/internal/atomicfile"
	task "github.com/sayaya1090/magi/internal/core/codingtask"
)

var safeDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

func validateFile(f *task.File) error {
	if f != nil && (!safeDigest.MatchString(f.Digest) || f.Size < 0 || f.Mode > 0777) {
		return fmt.Errorf("invalid snapshot metadata")
	}
	return nil
}

func excluded(name string) bool {
	switch name {
	case ".git", ".magi", ".codex", ".ssh", ".aws", "node_modules", ".venv", "venv", ".DS_Store":
		return true
	}
	return name == ".env" || (strings.HasPrefix(name, ".env.") && name != ".env.example")
}
func (m *Manager) snapshot(ctx context.Context, dir, root string, baselines ...task.Manifest) (task.Manifest, []string, error) {
	manifest := task.Manifest{}
	// Never infer that a tracked or already-managed source directory is merely
	// generated output. In particular, old task baselines keep their scope.
	protected := map[string]bool{}
	protect := func(path string) {
		for parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(path))); parent != "." && parent != "/"; parent = filepath.ToSlash(filepath.Dir(filepath.FromSlash(parent))) {
			protected[parent] = true
		}
	}
	for _, baseline := range baselines {
		for path := range baseline {
			protect(path)
		}
	}
	if insideGit, e := git(ctx, root, "rev-parse", "--is-inside-work-tree"); e == nil && insideGit == "true" {
		tracked, e := git(ctx, root, "ls-files", "-z", "--cached")
		if e != nil {
			return nil, nil, fmt.Errorf("cannot protect tracked source paths: %w", e)
		}
		for _, path := range strings.Split(tracked, "\x00") {
			if path != "" {
				protect(filepath.ToSlash(path))
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	skipped := []string{}
	var total int64
	maxFiles, maxBytes := m.MaxFiles, m.MaxBytes
	if maxFiles <= 0 || maxBytes <= 0 {
		return nil, nil, fmt.Errorf("snapshot requires positive file/byte budgets")
	}
	if err := os.MkdirAll(filepath.Join(dir, "blobs"), 0o700); err != nil {
		return nil, nil, err
	}
	// Bound retained versions as well as a single workspace snapshot. Repeated
	// reviews must not accumulate unlimited immutable backups.
	if m.MaxStoredBytes <= 0 {
		return nil, nil, fmt.Errorf("positive retained backup budget required")
	}
	blobsDir := filepath.Join(dir, "blobs")
	entries, err := os.ReadDir(blobsDir)
	if err != nil {
		return nil, nil, err
	}
	var stored int64
	for _, entry := range entries {
		info, e := entry.Info()
		if e != nil {
			return nil, nil, e
		}
		if !info.Mode().IsRegular() {
			return nil, nil, fmt.Errorf("unsupported backup entry: %s", entry.Name())
		}
		stored += info.Size()
	}
	if stored > m.MaxStoredBytes || len(entries) > 4*m.MaxFiles {
		return nil, nil, fmt.Errorf("retained backup budget exceeded; existing backups preserved")
	}
	count := len(entries)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("workspace root is not an ordinary directory")
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if excluded(entry.Name()) {
			skipped = append(skipped, rel)
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if !protected[rel] && generatedDirectory(path, entry.Name()) {
				skipped = append(skipped, rel)
				return filepath.SkipDir
			}
			return nil
		}
		if len(manifest) >= maxFiles {
			return fmt.Errorf("snapshot file budget exceeded (%d)", maxFiles)
		}
		f, err := m.readFile(ctx, root, rel, maxBytes-total)
		if err != nil {
			return err
		}
		total += f.Size
		b, err := readBounded(root, rel, maxBytes)
		if err != nil {
			return err
		}
		if hash(b) != f.Digest {
			return fmt.Errorf("file changed during snapshot: %s", rel)
		}
		blob := filepath.Join(blobsDir, f.Digest)
		existing, e := readBounded(blobsDir, f.Digest, maxBytes)
		if e == nil {
			if hash(existing) != f.Digest {
				return fmt.Errorf("backup blob is corrupt: %s", rel)
			}
		} else if os.IsNotExist(e) {
			if f.Size > m.MaxStoredBytes-stored || count >= 4*m.MaxFiles {
				return fmt.Errorf("retained backup budget exceeded; existing backups preserved")
			}
			if err := atomicfile.Write(blob, b, 0600); err != nil {
				return err
			}
			stored += f.Size
			count++
		} else {
			return e
		}
		manifest[rel] = f
		return nil
	})
	return manifest, skipped, err
}

func validRelative(rel string) bool {
	return !(rel == "" || rel == "." || filepath.IsAbs(rel) || strings.ContainsAny(rel, ":\\\x00") || filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel))) != rel) && rel != ".." && !strings.HasPrefix(rel, "../")
}
func target(root, rel string) (string, error) {
	if !validRelative(rel) {
		return "", fmt.Errorf("unsafe task path %q", rel)
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	if !inside(root, path) {
		return "", fmt.Errorf("task path escapes workspace")
	}
	current := root
	info, err := os.Lstat(current)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("workspace root changed")
	}
	for _, part := range strings.Split(rel, "/") {
		if excluded(part) {
			return "", fmt.Errorf("path is excluded from coding task scope: %s", rel)
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlinks are not supported in task snapshots: %s", rel)
		}
	}
	return path, nil
}
func readBounded(root, rel string, max int64) ([]byte, error) {
	path, err := target(root, rel)
	if err != nil {
		return nil, err
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("unsupported file type: %s", rel)
	}
	if before.Size() > max {
		return nil, fmt.Errorf("snapshot byte budget exceeded at %s", rel)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, opened) || !opened.Mode().IsRegular() {
		return nil, fmt.Errorf("file identity changed: %s", rel)
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("snapshot byte budget exceeded at %s", rel)
	}
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, fmt.Errorf("file changed while reading: %s", rel)
	}
	return b, nil
}
func (m *Manager) readFile(ctx context.Context, root, rel string, budget int64) (task.File, error) {
	if err := ctx.Err(); err != nil {
		return task.File{}, err
	}
	b, err := readBounded(root, rel, budget)
	if err != nil {
		return task.File{}, err
	}
	path, _ := target(root, rel)
	info, err := os.Lstat(path)
	if err != nil {
		return task.File{}, err
	}
	return task.File{Digest: hash(b), Mode: uint32(info.Mode().Perm()), Size: int64(len(b))}, nil
}
func (m *Manager) writeFile(dir, root, rel string, f *task.File) error {
	if err := validateFile(f); err != nil {
		return err
	}
	path, err := target(root, rel)
	if err != nil {
		return err
	}
	if f == nil {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	b, err := os.ReadFile(filepath.Join(dir, "blobs", f.Digest))
	if err != nil {
		return err
	}
	if hash(b) != f.Digest {
		return fmt.Errorf("backup blob is corrupt: %s", rel)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if _, err := target(root, rel); err != nil {
		return err
	}
	if err := atomicfile.Write(path, b, fs.FileMode(f.Mode)); err != nil {
		return err
	}
	return os.Chmod(path, fs.FileMode(f.Mode))
}

// Gradle marks its own build directory through the sibling module definition.
// Do not blanket-exclude every directory named build: src/build may be source.
func generatedDirectory(path, name string) bool {
	switch name {
	case ".gradle", ".kotlin", ".gradle-test-kit":
		return true
	}
	if name != "build" {
		return false
	}
	for _, marker := range []string{"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts"} {
		if info, err := os.Lstat(filepath.Join(filepath.Dir(path), marker)); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}
