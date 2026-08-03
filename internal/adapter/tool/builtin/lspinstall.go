package builtin

import (
	"fmt"
	"strings"
)

// Install hints for the language servers serverFor knows about. When a server
// binary is absent, the diagnostics path can't just say "install the language
// server" — a weak model won't know which command fits THIS platform, nor that
// the command's own prerequisite (a Node/JDK/Go/Rust/.NET/Ruby toolchain) may
// itself be missing. These tables turn that dead end into an actionable, OS-specific
// instruction the model can run via bash (under the usual policy guardrails);
// magi never auto-installs anything.

// serverHint is one server's install command, with per-OS overrides falling back
// to generic, plus the runtime prerequisite its command needs (empty when the
// command is self-contained or ships its own toolchain).
type serverHint struct {
	prereq                 string // a prereqBootstrap key ("node", "java", "go", "rust", "dotnet", "ruby"), or "" for none
	darwin, linux, windows string // OS-specific command (empty → use generic)
	generic                string // fallback command / release-download note
}

// forOS returns the command for goos, falling back to the generic entry.
func (h serverHint) forOS(goos string) string {
	switch goos {
	case "darwin":
		if h.darwin != "" {
			return h.darwin
		}
	case "linux":
		if h.linux != "" {
			return h.linux
		}
	case "windows":
		if h.windows != "" {
			return h.windows
		}
	}
	return h.generic
}

// lspInstallHints is keyed by the server binary (argv[0] from serverFor), so
// several extensions that share a server share one hint. Prefer the install
// method with the lightest prerequisite: OS package managers / standalone
// binaries where they exist, npm only where it's genuinely the distribution
// channel. A "# …" entry is a release-page pointer for servers with no single
// canonical command.
var lspInstallHints = map[string]serverHint{
	// npm-distributed servers (prereq: node)
	"typescript-language-server":  {prereq: "node", generic: "npm install -g typescript-language-server typescript"},
	"pyright-langserver":          {prereq: "node", generic: "npm install -g pyright\n    # or, with Python: pip install pyright"},
	"intelephense":                {prereq: "node", generic: "npm install -g intelephense"},
	"bash-language-server":        {prereq: "node", generic: "npm install -g bash-language-server"},
	"vue-language-server":         {prereq: "node", generic: "npm install -g @vue/language-server"},
	"svelteserver":                {prereq: "node", generic: "npm install -g svelte-language-server"},
	"vscode-html-language-server": {prereq: "node", generic: "npm install -g vscode-langservers-extracted"},
	"vscode-css-language-server":  {prereq: "node", generic: "npm install -g vscode-langservers-extracted"},
	"vscode-json-language-server": {prereq: "node", generic: "npm install -g vscode-langservers-extracted"},
	"yaml-language-server":        {prereq: "node", generic: "npm install -g yaml-language-server"},

	// Toolchain-bundled / self-contained servers.
	//
	// `prereq` is set only where EVERY platform's command needs that runtime — it is one label per
	// server, so a hint whose darwin command is brew and whose generic is cargo must not claim one:
	// the mac would be told to install a toolchain it does not need. taplo is exactly that shape and
	// deliberately declares nothing; its command names cargo or brew itself.
	"gopls":                           {prereq: "go", generic: "go install golang.org/x/tools/gopls@latest"},
	"rust-analyzer":                   {prereq: "rust", generic: "rustup component add rust-analyzer"},
	"clangd":                          {darwin: "brew install llvm", linux: "sudo apt-get install -y clangd  # Debian/Ubuntu; Fedora: sudo dnf install clang-tools-extra", windows: "choco install llvm"},
	"csharp-ls":                       {prereq: "dotnet", generic: "dotnet tool install --global csharp-ls"},
	"fsautocomplete":                  {prereq: "dotnet", generic: "dotnet tool install --global fsautocomplete"},
	"taplo":                           {generic: "cargo install taplo-cli --features lsp\n    # or: brew install taplo"},
	"haskell-language-server-wrapper": {generic: "ghcup install hls"},
	"ocamllsp":                        {generic: "opam install ocaml-lsp-server"},
	"perl":                            {generic: "cpan Perl::LanguageServer"},
	"zls":                             {darwin: "brew install zls", generic: "# download a release from https://github.com/zigtools/zls/releases"},
	"lua-language-server":             {darwin: "brew install lua-language-server", windows: "scoop install lua-language-server", generic: "# download a release from https://github.com/LuaLS/lua-language-server/releases"},
	"marksman":                        {darwin: "brew install marksman", generic: "# download a release from https://github.com/artempyanykh/marksman/releases"},
	"terraform-ls":                    {darwin: "brew install hashicorp/tap/terraform-ls", generic: "# download from https://releases.hashicorp.com/terraform-ls/"},
	"julia":                           {generic: `julia -e 'using Pkg; Pkg.add("LanguageServer")'`},
	"sourcekit-lsp":                   {darwin: "# ships with Xcode: xcode-select --install", generic: "# ships with the Swift toolchain from https://swift.org/download"},
	"dart":                            {generic: "# ships with the Dart/Flutter SDK: https://dart.dev/get-dart"},
	"elixir-ls":                       {generic: "# download a release from https://github.com/elixir-lsp/elixir-ls/releases"},

	// JVM servers (prereq: java)
	"jdtls":                  {prereq: "java", darwin: "brew install jdtls", generic: "# download from https://download.eclipse.org/jdtls/ (or a distro package)"},
	"kotlin-language-server": {prereq: "java", darwin: "brew install kotlin-language-server", generic: "# download a release from https://github.com/fwcd/kotlin-language-server/releases"},
	"metals":                 {prereq: "java", generic: "cs install metals\n    # (Coursier); or install the Metals editor extension"},
	"groovy-language-server": {prereq: "java", generic: "# build from https://github.com/GroovyLanguageServer/groovy-language-server"},

	// Ruby (prereq: ruby)
	"ruby-lsp": {prereq: "ruby", generic: "gem install ruby-lsp"},
}

