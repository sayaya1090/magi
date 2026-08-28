package app

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

// Policy is the guardrail decision engine that sits above interactive permission
// prompting. It composes three concerns: a sandbox × approval axis, pattern
// rules, and bash command analysis:
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
	// secret and guardrail are the built-in floors, held as patterns rather than as rules bound to
	// tool names. They used to be expanded into one rule per name × glob for "read", "write",
	// "edit", "multiedit" — exact while those were the only file tools, and silently empty for a
	// tool that edits the same workspace under a name nobody listed. They are now matched against
	// any call that says it touches a file (port.FileTool), which is decided per call by touches.
	secret       []*regexp.Regexp
	secretRaw    []string
	guardrail    []*regexp.Regexp
	guardrailRaw []string
	// touches answers whether a call opens a file and whether it writes it. nil = the builtin
	// names, which is what a Policy built without a registry behind it has always meant.
	touches func(tool string, args json.RawMessage) (fileTouch, bool)
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

// guardrailGlobs are the files that decide what the agent may do. Denied for WRITING, allowed for
// reading — knowing your own posture is useful and harmless; rewriting it is the whole problem.
//
// The project config is inside the workspace, which is inside the tool jail, so `write` reached it.
// In a trusted workspace that file is taken as written, so an agent could grant itself hooks, tool
// servers, an approval list — and in `auto` mode an edit is approved without anybody seeing it. A
// plugin's manifest is the same shape one level down: it declares the permissions the host then
// grants it.
//
// This is the file-tool half. A bash command touching them is caught by the same scan that catches
// a secret path, and magi's own writers (the persisters, the console's editors) are not tools and
// are unaffected — which is why the settings a person changes still change.
var guardrailGlobs = []string{
	"**/.magi/config.toml", "**/.magi/plugins/**",
}

// bashDestructive matches commands whose blast radius is large and irreversible.
var bashDestructive = []*regexp.Regexp{
	regexp.MustCompile(`\brm\s+(-[a-zA-Z]*\s+)*-[a-zA-Z]*[rf][a-zA-Z]*\b`), // rm -rf / -fr / -r / -f (short flags)
	regexp.MustCompile(`\brm\s+[^;&|]*--(recursive|force)\b`),              // rm --recursive / --force (GNU long form)
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
	// The two built-in floors. Compiled with a case fold (compileGlob(g, true)) — see compileGlob —
	// and held as patterns, not as rules bound to a list of tool names: which calls they apply to
	// is a question about the CALL (does it open a file?) and is asked per call in Decide.
	for _, g := range secretGlobs {
		p.secret = append(p.secret, compileGlob(g, true))
		p.secretRaw = append(p.secretRaw, g)
	}
	for _, g := range guardrailGlobs {
		p.guardrail = append(p.guardrail, compileGlob(g, true))
		p.guardrailRaw = append(p.guardrailRaw, g)
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
	// A "cmd:*" suffix → ':' is a soft separator and the trailing
	// "*" means "any args", i.e. a prefix match on the (literal) command.
	//
	// Anchored to a command boundary, not a bare prefix. `^git` (no boundary) matched every
	// program whose name merely BEGINS with the prefix — bash(git:*) covered github, gitleaks,
	// git-crypt; bash(ls:*) covered lsof, lsblk — silently granting programs the operator never
	// approved. `^git(\s|$)` matches "git" alone and "git status …" but not "github". (The
	// dangerous-tail case, "git status && rm -rf", is caught by the scanner-forced prompt in
	// gatePermission, which an allow rule no longer waives — this anchor is the sibling-name half.)
	if prefix, ok := strings.CutSuffix(spec, ":*"); ok {
		pr.re = regexp.MustCompile("^" + regexp.QuoteMeta(prefix) + `(\s|$)`)
		return pr, true
	}
	pr.re = globToRegexp(spec)
	return pr, true
}

// globToRegexp converts a glob to an anchored regexp. "*" matches within a path
// segment, "**" crosses segments, and "**/" matches zero or more leading
// directories (so "**/.env" catches both ".env" and "a/b/.env"). "?" matches one
// non-separator char.
func globToRegexp(g string) *regexp.Regexp { return compileGlob(g, false) }

// compileGlob is globToRegexp with an optional case fold. The secret and guardrail deny rules are
// folded (fold=true): the read tool resolves a path case-insensitively — findByBase lowercases both
// sides and walks the tree, so `read {"path":"ID_RSA"}` misses a case-sensitive `**/id_rsa` deny and
// then finds the real key — and on a case-insensitive filesystem (macOS) os.Stat("ID_RSA") opens it
// directly. A case-sensitive floor over a case-insensitive resolver is not a floor. Allow rules and
// user-written rules are NOT folded: there, case-insensitivity would GRANT siblings (a rule for
// build.sh would also cover BUILD.SH), which is the opposite of safe.
func compileGlob(g string, fold bool) *regexp.Regexp {
	var b strings.Builder
	if fold {
		b.WriteString("(?i)")
	}
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
	// Full-match the subject against the anchored glob regexp. A rule reaches
	// nested paths only through an explicit "**/" prefix (compiled to an optional
	// leading-directory group), so "**/.env" catches both ".env" and "a/b/.env",
	// while a bare ".env" glob matches only the exact subject ".env".
	return r.re.MatchString(subject)
}

// Decide returns the guardrail verdict for a tool call: "deny" (hard-blocked,
// with reason), "ask" (must prompt regardless of base policy), or "" (defer to
// the normal base-permission path). It never auto-allows; allow rules are
// surfaced separately via AllowedByRule.
func (p *Policy) Decide(toolName string, args json.RawMessage) (verdict, reason string) {
	tool := strings.ToLower(toolName)
	subj := subjectOf(tool, args)

	// The built-in floors, applied to whatever this call says it opens. A secret is refused to
	// readers and writers alike; the guardrail paths only to writers — .magi/config.toml is a file
	// the agent may read and may not rewrite.
	if touch, ok := p.fileTouch(toolName, args); ok && touch.path != "" {
		for i, re := range p.secret {
			if re.MatchString(touch.path) {
				return "deny", "matches deny rule " + toolName + "(" + p.secretRaw[i] + ")"
			}
		}
		if touch.writes {
			for i, re := range p.guardrail {
				if re.MatchString(touch.path) {
					return "deny", "matches deny rule " + toolName + "(" + p.guardrailRaw[i] + ")"
				}
			}
		}
	}

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
		case "bash", "wait_for":
			// wait_for executes its condition through the shell, so its subject is
			// scanned for egress exactly like a bash command.
			//
			// Two strengths, honestly split. A LITERAL URL naming an off-list host is a hard
			// deny — the same verdict webfetch gets, because the host is right there to check.
			// Everything else the egress scan catches only FORCES A PROMPT: a shell command can
			// reach the network through a variable, a config file, or a bare hostname this scan
			// cannot read, so "the allowlist is enforced" would be a claim string-scanning cannot
			// keep — and under a headless `allow` run a forced prompt resolves to allow, which is
			// that posture's meaning. The deny is the half that holds everywhere.
			if h := offListURLHost(subj, p.allowDomains); h != "" {
				return "deny", "host " + h + " not in egress allowlist"
			}
			if bashEgress.MatchString(subj) {
				return "ask", "network egress command (host allowlist checked where a URL is literal)"
			}
		}
	}
	if tool == "bash" || tool == "wait_for" {
		if rs := scanBash(subj, p); rs != "" {
			return "ask", rs
		}
	}
	return "", ""
}

