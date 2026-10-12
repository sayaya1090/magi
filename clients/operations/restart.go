package operations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/sayaya1090/magi/clients/appbridge"
)

type RestartProposal struct {
	ID          string    `json:"id"`
	Environment string    `json:"environment"`
	Resource    string    `json:"resource"`
	Impact      string    `json:"impact"`
	Recovery    string    `json:"recovery"`
	Expires     time.Time `json:"expires"`
}
type RestartService struct {
	Adapter     Adapter
	Enabled     bool
	Deployments []string
	mu          sync.Mutex
	executeMu   sync.Mutex
	pending     map[string]RestartProposal
}

func (s *RestartService) Validate() error {
	if err := s.Adapter.Config.Validate(); err != nil {
		return err
	}
	if s.Enabled && s.Adapter.Config.Runtime == "kubernetes" {
		if len(s.Deployments) == 0 {
			return errors.New("deployment restart allowlist is required")
		}
		for _, name := range s.Deployments {
			if !namePattern.MatchString(name) {
				return errors.New("invalid deployment restart allowlist")
			}
		}
	}
	return nil
}

func (s *RestartService) Tools() []appbridge.Tool {
	t := s.Adapter.Tools()
	if s.Enabled {
		t = append(t, appbridge.Tool{Name: "propose_restart", Description: "Propose a restart in the fixed environment for explicit human confirmation in the operations UI. Does not execute the restart. Inspect current state first; user approval is mandatory. Resource is an allowed Docker container or separately allowed Kubernetes Deployment.", ReadOnly: true, InputSchema: map[string]any{"type": "object", "properties": map[string]any{"resource": map[string]any{"type": "string"}}, "required": []string{"resource"}, "additionalProperties": false}})
	}
	return t
}
func (s *RestartService) pruneLocked() {
	for id, p := range s.pending {
		if time.Now().After(p.Expires) {
			delete(s.pending, id)
		}
	}
}
func (s *RestartService) Call(ctx context.Context, target, name string, args json.RawMessage) (any, error) {
	if name != "propose_restart" {
		return s.Adapter.Call(ctx, target, name, args)
	}
	if !s.Enabled || target != s.Adapter.Config.Target() {
		return nil, errors.New("restart proposals are disabled or target does not match")
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(args, &input) != nil || len(input) != 1 {
		return nil, errors.New("only resource is accepted")
	}
	var resource string
	if json.Unmarshal(input["resource"], &resource) != nil || !namePattern.MatchString(resource) {
		return nil, errors.New("invalid resource")
	}
	allowed := s.Adapter.Config.Resources
	if s.Adapter.Config.Runtime == "kubernetes" {
		allowed = s.Deployments
	}
	match := false
	for _, r := range allowed {
		if r == resource {
			match = true
		}
	}
	if !match {
		return nil, errors.New("resource is outside restart ownership")
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	p := RestartProposal{ID: hex.EncodeToString(b), Environment: target, Resource: resource, Impact: "The named workload will restart; requests or in-memory work may be interrupted.", Recovery: "Restart does not restore previous in-memory state. Inspect logs and health; do not retry blindly on an uncertain result.", Expires: time.Now().Add(5 * time.Minute)}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	if len(s.pending) >= 20 {
		return nil, errors.New("too many pending restart proposals")
	}
	if s.pending == nil {
		s.pending = map[string]RestartProposal{}
	}
	s.pending[p.ID] = p
	return map[string]any{"executed": false, "approvalRequired": true, "proposal": p}, nil
}
func (s *RestartService) Decide(ctx context.Context, id string, allow bool) (any, error) {
	s.mu.Lock()
	s.pruneLocked()
	p, ok := s.pending[id]
	delete(s.pending, id)
	s.mu.Unlock()
	if !ok {
		return nil, errors.New("restart proposal expired or was already handled")
	}
	if !allow {
		return map[string]any{"executed": false, "denied": true}, nil
	}
	s.executeMu.Lock()
	defer s.executeMu.Unlock()
	if time.Now().After(p.Expires) {
		return nil, errors.New("restart approval expired before execution")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	run := s.Adapter.Run
	if run == nil {
		run = defaultRestartRunner
	}
	c := s.Adapter.Config
	bin := "docker"
	prefix := []string{"--context", c.Context}
	action := []string{"restart", "--time", "10", p.Resource}
	check := []string{"inspect", "--format", `{{json .State}}`, p.Resource}
	if c.Runtime == "kubernetes" {
		bin = "kubectl"
		prefix = []string{"--context", c.Context, "--namespace", c.Namespace}
		action = []string{"rollout", "restart", "deployment/" + p.Resource}
		check = []string{"rollout", "status", "deployment/" + p.Resource, "--timeout=60s"}
	}
	if _, err := run(ctx, bin, append(append([]string{}, prefix...), action...)...); err != nil {
		return map[string]any{"executed": "unknown", "verified": false, "error": "Restart failed or completion is uncertain; inspect workload state before retrying."}, nil
	}
	out, err := run(ctx, bin, append(append([]string{}, prefix...), check...)...)
	if err != nil {
		return map[string]any{"executed": true, "verified": false, "error": "Restart command completed but post-change inspection failed."}, nil
	}
	verified := true
	if c.Runtime == "docker" {
		var state struct {
			Running    bool
			Restarting bool
			Health     *struct{ Status string }
		}
		verified = json.Unmarshal(out, &state) == nil && state.Running && !state.Restarting && (state.Health == nil || state.Health.Status == "healthy")
	}
	return map[string]any{"executed": true, "verified": verified, "environment": p.Environment, "resource": p.Resource, "status": string(out)}, nil
}
func defaultRestartRunner(ctx context.Context, bin string, args ...string) ([]byte, error) {
	return run(ctx, bin, args...)
}
func (s *RestartService) PendingHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "GET required", 405)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	rows := []RestartProposal{}
	for _, p := range s.pending {
		rows = append(rows, p)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rows)
}
func (s *RestartService) DecideHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var p struct {
		ID       string `json:"id"`
		Decision string `json:"decision"`
	}
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&p) != nil || d.Decode(&extra) != io.EOF || (p.Decision != "allow" && p.Decision != "deny") {
		http.Error(w, "invalid restart decision", 400)
		return
	}
	out, err := s.Decide(r.Context(), p.ID, p.Decision == "allow")
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}
