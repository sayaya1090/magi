package openai

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sayaya1090/magi/internal/port"
)

// WithLlamaPrefixPrefill enables llama-server's native, zero-generation path.
// Explicit native mode skips detection; automatic mode probes only read-only server properties.
func WithLlamaPrefixPrefill(preempt ...bool) Option {
	return func(c *Client) {
		c.llamaPrefill = true
		c.prefixPreempt = len(preempt) > 0 && preempt[0]
	}
}

// WithAutoPrefixPrefill detects native llama-server APIs using read-only properties.
// Unknown gateways are never sent a completion request for preparation.
func WithAutoPrefixPrefill(preempt bool) Option {
	return func(c *Client) { c.llamaPrefill = true; c.prefixAuto = true; c.prefixPreempt = preempt }
}

func (c *Client) PrefillPrefix(ctx context.Context, r port.ChatRequest) error {
	if !c.llamaPrefill {
		return port.ErrCapabilityAbsent
	}
	c.prefixMu.Lock()
	if c.foreground > 0 || c.prefixRunning {
		c.prefixMu.Unlock()
		return port.ErrPrefillBusy
	}
	ctx, cancel := context.WithCancel(ctx)
	c.prefixCancel = cancel
	c.prefixRunning = true
	c.prefixMu.Unlock()
	defer func() {
		cancel()
		c.prefixMu.Lock()
		c.prefixCancel = nil
		c.prefixRunning = false
		c.prefixMu.Unlock()
	}()
	if c.prefixAuto {
		if err := c.detectPrefixAPI(ctx); err != nil {
			return err
		}
	}
	if len(r.Messages) != 1 || len(r.Tools) != 0 {
		return fmt.Errorf("prefill requires one evidence message and no tools")
	}
	// Render through the server's own chat template. Stop inside the evidence user
	// message, before its end-of-turn/assistant suffix: judging extends this message.
	br := buildRequest(r, false, false, c.reasoningEffort, 0, c.sampling, c.seesImages)
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	marker := "MAGI_PREFIX_END_" + hex.EncodeToString(nonce[:])
	if len(br.Messages) == 0 {
		return fmt.Errorf("empty prefill messages")
	}
	last := len(br.Messages) - 1
	body, ok := br.Messages[last].Content.(string)
	if !ok {
		return fmt.Errorf("prefill requires plain text")
	}
	br.Messages[last].Content = body + marker
	var template struct {
		Prompt string `json:"prompt"`
	}
	if err := c.prefillPost(ctx, "/apply-template", map[string]any{"model": r.Model, "messages": br.Messages, "add_generation_prompt": false}, &template); err != nil {
		return err
	}
	cut := strings.LastIndex(template.Prompt, marker)
	if cut < 0 {
		return fmt.Errorf("server template did not preserve the evidence prefix")
	}
	var result struct {
		Content   *string `json:"content"`
		Predicted *int    `json:"tokens_predicted"`
	}
	if err := c.prefillPost(ctx, "/completion", map[string]any{"model": r.Model, "prompt": template.Prompt[:cut], "n_predict": 0, "cache_prompt": true, "stream": false}, &result); err != nil {
		return err
	}
	if result.Content == nil || result.Predicted == nil || *result.Content != "" || *result.Predicted != 0 {
		return fmt.Errorf("backend generated output during zero-generation prefill")
	}
	return nil
}

func (c *Client) prefillPost(ctx context.Context, path string, in, out any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	base := strings.TrimSuffix(strings.TrimRight(c.base(), "/"), "/v1")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	c.applyExtraHeaders(req)
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("prefix prefill %s returned status %d", path, res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(out)
}

func (c *Client) detectPrefixAPI(ctx context.Context) error {
	base := c.base()
	c.prefixMu.Lock()
	known := c.prefixDetectedBase == base
	capable := c.prefixDetected
	c.prefixMu.Unlock()
	if known {
		if capable {
			return nil
		}
		return port.ErrCapabilityAbsent
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(strings.TrimRight(base, "/"), "/v1")+"/props", nil)
	if err != nil {
		return err
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	c.applyExtraHeaders(req)
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var props struct {
		Slots    int            `json:"total_slots"`
		Template string         `json:"chat_template"`
		Settings map[string]any `json:"default_generation_settings"`
	}
	capable = res.StatusCode == http.StatusOK && json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&props) == nil && props.Slots > 0 && props.Template != "" && props.Settings != nil
	c.prefixMu.Lock()
	c.prefixDetectedBase = base
	c.prefixDetected = capable
	c.prefixMu.Unlock()
	if !capable {
		return port.ErrCapabilityAbsent
	}
	return nil
}
