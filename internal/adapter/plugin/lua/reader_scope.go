package lua

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// readerScope identifies the file or the directory searched by this call.
func readerScope(workdir, tool string, args json.RawMessage) (string, error) {
	var input struct{ Path string }
	if err := json.Unmarshal(args, &input); err != nil {
		return "", err
	}
	switch tool {
	case "read", "grep", "glob", "list":
	default:
		return "", fmt.Errorf("%s is not a scoped reader", tool)
	}
	if workdir == "" {
		return "", fmt.Errorf("reader has no workspace")
	}
	base, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		return "", err
	}
	target := input.Path
	if !filepath.IsAbs(target) {
		target = filepath.Join(workdir, target)
	}
	real, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, real)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("reader path is outside the workspace")
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if tool == "read" {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("reader path is not a regular file")
		}
		return "file:" + filepath.ToSlash(rel), nil
	} else if !info.IsDir() {
		return "", fmt.Errorf("search path is not a directory")
	}
	if rel == "." {
		return "", fmt.Errorf("automatic directory readers require a workspace subdirectory")
	}
	return tool + ":" + filepath.ToSlash(rel), nil
}

func (p *plugin) bridgeReaderScope(L *lua.LState) int {
	p.requireCap(L, "call_tools")
	scope, err := readerScope(p.env.Workdir, L.CheckString(1), json.RawMessage(L.CheckString(2)))
	if err != nil {
		return fail(L, "reader_scope: "+err.Error())
	}
	L.Push(lua.LString(scope))
	return 1
}

// Only actual matches whose resolved files remain under the approved search directory
// become readable, for the duration of this script.
func scopedGlobFiles(workdir, dir string, content json.RawMessage) []string {
	var paths []string
	if len(content) > 65536 || json.Unmarshal(content, &paths) != nil || len(paths) > 64 {
		return nil
	}
	var scopes []string
	for _, path := range paths {
		raw, _ := json.Marshal(map[string]string{"path": path})
		scope, err := readerScope(workdir, "read", raw)
		if err == nil && strings.HasPrefix(scope, "file:"+dir+"/") {
			scopes = append(scopes, scope)
		}
	}
	return scopes
}
