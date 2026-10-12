// Package headless provides an isolated Playwright Chromium browser tool backend.
package headless

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
)

type Config struct {
	Channel        string        `json:"channel,omitempty"`
	AllowedOrigins []string      `json:"allowedOrigins"`
	InitialURL     string        `json:"initialURL"`
	AllowActions   bool          `json:"allowActions"`
	NodePath       string        `json:"-"`
	RunnerPath     string        `json:"-"`
	Timeout        time.Duration `json:"-"`
}
type response struct {
	Ready  bool            `json:"ready"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}
type Backend struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	input    io.WriteCloser
	output   *bufio.Scanner
	timeout  time.Duration
	closed   bool
	reaped   bool
	config   Config
	children map[string]*Backend
}

func New(ctx context.Context, c Config) (*Backend, error) {
	if c.Channel != "" && c.Channel != "chrome" {
		return nil, errors.New("headless channel must be chrome or empty")
	}
	if len(c.AllowedOrigins) == 0 {
		return nil, errors.New("explicit allowed HTTP(S) origins are required")
	}
	for _, origin := range c.AllowedOrigins {
		u, e := url.Parse(origin)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("invalid allowed origin %q", origin)
		}
	}
	if c.NodePath == "" {
		c.NodePath = "node"
	}
	if c.RunnerPath == "" {
		_, file, _, _ := runtime.Caller(0)
		c.RunnerPath = filepath.Join(filepath.Dir(file), "runner.mjs")
	}
	if c.Timeout <= 0 {
		c.Timeout = 20 * time.Second
	}
	payload, _ := json.Marshal(c)
	cmd := exec.Command(c.NodePath, c.RunnerPath, string(payload))
	in, e := cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	b := &Backend{cmd: cmd, input: in, output: bufio.NewScanner(out), timeout: c.Timeout, config: c, children: map[string]*Backend{}}
	b.output.Buffer(make([]byte, 4096), 256*1024)
	if e = cmd.Start(); e != nil {
		return nil, e
	}
	r, e := b.receive(ctx)
	if e != nil || !r.Ready {
		b.Close()
		if e == nil {
			e = errors.New(r.Error)
		}
		return nil, e
	}
	return b, nil
}
func (b *Backend) receive(ctx context.Context) (response, error) {
	ch := make(chan struct {
		r response
		e error
	}, 1)
	go func() {
		var r response
		var e error
		if !b.output.Scan() {
			e = b.output.Err()
			if e == nil {
				e = io.EOF
			}
		} else {
			e = json.Unmarshal(b.output.Bytes(), &r)
		}
		ch <- struct {
			r response
			e error
		}{r, e}
	}()
	timer := time.NewTimer(b.timeout)
	defer timer.Stop()
	select {
	case out := <-ch:
		return out.r, out.e
	case <-ctx.Done():
		if err := b.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			log.Printf("headless termination: %v", err)
		}
		b.closed = true
		return response{}, ctx.Err()
	case <-timer.C:
		if err := b.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			log.Printf("headless termination: %v", err)
		}
		b.closed = true
		return response{}, errors.New("headless call timed out; outcome uncertain, no retry")
	}
}
func (b *Backend) Call(ctx context.Context, target, name string, args json.RawMessage) (any, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.closed {
		return nil, errors.New("headless backend closed")
	}
	if target != "" {
		child := b.children[target]
		if child == nil {
			if len(b.children) >= 20 {
				return nil, errors.New("headless lease capacity exceeded")
			}
			var err error
			child, err = New(ctx, b.config)
			if err != nil {
				return nil, err
			}
			b.children[target] = child
		}
		return child.Call(ctx, "", name, args)
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	payload, e := json.Marshal(map[string]any{"name": name, "args": args})
	if e != nil {
		return nil, e
	}
	if _, e = b.input.Write(append(payload, '\n')); e != nil {
		return nil, e
	}
	r, e := b.receive(ctx)
	if e != nil {
		return nil, e
	}
	if r.Error != "" {
		return nil, errors.New(r.Error)
	}
	var result any
	e = json.Unmarshal(r.Result, &result)
	return result, e
}
func (b *Backend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.reaped {
		return nil
	}
	b.reaped = true
	var cleanupErr error
	for _, child := range b.children {
		cleanupErr = errors.Join(cleanupErr, child.Close())
	}
	b.children = nil
	cleanupErr = errors.Join(cleanupErr, b.input.Close())
	done := make(chan error, 1)
	go func() { done <- b.cmd.Wait() }()
	select {
	case e := <-done:
		b.closed = true
		return errors.Join(cleanupErr, e)
	case <-time.After(5 * time.Second):
		if err := b.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			cleanupErr = errors.Join(cleanupErr, err)
		}
		b.closed = true
		return errors.Join(cleanupErr, <-done)
	}
}

// Release closes the isolated browser assigned to one lease target.
func (b *Backend) Release(target string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	child := b.children[target]
	if child == nil {
		return nil
	}
	delete(b.children, target)
	return child.Close()
}
