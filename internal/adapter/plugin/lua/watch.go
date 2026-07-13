package lua

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// LoadDir loads every immediate subdirectory of root that contains a plugin.toml.
// It returns the successfully loaded plugin names and any per-plugin load errors.
func (h *Host) LoadDir(ctx context.Context, root string) ([]string, []error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []error{err}
	}
	var loaded []string
	var errs []error
	for _, e := range entries {
		if !e.IsDir() {
			// DirEntry.IsDir is false for a symlink even when it points at a
			// directory — and `ln -s <repo>/plugins/engram ~/.config/magi/plugins/`
			// is the documented install. Follow the link before skipping.
			fi, err := os.Stat(filepath.Join(root, e.Name()))
			if err != nil || !fi.IsDir() {
				continue
			}
		}
		dir := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "plugin.toml")); err != nil {
			continue
		}
		info, err := h.Load(ctx, dir)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		loaded = append(loaded, info.Name)
	}
	return loaded, errs
}

// Watch watches all currently-loaded plugin directories and hot-reloads a plugin
// when its files change (debounced). It runs until ctx is cancelled. Reload
// preserves session state because that lives in the core, not the plugin.
func (h *Host) Watch(ctx context.Context) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	// dir -> plugin name, so an event path maps back to a plugin.
	dirToName := map[string]string{}
	h.mu.Lock()
	for name, p := range h.plugins {
		if w.Add(p.dir) == nil {
			dirToName[filepath.Clean(p.dir)] = name
		}
	}
	h.mu.Unlock()

	go func() {
		defer w.Close()
		var mu sync.Mutex
		timers := map[string]*time.Timer{}

		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
					continue
				}
				name, ok := dirToName[filepath.Clean(filepath.Dir(ev.Name))]
				if !ok {
					continue
				}
				// Debounce bursts of events (editors write several at once).
				mu.Lock()
				if t := timers[name]; t != nil {
					t.Stop()
				}
				timers[name] = time.AfterFunc(200*time.Millisecond, func() {
					_ = h.Reload(name)
				})
				mu.Unlock()
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return nil
}
