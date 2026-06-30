package lua

import (
	"context"
	"fmt"
	"sync"

	"github.com/sayaya1090/magi/internal/port"
	"github.com/sayaya1090/magi/internal/prompt"
)

// ToolSink is the subset of a tool registry the host needs to add and remove
// plugin-contributed tools (satisfied by *builtin.Registry).
type ToolSink interface {
	Register(port.Tool)
	Unregister(name string)
}

// MCPManager is the interface for registering MCP servers from plugins.
type MCPManager interface {
	AddHTTP(ctx context.Context, name, url string, headers map[string]string) error
	AddHTTPDynamic(ctx context.Context, name, url string, headersFn func() map[string]string) error
	AddStdio(ctx context.Context, name, command string, args, env []string) error
	Remove(name string)
}

// RuntimeInfo provides runtime context information to plugins.
type RuntimeInfo struct {
	Model    string // Current LLM model name
	Platform string // OS platform (darwin, linux, windows)
	Workdir  string // Current working directory
}

// ContextProviderRegistry allows plugins to register context providers.
type ContextProviderRegistry interface {
	RegisterContextProvider(port.ContextProvider)
}

// LLMHeaderRegistry allows plugins to inject custom headers into LLM backend
// requests (e.g. an in-house gateway's X-CLIENT-API-KEY or an SSO token). fn is
// re-evaluated on every request.
type LLMHeaderRegistry interface {
	AddLLMHeaders(fn func() map[string]string)
}

// BaseURLRegistry lets a plugin redirect the LLM backend base URL at runtime
// (magi.set_base_url) — e.g. to point the agent at a loopback server the plugin runs
// in-process via magi.serve. An empty string clears the override.
type BaseURLRegistry interface {
	SetBaseURL(url string)
	// ClearBaseURLIfEquals clears the override only if it still equals url (compare-and-swap),
	// so unloading one plugin can't wipe an override another plugin has since installed.
	ClearBaseURLIfEquals(url string)
}

// Host loads, reloads, and unloads Lua plugins, registering their tools into a
// shared ToolSink so changes take effect in the running agent (hot reload).
type Host struct {
	sink          ToolSink
	mcpMgr        MCPManager
	contextReg    ContextProviderRegistry
	llmReg        LLMHeaderRegistry
	baseReg       BaseURLRegistry
	runtime       RuntimeInfo
	pluginConfigs map[string]map[string]any // [plugins.<name>] sections from config.toml
	configPath    string                    // path to the user's config.toml (magi.get/set_config_key)
	dataDir       string                    // base dir for per-plugin persistent stores
	prompter      prompt.Prompter           // interactive prompts (magi.ask); nil = unavailable
	logf          func(string)

	mu      sync.Mutex
	plugins map[string]*plugin
}

// HostConfig configures the plugin host.
type HostConfig struct {
	ToolSink      ToolSink
	MCPMgr        MCPManager                // optional: enables magi.register_mcp()
	ContextReg    ContextProviderRegistry   // optional: enables magi.register_context_provider()
	LLMReg        LLMHeaderRegistry         // optional: enables magi.set_llm_headers()
	BaseReg       BaseURLRegistry           // optional: enables magi.set_base_url()
	PluginConfigs map[string]map[string]any // optional: [plugins.<name>] settings, read via magi.store_get
	ConfigPath    string                    // optional: path to config.toml (enables magi.get/set_config_key)
	DataDir       string                    // base dir for per-plugin persistent config stores (store_set)
	Prompter      prompt.Prompter           // optional: enables magi.ask interactive prompts
	Runtime       RuntimeInfo               // runtime context for plugins
	Logf          func(string)              // optional: log output
}

// NewHost returns a plugin host that registers tools into sink. logf may be nil.
func NewHost(sink ToolSink, logf func(string)) *Host {
	if logf == nil {
		logf = func(string) {}
	}
	return &Host{sink: sink, logf: logf, plugins: map[string]*plugin{}}
}

// NewHostWithConfig returns a plugin host with full configuration.
func NewHostWithConfig(cfg HostConfig) *Host {
	if cfg.Logf == nil {
		cfg.Logf = func(string) {}
	}
	return &Host{
		sink:          cfg.ToolSink,
		mcpMgr:        cfg.MCPMgr,
		contextReg:    cfg.ContextReg,
		llmReg:        cfg.LLMReg,
		baseReg:       cfg.BaseReg,
		runtime:       cfg.Runtime,
		pluginConfigs: cfg.PluginConfigs,
		configPath:    cfg.ConfigPath,
		dataDir:       cfg.DataDir,
		prompter:      cfg.Prompter,
		logf:          cfg.Logf,
		plugins:       map[string]*plugin{},
	}
}

// FireEvent runs every loaded plugin's handlers for a lifecycle event,
// synchronously (handlers may block — e.g. a startup auth flow). Call "startup"
// after loading plugins and before the first turn; "shutdown" on exit.
func (h *Host) FireEvent(event string) {
	h.mu.Lock()
	plugins := make([]*plugin, 0, len(h.plugins))
	for _, p := range h.plugins {
		plugins = append(plugins, p)
	}
	h.mu.Unlock()
	for _, p := range plugins {
		p.fire(event)
	}
}

// Load loads the plugin in dir and registers its capabilities.
func (h *Host) Load(ctx context.Context, dir string) (port.PluginInfo, error) {
	p, err := loadPlugin(dir, h.logf, h)
	if err != nil {
		return port.PluginInfo{}, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if old, ok := h.plugins[p.name]; ok {
		h.unregister(old)
		old.close()
	}
	h.plugins[p.name] = p
	for _, t := range p.tools {
		h.sink.Register(t)
	}
	return port.PluginInfo{Name: p.name, Version: p.manifest.Version, Capabilities: p.manifest.Capabilities}, nil
}

// Unload removes a plugin and its capabilities.
func (h *Host) Unload(name string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not loaded", name)
	}
	h.unregister(p)
	p.close()
	delete(h.plugins, name)
	return nil
}

// Reload re-reads a plugin from disk (hot reload). Session state is unaffected
// because it lives in the core, not the plugin.
func (h *Host) Reload(name string) error {
	h.mu.Lock()
	dir := ""
	if p, ok := h.plugins[name]; ok {
		dir = p.dir
	}
	h.mu.Unlock()
	if dir == "" {
		return fmt.Errorf("plugin %q not loaded", name)
	}
	_, err := h.Load(context.Background(), dir)
	return err
}

// Capabilities returns the aggregate capabilities across loaded plugins.
func (h *Host) Capabilities() port.CapabilitySet {
	h.mu.Lock()
	defer h.mu.Unlock()
	var cs port.CapabilitySet
	for _, p := range h.plugins {
		for _, t := range p.tools {
			cs.Tools = append(cs.Tools, t)
		}
	}
	return cs
}

// unregister removes a plugin's tools from the sink (caller holds h.mu).
func (h *Host) unregister(p *plugin) {
	for _, t := range p.tools {
		h.sink.Unregister(t.name)
	}
}