// fileTouch asks what this call does to a file, through whatever the policy was given. A Policy
// with no answer behind it falls back to the builtin names, which is what it did before a tool
// could say so itself.
func (p *Policy) fileTouch(tool string, args json.RawMessage) (fileTouch, bool) {
	if p.touches != nil {
		return p.touches(tool, args)
	}
	return touchesFileIn(nil, tool, args)
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
	// A bash command that names a protected path: the built-in floors, and any path rule the
	// operator wrote. A shell command is not a file tool — nothing declares what it opens — so this
	// is the one place that still has to guess, by matching each token of the command.
	for _, r := range p.deny {
		if r.domain || r.re == nil {
			continue
		}
		if tok := firstTokenMatching(cmd, r.re); tok != "" {
			return "command references a protected path (" + r.raw + ")"
		}
	}
	for i, re := range p.secret {
		if tok := firstTokenMatching(cmd, re); tok != "" {
			return "command references a protected path (" + p.secretRaw[i] + ")"
		}
	}
	for i, re := range p.guardrail {
		if tok := firstTokenMatching(cmd, re); tok != "" {
			return "command references a protected path (" + p.guardrailRaw[i] + ")"
		}
	}
	return ""
}

// firstTokenMatching returns the first whitespace token of a command that the pattern matches,
// quotes stripped, or "" when none does.
func firstTokenMatching(cmd string, re *regexp.Regexp) string {
	for _, tok := range strings.Fields(cmd) {
		tok = strings.Trim(tok, `"'`)
		if re.MatchString(tok) {
			return tok
		}
	}
	return ""
}

// subjectOf extracts the match subject for a tool call: the command for bash, the
// condition for wait_for, the URL for webfetch, otherwise the path argument.
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
	case "wait_for":
		return get("condition")
	case "webfetch":
		return get("url")
	default:
		return get("path")
	}
}

// offListURLHost returns the first host a command names in a LITERAL URL that is not on the
// allowlist, or "" when every literal URL is covered (or none is present).
//
// Literal URLs only — a token carrying "://" — because that is the case the string can actually
// answer. A bare hostname argument (`curl example.com`), a variable, or a host read from a file is
// out of reach here by construction; those fall to the forced prompt beside this. Quotes are
// stripped the way the secret-path scan strips them, so `curl "https://x"` reads the same as the
// unquoted form.
func offListURLHost(cmd string, allow []string) string {
	for _, tok := range strings.Fields(cmd) {
		tok = strings.Trim(tok, `"'`)
		if !strings.Contains(tok, "://") {
			continue
		}
		if h := hostOf(tok); h != "" && !anyHost(allow, h) {
			return h
		}
	}
	return ""
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
