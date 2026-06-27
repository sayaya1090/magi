package lua

import (
	"path/filepath"
	"strings"
)

// perms is the parsed permission grant of a plugin. Capabilities a plugin did
// not declare are denied at the bridge layer (SPEC F-PLUGIN). Permission strings:
//
//	fs:read:<prefix>   fs:write:<prefix>   net:<host>   exec:<cmd>
//
// A bare "fs:read" / "fs:write" with no prefix grants the whole workdir.
type perms struct {
	fsRead  []string
	fsWrite []string
	net     []string
	exec    []string
}

func parsePerms(decls []string) perms {
	var p perms
	for _, d := range decls {
		parts := strings.SplitN(d, ":", 3)
		switch {
		case len(parts) >= 2 && parts[0] == "fs" && parts[1] == "read":
			p.fsRead = append(p.fsRead, prefixOf(parts))
		case len(parts) >= 2 && parts[0] == "fs" && parts[1] == "write":
			p.fsWrite = append(p.fsWrite, prefixOf(parts))
		case len(parts) == 2 && parts[0] == "net":
			p.net = append(p.net, parts[1])
		case len(parts) == 2 && parts[0] == "exec":
			p.exec = append(p.exec, parts[1])
		}
	}
	return p
}

func prefixOf(parts []string) string {
	if len(parts) == 3 && parts[2] != "" {
		return parts[2]
	}
	return "." // whole workdir
}

// allowFSRead reports whether rel (a workdir-relative, cleaned path) is covered
// by a granted read prefix.
func (p perms) allowFSRead(rel string) bool  { return underAny(p.fsRead, rel) }
func (p perms) allowFSWrite(rel string) bool { return underAny(p.fsWrite, rel) }

func (p perms) allowNet(host string) bool { return contains(p.net, host) }
func (p perms) allowExec(cmd string) bool { return contains(p.exec, cmd) }

func underAny(prefixes []string, rel string) bool {
	rel = filepath.ToSlash(filepath.Clean(rel))
	for _, pre := range prefixes {
		pre = filepath.ToSlash(filepath.Clean(pre))
		if pre == "." || pre == "" {
			return true
		}
		if rel == pre || strings.HasPrefix(rel, pre+"/") {
			return true
		}
	}
	return false
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v || x == "*" {
			return true
		}
	}
	return false
}
