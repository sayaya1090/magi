package lua

import (
	"context"
	"fmt"
	"strings"
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

// ModelRegistry lets a plugin change the current session's active model at
// runtime (magi.set_model) — e.g. an SSO plugin that picks the model to use only
// after it learns which backend the user is entitled to. It mirrors what the TUI
// /route editor does; the implementation is expected to apply the change to the
// live session and persist it. An empty model id is rejected by the bridge.
type ModelRegistry interface {
	SetModel(modelID string) error
}

// UserLabelRegistry lets a plugin set the display name shown for the user in the
// transcript (magi.set_user_label) — e.g. an SSO plugin that learns the
// authenticated username. The implementation applies it to the live session and
// broadcasts the change; an empty label is ignored (the UI keeps "you").
type UserLabelRegistry interface {
	SetUserLabel(label string)
}

// Host loads, reloads, and unloads Lua plugins, registering their tools into a
// shared ToolSink so changes take effect in the running agent (hot reload).
type Host struct {
	sink          ToolSink
	mcpMgr        MCPManager
	contextReg    ContextProviderRegistry
	llmReg        LLMHeaderRegistry
	baseReg       BaseURLRegistry
	modelReg      ModelRegistry
	userReg       UserLabelRegistry
	runtime       RuntimeInfo
	pluginConfigs map[string]map[string]any // [plugins.<name>] sections from config.toml
	configPath    string                    // path to the user's config.toml (magi.get/set_config_key)
	dataDir       string                    // base dir for per-plugin persistent stores
	prompter      prompt.Prompter           // interactive prompts (magi.ask); nil = unavailable
	analyzer      Analyzer                  // sidecar LLM analysis (magi.analyze); nil = unavailable
	logf          func(string)

	mu        sync.Mutex
	plugins   map[string]*plugin
	uiEffects []string // UI effects queued by bridges (e.g. "clear_transcript"), drained by the interactive UI

	// Observation events (user_message / turn_finished) are fired through a
	// bounded queue drained by ONE worker goroutine: the app-side caller
	// (FireEventWith) never blocks on a plugin handler — a handler is free to
	// run a slow magi.analyze sidecar without holding up the turn — and per-
	// plugin handler order is preserved. Overflow drops the event (observation
	// is best-effort, never back-pressure on the conversation).
	evOnce  sync.Once
	evQueue chan firedEvent
}

type firedEvent struct {
	name    string
	payload map[string]string
}

// HostConfig configures the plugin host.
// Analyzer runs a one-shot, tool-free LLM analysis on behalf of a plugin
// (magi.analyze) — a "sidecar" call for observation-style plugins (lesson
// extraction, summarizers) that must never mutate anything. model "" = the
// session's current model. Implementations bound the call with their own
// timeout.
type Analyzer interface {
	Analyze(ctx context.Context, system, text, model string) (string, error)
}

type HostConfig struct {
	ToolSink      ToolSink
	MCPMgr        MCPManager                // optional: enables magi.register_mcp()
	ContextReg    ContextProviderRegistry   // optional: enables magi.register_context_provider()
	LLMReg        LLMHeaderRegistry         // optional: enables magi.set_llm_headers()
	BaseReg       BaseURLRegistry           // optional: enables magi.set_base_url()
	ModelReg      ModelRegistry             // optional: enables magi.set_model()
	UserReg       UserLabelRegistry         // optional: enables magi.set_user_label()
	Analyzer      Analyzer                  // optional: enables magi.analyze (sidecar LLM analysis)
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
		modelReg:      cfg.ModelReg,
		userReg:       cfg.UserReg,
		runtime:       cfg.Runtime,
		pluginConfigs: cfg.PluginConfigs,
		configPath:    cfg.ConfigPath,
		dataDir:       cfg.DataDir,
		prompter:      cfg.Prompter,
		analyzer:      cfg.Analyzer,
		logf:          cfg.Logf,
		plugins:       map[string]*plugin{},
	}
}

