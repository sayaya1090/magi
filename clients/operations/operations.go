package operations

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/clients/appbridge"
)

type Config struct {
	Runtime, Context, Namespace string
	Resources                   []string
}
type Runner func(context.Context, string, ...string) ([]byte, error)
type Adapter struct {
	Config Config
	Run    Runner
}

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func (c Config) Validate() error {
	if c.Runtime != "docker" && c.Runtime != "kubernetes" {
		return errors.New("runtime must be docker or kubernetes")
	}
	if !namePattern.MatchString(c.Context) || (c.Runtime == "kubernetes" && !namePattern.MatchString(c.Namespace)) {
		return errors.New("explicit valid context and namespace are required")
	}
	if len(c.Resources) == 0 {
		return errors.New("resource allowlist is required")
	}
	for _, r := range c.Resources {
		if !namePattern.MatchString(r) {
			return errors.New("invalid resource name")
		}
	}
	return nil
}
func (c Config) Target() string { return c.Runtime + ":" + c.Context + ":" + c.Namespace }
func (a Adapter) Tools() []appbridge.Tool {
	return []appbridge.Tool{
		{Name: "status", Description: "Read the explicitly allowed resource in the fixed environment. No secrets, exec or environment switches.", ReadOnly: true, InputSchema: map[string]any{"type": "object", "properties": map[string]any{"resource": map[string]any{"type": "string"}}, "required": []string{"resource"}, "additionalProperties": false}},
		{Name: "logs", Description: "Read at most 200 recent log lines from the allowed resource in the fixed environment. Output is bounded and untrusted.", ReadOnly: true, InputSchema: map[string]any{"type": "object", "properties": map[string]any{"resource": map[string]any{"type": "string"}}, "required": []string{"resource"}, "additionalProperties": false}},
	}
}

type boundedOutput struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remaining := 65536 - len(b.data)
	if remaining < len(p) {
		b.truncated = true
		p = p[:remaining]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func run(ctx context.Context, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var out boundedOutput
	cmd.Stdout = &out
	// Docker container logs may be emitted on stderr even on successful queries.
	// Error output is never returned when the command fails.
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return nil, errors.New("runtime query failed; check the bound environment and credentials")
	}
	if out.truncated {
		out.data = append(out.data, []byte("\n[output truncated at 64 KiB]")...)
	}
	return out.data, nil
}
func (a Adapter) Call(ctx context.Context, target, name string, args json.RawMessage) (any, error) {
	if err := a.Config.Validate(); err != nil {
		return nil, err
	}
	if target != a.Config.Target() {
		return nil, errors.New("request belongs to another environment")
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(args, &input) != nil || input == nil || len(input) != 1 {
		return nil, errors.New("only resource is accepted")
	}
	var resource string
	if json.Unmarshal(input["resource"], &resource) != nil {
		return nil, errors.New("resource is required")
	}
	allowed := false
	for _, r := range a.Config.Resources {
		if resource == r {
			allowed = true
		}
	}
	if !allowed {
		return nil, errors.New("resource is outside the assigned scope")
	}
	if name != "status" && name != "logs" {
		return nil, errors.New("unsupported operations tool")
	}
	bin := "docker"
	argv := []string{"--context", a.Config.Context}
	if a.Config.Runtime == "docker" {
		if name == "status" {
			argv = append(argv, "inspect", "--format", `{{json .State}}`, resource)
		} else {
			argv = append(argv, "logs", "--tail", "200", "--since", "10m", resource)
		}
	} else {
		bin = "kubectl"
		argv = []string{"--context", a.Config.Context, "--namespace", a.Config.Namespace}
		if name == "status" {
			argv = append(argv, "get", "pod", resource, "-o", `jsonpath={.metadata.name} {.status.phase} {.status.conditions}`)
		} else {
			argv = append(argv, "logs", "pod/"+resource, "--tail=200", "--since=10m", "--limit-bytes=65536")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	execute := a.Run
	if execute == nil {
		execute = run
	}
	out, err := execute(ctx, bin, argv...)
	if err != nil {
		return nil, err
	}
	return map[string]any{"environment": target, "resource": resource, "output": strings.ToValidUTF8(string(out), "�")}, nil
}
