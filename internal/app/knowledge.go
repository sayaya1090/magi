package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sayaya1090/magi/internal/port"
)

// ManageProjectKnowledge is for the local user's IDE controls, never a model tool.
func (a *App) ManageProjectKnowledge(ctx context.Context, workdir string, action port.KnowledgeAction) ([]port.KnowledgeItem, error) {
	manager, ok := a.cfg.Experience.(port.KnowledgeManager)
	if !ok {
		return nil, fmt.Errorf("project knowledge management is unavailable")
	}
	if action.Action == "import" {
		importer, ok := a.cfg.Experience.(port.LegacySkillImporter)
		if !ok {
			return nil, fmt.Errorf("legacy skill import is unavailable")
		}
		name := strings.TrimPrefix(action.Name, "legacy:")
		if !strings.HasPrefix(action.Name, "legacy:") || name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
			return nil, fmt.Errorf("invalid legacy skill selection")
		}
		body, err := legacySkillDocument(workdir, name)
		if err != nil {
			return nil, err
		}
		if digestString(body) != action.Revision {
			return nil, fmt.Errorf("legacy skill changed; reload and review it again")
		}
		if err := importer.ImportLegacySkill(ctx, port.Skill{Name: name, Description: name, Body: body}); err != nil {
			return nil, err
		}
		action = port.KnowledgeAction{}
	}
	items, err := manager.ManageKnowledge(ctx, action)
	if err != nil {
		return nil, err
	}
	importer, canImport := a.cfg.Experience.(port.LegacySkillImporter)
	if !canImport || importer == nil {
		return items, nil
	}
	entries, err := legacySkillEntries(workdir)
	if os.IsNotExist(err) {
		return items, nil
	}
	if err != nil {
		return nil, err
	}
	existing := map[string]bool{}
	for _, item := range items {
		existing[item.Name] = true
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		h := sha256.Sum256([]byte(entry.Name()))
		if existing["managed:import_"+hex.EncodeToString(h[:])] {
			continue
		}
		// Existing automatic managed objects are not duplicated as user imports.
		if activity, ok := a.cfg.Experience.(port.ExperienceActivityStore); ok {
			refs, err := activity.ExperienceReferences(ctx, entry.Name())
			if err != nil {
				return nil, err
			}
			if len(refs) > 0 {
				continue
			}
		}
		body, err := legacySkillDocument(workdir, entry.Name())
		if err != nil {
			continue
		}
		items = append(items, port.KnowledgeItem{Name: "legacy:" + entry.Name(), Kind: "skill", Description: entry.Name(), Revision: digestString(body), Importable: true})
	}
	return items, nil
}

func digestString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func legacySkillEntries(workdir string) ([]os.DirEntry, error) {
	root, err := os.OpenRoot(workdir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	dir, err := root.Open(filepath.Join(".claude", "skills"))
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return dir.ReadDir(-1)
}

func legacySkillDocument(workdir, name string) (string, error) {
	root, err := os.OpenRoot(workdir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	file, err := root.Open(filepath.Join(".claude", "skills", name, "SKILL.md"))
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 256*1024 {
		return "", fmt.Errorf("legacy skill must be a regular document up to 256 KiB")
	}
	body, err := io.ReadAll(io.LimitReader(file, 256*1024+1))
	if err != nil {
		return "", err
	}
	if len(body) == 0 || len(body) > 256*1024 {
		return "", fmt.Errorf("invalid legacy skill document size")
	}
	return string(body), nil
}
