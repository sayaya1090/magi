package lua

import (
	"path/filepath"
	"regexp"
	"strings"
)

// perms is the parsed permission grant of a plugin. Capabilities a plugin did
// not declare are denied at the bridge layer (SPEC F-PLUGIN). Permission strings:
//
//	fs:read:<prefix>   fs:write:<prefix>   net:<host>   exec:<cmd>
//	config:read:<key>  config:write:<key>   (dotted config.toml key; trailing * = prefix)
//
// A bare "fs:read" / "fs:write" with no prefix grants the whole workdir.
type perms struct {
	fsRead      []string
	fsWrite     []string
	net         []string
	exec        []string
	configRead  []string
	configWrite []string
}

// denyConfigSections are config.toml areas a plugin may never read or write, even with a
// matching grant: they can run commands (mcp/hooks) or relax the security posture
// (allow/deny/permission/sandbox/profile/allow_domains). Matched (lower-cased) on the
// dotted key's first segment.
var denyConfigSections = map[string]bool{
	"mcp": true, "hooks": true, "allow": true, "deny": true,
	"permission": true, "sandbox": true, "profile": true, "allow_domains": true,
}

// configKeyRe is the strict charset a config key must match: dot-separated segments of
// [A-Za-z0-9_-]. This rejects spaces, newlines, brackets, quotes and '=' BEFORE the key
// reaches config.SetKey (whose leaf is interpolated raw) — closing a TOML-injection hole
// where a newline in the key could open a `[hooks]`/`[mcp]` table and bypass the deny-list.
var configKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`)

// validConfigKey reports whether key is a well-formed dotted config key (safe to edit).
func validConfigKey(key string) bool { return configKeyRe.MatchString(key) }

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
		case len(parts) == 3 && parts[0] == "config" && parts[1] == "read":
			p.configRead = append(p.configRead, parts[2])
		case len(parts) == 3 && parts[0] == "config" && parts[1] == "write":
			p.configWrite = append(p.configWrite, parts[2])
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

// allowConfigRead/allowConfigWrite report whether a granted config key pattern covers the
// dotted key. A grant "a.b.*" matches "a.b" and "a.b.<anything>"; "*" matches all; an exact
// "a.b" matches only "a.b". The deny-list (checked separately) overrides any grant.
func (p perms) allowConfigRead(key string) bool  { return configKeyMatch(p.configRead, key) }
func (p perms) allowConfigWrite(key string) bool { return configKeyMatch(p.configWrite, key) }

func configKeyMatch(grants []string, key string) bool {
	for _, g := range grants {
		switch {
		case g == "*":
			return true
		case strings.HasSuffix(g, ".*"):
			pre := strings.TrimSuffix(g, ".*")
			if key == pre || strings.HasPrefix(key, pre+".") {
				return true
			}
		case g == key:
			return true
		}
	}
	return false
}

// configKeyDenied reports whether a dotted config key falls under a never-allowed section
// (its first segment is in denyConfigSections) — blocked even with a matching grant.
func configKeyDenied(key string) bool {
	first := key
	if i := strings.IndexByte(key, '.'); i >= 0 {
		first = key[:i]
	}
	return denyConfigSections[strings.ToLower(first)] // TOML matches sections case-insensitively
}

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