// prereqBootstrap installs the runtime that gates a server's own install command.
//
// It used to hold three — node, python, java — on the reasoning that the others "name themselves in
// the server command", i.e. `go install …` tells you it needs go. That is true of the WORD and not
// of the reader: a model that does not have go installed reads `go install golang.org/x/tools/gopls`
// and runs it, and what comes back is `go: command not found`, which is the dead end this file
// exists to prevent. And it left two of prereqBinary's labels — rust and dotnet — with nothing to
// probe for, because no hint declared them: the lookup was there, the producer was not.
//
// The linux commands name their distro rather than pretending to be universal. A wrong-but-specific
// command is worse than a labelled one: the reader can translate `apt-get` to their own manager if
// they know that is what they are looking at, and cannot if it just failed.
var prereqBootstrap = map[string]serverHint{
	"node":   {darwin: "brew install node", linux: "sudo apt-get install -y nodejs npm  # Debian/Ubuntu", windows: "choco install nodejs"},
	"java":   {darwin: "brew install openjdk", linux: "sudo apt-get install -y default-jdk  # Debian/Ubuntu", windows: "choco install temurin"},
	"go":     {darwin: "brew install go", linux: "sudo apt-get install -y golang-go  # Debian/Ubuntu", windows: "choco install golang"},
	"rust":   {windows: "# download rustup-init.exe from https://rustup.rs", generic: "curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh"},
	"dotnet": {darwin: "brew install --cask dotnet-sdk", linux: "sudo apt-get install -y dotnet-sdk-8.0  # Debian/Ubuntu", windows: "choco install dotnet-sdk"},
	"ruby":   {darwin: "brew install ruby", linux: "sudo apt-get install -y ruby-full  # Debian/Ubuntu", windows: "choco install ruby"},
}

// prereqBinary maps a prerequisite label to the PATH binary whose presence
// proves it (so the wiring can LookPath it), or "" when there's nothing to probe.
func prereqBinary(prereq string) string {
	switch prereq {
	case "node":
		return "npm"
	case "java":
		return "java"
	case "go":
		return "go"
	case "rust":
		return "rustup"
	case "ruby":
		return "gem"
	case "dotnet":
		return "dotnet"
	}
	return ""
}

// serverInstall returns the OS-appropriate install command and prerequisite label
// for a server binary, or ("","") when the server is unknown.
func serverInstall(server, goos string) (cmd, prereq string) {
	h, ok := lspInstallHints[server]
	if !ok {
		return "", ""
	}
	return h.forOS(goos), h.prereq
}

// composeInstallAdvice builds the "server not installed" guidance: the server's
// own install command, prefixed with the prerequisite runtime's bootstrap when
// that runtime is missing too (prereqMissing is computed by the caller, which
// owns the PATH lookup). Returns "" for an unknown server. Pure — no I/O — so the
// message is unit-testable without a PATH or a real server.
func composeInstallAdvice(server, goos string, prereqMissing bool) string {
	h, ok := lspInstallHints[server]
	if !ok {
		return ""
	}
	cmd := h.forOS(goos)
	if cmd == "" {
		return ""
	}
	var b strings.Builder
	// Addressed to whoever is reading the diagnostic, not to the model: there is no tool to
	// re-run. The server is what the post-edit diagnostics use, so installing it turns them on.
	fmt.Fprintf(&b, "'%s' language server isn't installed; diagnostics for this file are off until it is:\n", server)
	if h.prereq != "" && prereqMissing {
		if boot, ok := prereqBootstrap[h.prereq]; ok {
			if bc := boot.forOS(goos); bc != "" {
				fmt.Fprintf(&b, "  first install %s:\n    %s\n  then:\n", h.prereq, bc)
			}
		}
	}
	fmt.Fprintf(&b, "    %s", cmd)
	return b.String()
}
