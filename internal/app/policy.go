package app

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

// Policy is the guardrail decision engine that sits above interactive permission
// prompting. It composes three concerns drawn from the sandbox design (sandbox × approval
// axes) and a reference agent (pattern rules + bash command analysis):
//
//   - deny rules  — a hard floor: matching calls are blocked even under "allow".
//     Secret-looking paths ship denied by default so a prompt-injected agent
//     can't read/exfiltrate .env, keys, or credentials.
//   - allow rules — skip the interactive prompt for trusted calls (in "ask").
//   - bash scan   — destructive / pipe-to-shell / network-egress / secret-path
//     commands force a prompt even under "allow"/"auto" (deny under "deny").
//
// Rules are written "Tool(spec)" — e.g. Bash(git push:*), Read(**/.env),
// WebFetch(domain:example.com). spec is a glob; for WebFetch, "domain:x" matches
// the URL host (and its subdomains).
type Policy struct {
	allow []policyRule
	deny  []policyRule
	// allowDomains, when non-empty, restricts WebFetch/bash egress to these hosts
	// (and subdomains); any other host is denied. Empty = no host allowlist.
	allowDomains []string
}

type policyRule struct {
	tool    string // lower-cased tool name; "*" = any
	domain  bool   // spec was "domain:..." (match URL host)
	raw     string // original spec
	re      *regexp.Regexp
	hostPat string // for domain rules
}

// secretGlobs are paths denied by default for file tools and flagged in bash
// commands — credentials and key material that should never be read or written
// by an autonomous agent without explicit confirmation.
var secretGlobs = []string{
	"**/.env", "**/.env.*", "**/*.pem", "**/*.key", "**/id_rsa", "**/id_dsa",
	"**/id_ecdsa", "**/id_ed25519", "**/.ssh/**", "**/.aws/credentials",
	"**/.aws/config", "**/.netrc", "**/.npmrc", "**/.pypirc",
	"**/secrets/**", "**/*.secret", "**/credentials.json",
}

// bashDestructive matches commands whose blast radius is large and irreversible.
var bashDestructive = []*regexp.Regexp{
	regexp.MustCompile(`\brm\s+(-[a-zA-Z]*\s+)*-[a-zA-Z]*[rf][a-zA-Z]*\b`), // rm -rf / -fr
	regexp.MustCompile(`\bgit\s+push\b.*--force\b`),
	regexp.MustCompile(`\bgit\s+push\b.*\s-f\b`),
	regexp.MustCompile(`\bgit\s+reset\s+--hard\b`),
	regexp.MustCompile(`\bgit\s+clean\s+-[a-zA-Z]*f`),
	regexp.MustCompile(`\b(dd|mkfs|fdisk)\b`),
	regexp.MustCompile(`\bchmod\s+-R\b`),
	regexp.MustCompile(`\bchown\s+-R\b`),
	regexp.MustCompile(`:\(\)\s*\{.*\}`), // fork bomb :(){ :|:& };:
	regexp.MustCompile(`>\s*/dev/sd[a-z]`),
}

// bashPipeToShell matches "download then execute" — the classic injection vector.
var bashPipeToShell = regexp.MustCompile(`(?:curl|wget|fetch)\b[^|]*\|\s*(?:sudo\s+)?(?:ba)?sh\b`)

// bashEgress matches commands that reach the network (outbound).
var bashEgress = regexp.MustCompile(`\b(curl|wget|nc|ncat|netcat|telnet|ssh|scp|sftp|rsync)\b`)

// newPolicy builds a Policy from allow/deny rule strings plus the default secret
// deny rules and an optional egress host allowlist.
func newPolicy(allow, deny, allowDomains []string) *Policy {
	p := &Policy{allowDomains: normHosts(allowDomains)}
	for _, r := range allow {
		if pr, ok := parseRule(r); ok {
			p.allow = append(p.allow, pr)
		}
	}
	// Default secret protections come first, then user deny rules.
	for _, g := range secretGlobs {
		for _, t := range []string{"read", "write", "edit", "multiedit"} {
			if pr, ok := parseRule(t + "(" + g + ")"); ok {
				p.deny = append(p.deny, pr)
			}
		}
	}
	for _, r := range deny {
		if pr, ok := parseRule(r); ok {
			p.deny = append(p.deny, pr)
		}
	}
	return p
}

// parseRule parses "Tool(spec)" into a policyRule. Returns ok=false on garbage.
func parseRule(s string) (policyRule, bool) {
	s = strings.TrimSpace(s)
	open := strings.IndexByte(s, '(')
	if open <= 0 || !strings.HasSuffix(s, ")") {
		return policyRule{}, false
	}
	tool := strings.ToLower(strings.TrimSpace(s[:open]))
	spec := s[open+1 : len(s)-1]
	pr := policyRule{tool: tool, raw: spec}
	if rest, ok := strings.CutPrefix(spec, "domain:"); ok {
		pr.domain = true
		pr.hostPat = strings.ToLower(strings.TrimSpace(rest))
		return pr, true
	}
	// placeholder-style "cmd:*" suffix → ':' is a soft separator and the trailing
	// "*" means "any args", i.e. a prefix match on the (literal) command.
	if prefix, ok := strings.CutSuffix(spec, ":*"); ok {
		pr.re = regexp.MustCompile("^" + regexp.QuoteMeta(prefix))
		return pr, true
	}
	pr.re = globToRegexp(spec)
	return pr, true
}