// HasEventHandlers reports whether any loaded plugin registered a handler for
// the event — lets the app skip building observation payloads nobody consumes.
// The plugin list is snapshotted and h.mu RELEASED before touching any p.mu:
// bridge paths run Lua under p.mu and then take h.mu (runtimeModel/uiEffects),
// so holding both here would invert the lock order into an ABBA deadlock.
func (h *Host) HasEventHandlers(event string) bool {
	h.mu.Lock()
	plugins := make([]*plugin, 0, len(h.plugins))
	for _, p := range h.plugins {
		plugins = append(plugins, p)
	}
	h.mu.Unlock()
	for _, p := range plugins {
		if p.hasHook(event) {
			return true
		}
	}
	return false
}

// FireEventWith enqueues a payload-carrying observation event (user_message /
// turn_finished) for every loaded plugin. Non-blocking: the caller returns
// immediately and one background worker runs the handlers in order; when the
// queue is full the event is dropped (observation is best-effort).
func (h *Host) FireEventWith(event string, payload map[string]string) {
	h.evOnce.Do(func() {
		h.evQueue = make(chan firedEvent, 128)
		go func() {
			for ev := range h.evQueue {
				h.mu.Lock()
				plugins := make([]*plugin, 0, len(h.plugins))
				for _, p := range h.plugins {
					plugins = append(plugins, p)
				}
				h.mu.Unlock()
				for _, p := range plugins {
					p.fireWith(ev.name, ev.payload)
				}
			}
		}()
	})
	select {
	case h.evQueue <- firedEvent{name: event, payload: payload}:
	default:
		h.logf("plugin events: queue full — dropped " + event)
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
		for _, c := range p.commands {
			cs.Commands = append(cs.Commands, c)
		}
	}
	return cs
}

// PluginCommands returns every slash command contributed by loaded plugins, so
// the TUI can surface them in /help and the palette.
func (h *Host) PluginCommands() []port.PluginCommand {
	h.mu.Lock()
	defer h.mu.Unlock()
	var cmds []port.PluginCommand
	for _, p := range h.plugins {
		for _, c := range p.commands {
			cmds = append(cmds, c)
		}
	}
	return cmds
}

// DoctorProbes returns every environment check contributed by loaded plugins, so
// `magi -doctor` can fold them into its report (mirrors PluginCommands).
func (h *Host) DoctorProbes() []port.DoctorProbe {
	h.mu.Lock()
	defer h.mu.Unlock()
	var probes []port.DoctorProbe
	for _, p := range h.plugins {
		for _, pr := range p.probes {
			probes = append(probes, pr)
		}
	}
	return probes
}

// DispatchCommand runs the plugin command whose name matches (leading slash
// tolerated), returning (true, err) if one handled it and (false, nil) if no
// plugin owns the name so the caller can report "unknown command".
func (h *Host) DispatchCommand(name string, args []string) (bool, error) {
	name = strings.TrimPrefix(name, "/")
	h.mu.Lock()
	var cmd *luaCommand
	for _, p := range h.plugins {
		for _, c := range p.commands {
			if c.name == name {
				cmd = c
				break
			}
		}
	}
	h.mu.Unlock() // release before Execute: it locks the plugin, not the host
	if cmd == nil {
		return false, nil
	}
	return true, cmd.Execute(args)
}

// setRuntimeModel updates the model reported by magi.model() so a set_model /
// reload_config bridge keeps the read side in sync with the running session.
func (h *Host) setRuntimeModel(model string) {
	h.mu.Lock()
	h.runtime.Model = model
	h.mu.Unlock()
}

// runtimeModel returns the model reported to plugins (guarded, since set_model /
// reload_config may update it from a command handler).
func (h *Host) runtimeModel() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.runtime.Model
}

// pushUIEffect queues a UI effect requested by a plugin bridge (e.g. clearing the
// transcript back to the splash). Runs during a command's Lua handler, when the
// caller has released h.mu; the interactive UI drains these via TakeUIEffects.
func (h *Host) pushUIEffect(kind string) {
	h.mu.Lock()
	h.uiEffects = append(h.uiEffects, kind)
	h.mu.Unlock()
}

// TakeUIEffects returns and clears the queued UI effects. The TUI calls this right
// after DispatchCommand so a plugin command (e.g. /logout) can return to the splash.
func (h *Host) TakeUIEffects() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	e := h.uiEffects
	h.uiEffects = nil
	return e
}

// unregister removes a plugin's tools from the sink (caller holds h.mu).
func (h *Host) unregister(p *plugin) {
	for _, t := range p.tools {
		h.sink.Unregister(t.name)
	}
}