// globToRegexp converts a glob to an anchored regexp. "*" matches within a path
// segment, "**" crosses segments, and "**/" matches zero or more leading
// directories (so "**/.env" catches both ".env" and "a/b/.env"). "?" matches one
// non-separator char.
func globToRegexp(g string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(g); i++ {
		switch c := g[i]; c {
		case '*':
			if i+1 < len(g) && g[i+1] == '*' {
				if i+2 < len(g) && g[i+2] == '/' {
					b.WriteString("(?:.*/)?") // **/ → zero or more dirs
					i += 2
				} else {
					b.WriteString(".*") // ** → anything, crossing separators
					i++
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return regexp.MustCompile(`^\x00$`) // never matches
	}
	return re
}

// matches reports whether the rule applies to (tool, subject).
func (r policyRule) matches(tool, subject string) bool {
	if r.tool != "*" && r.tool != tool {
		return false
	}
	if r.domain {
		return hostMatches(hostOf(subject), r.hostPat)
	}
	if r.re == nil {
		return false
	}
	// Match the whole subject, or — for path globs without a leading ** — any
	// suffix segment, so "**/.env" catches "a/b/.env" and ".env" alike.
	return r.re.MatchString(subject)
}

// Decide returns the guardrail verdict for a tool call: "deny" (hard-blocked,
// with reason), "ask" (must prompt regardless of base policy), or "" (defer to
// the normal base-permission path). It never auto-allows; allow rules are
// surfaced separately via AllowedByRule.
func (p *Policy) Decide(toolName string, args json.RawMessage) (verdict, reason string) {
	tool := strings.ToLower(toolName)
	subj := subjectOf(tool, args)

	for _, r := range p.deny {
		if r.matches(tool, subj) {
			return "deny", "matches deny rule " + ruleString(tool, r)
		}
	}
	// Egress host allowlist for network tools.
	if len(p.allowDomains) > 0 {
		switch tool {
		case "webfetch":
			if h := hostOf(subj); h != "" && !anyHost(p.allowDomains, h) {
				return "deny", "host " + h + " not in egress allowlist"
			}
		case "bash":
			if bashEgress.MatchString(subj) {
				return "ask", "network egress command (host allowlist enforced)"
			}
		}
	}
	if tool == "bash" {
		if rs := scanBash(subj, p); rs != "" {
			return "ask", rs
		}
	}
	return "", ""
}

// AllowedByRule reports whether an explicit allow rule covers the call, letting
// the loop skip the interactive prompt.
func (p *Policy) AllowedByRule(toolName string, args json.RawMessage) bool {
	tool := strings.ToLower(toolName)
	subj := subjectOf(tool, args)
	for _, r := range p.allow {
		if r.matches(tool, subj) {
			return true
		}
	}
	return false
}

// scanBash inspects a shell command for destructive, injection, egress, or
// secret-touching patterns, returning a human reason (empty = clean).
func scanBash(cmd string, p *Policy) string {
	for _, re := range bashDestructive {
		if re.MatchString(cmd) {
			return "destructive command detected"
		}
	}
	if bashPipeToShell.MatchString(cmd) {
		return "pipe-to-shell (remote code execution) detected"
	}
	if bashEgress.MatchString(cmd) {
		return "network egress command"
	}
	// A bash command that names a secret-protected path.
	for _, r := range p.deny {
		if r.domain || r.re == nil {
			continue
		}
		// Match the rule's path glob against each whitespace token of the command.
		for _, tok := range strings.Fields(cmd) {
			tok = strings.Trim(tok, `"'`)
			if r.re.MatchString(tok) {
				return "command references a protected path (" + r.raw + ")"
			}
		}
	}
	return ""
}

// subjectOf extracts the match subject for a tool call: the command for bash,
// the URL for webfetch, otherwise the path argument.
func subjectOf(tool string, args json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(args, &m)
	get := func(k string) string {
		if v, ok := m[k].(string); ok {
			return v
		}
		return ""
	}
	switch tool {
	case "bash":
		return get("command")
	case "webfetch":
		return get("url")
	default:
		return get("path")
	}
}

// --- host helpers ---

func hostOf(raw string) string {
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func hostMatches(host, pat string) bool {
	if host == "" || pat == "" {
		return false
	}
	return host == pat || strings.HasSuffix(host, "."+pat)
}

func anyHost(pats []string, host string) bool {
	for _, p := range pats {
		if hostMatches(host, p) {
			return true
		}
	}
	return false
}

func normHosts(hs []string) []string {
	out := make([]string, 0, len(hs))
	for _, h := range hs {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			out = append(out, h)
		}
	}
	return out
}

func ruleString(tool string, r policyRule) string {
	if r.domain {
		return tool + "(domain:" + r.hostPat + ")"
	}
	return tool + "(" + r.raw + ")"
}
