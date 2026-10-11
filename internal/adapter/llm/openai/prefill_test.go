package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

func prefixRequest() port.ChatRequest {
	return port.ChatRequest{Model: "test", System: "common", Messages: []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: "evidence A"}}}}}
}
func TestLlamaPrefixDoesNotGenerateOrIncludeAssistantSuffix(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/apply-template":
			var req struct {
				Messages []wireMessage `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			raw := fmt.Sprint(req.Messages[len(req.Messages)-1].Content)
			json.NewEncoder(w).Encode(map[string]string{"prompt": "<system>common</system><user>" + raw + "</user><assistant>"})
		case "/completion":
			var req struct {
				Prompt  string `json:"prompt"`
				Predict int    `json:"n_predict"`
				Cache   bool   `json:"cache_prompt"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if req.Prompt != "<system>common</system><user>evidence A" || req.Predict != 0 || !req.Cache {
				t.Errorf("wrong prefill: %+v", req)
			}
			fmt.Fprint(w, `{"content":"","tokens_predicted":0}`)
		default:
			t.Errorf("generation endpoint called: %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()
	c := New(srv.URL+"/v1", "", WithLlamaPrefixPrefill())
	if err := c.PrefillPrefix(context.Background(), prefixRequest()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal(calls.Load())
	}
	if err := New(srv.URL+"/v1", "").PrefillPrefix(context.Background(), prefixRequest()); !errors.Is(err, port.ErrCapabilityAbsent) {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("unsupported provider made requests")
	}
}
func TestForegroundCancelsNativePrefill(t *testing.T) {
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apply-template":
			io.Copy(io.Discard, r.Body)
			close(entered)
			select {
			case <-r.Context().Done():
				close(cancelled)
			case <-release:
			}
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: [DONE]\n\n")
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	defer close(release)
	c := New(srv.URL+"/v1", "", WithLlamaPrefixPrefill(true))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.PrefillPrefix(ctx, prefixRequest()) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("prefill never started")
	}
	stream, err := c.StreamChat(ctx, prefixRequest())
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("foreground did not cancel prefill")
	}
	if err := <-done; err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("prefill result %v", err)
	}
}

func TestDedicatedPrefillMayFinishDuringForeground(t *testing.T) {
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apply-template":
			var req struct {
				Messages []wireMessage `json:"messages"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			close(entered)
			select {
			case <-r.Context().Done():
				t.Error("dedicated preparation was cancelled")
				return
			case <-release:
			}
			json.NewEncoder(w).Encode(map[string]string{"prompt": "<user>" + fmt.Sprint(req.Messages[len(req.Messages)-1].Content) + "</user>"})
		case "/completion":
			fmt.Fprint(w, `{"content":"","tokens_predicted":0}`)
		case "/v1/chat/completions":
			fmt.Fprint(w, "data: [DONE]\n\n")
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := New(srv.URL+"/v1", "", WithLlamaPrefixPrefill(false))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { done <- c.PrefillPrefix(ctx, prefixRequest()) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("prefill never began")
	}
	stream, err := c.StreamChat(ctx, prefixRequest())
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	for range stream {
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAutoPrefillOnlyCallsRecognizedNativeServer(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprint(native), func(t *testing.T) {
			var probes, completions atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/props":
					probes.Add(1)
					if native {
						fmt.Fprint(w, `{"total_slots":2,"chat_template":"native-template","default_generation_settings":{}}`)
					} else {
						w.WriteHeader(404)
					}
				case "/apply-template":
					var req struct {
						Messages []wireMessage `json:"messages"`
					}
					json.NewDecoder(r.Body).Decode(&req)
					json.NewEncoder(w).Encode(map[string]string{"prompt": "<user>" + fmt.Sprint(req.Messages[len(req.Messages)-1].Content) + "</user>"})
				case "/completion":
					completions.Add(1)
					fmt.Fprint(w, `{"content":"","tokens_predicted":0}`)
				default:
					t.Errorf("unexpected endpoint: %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer srv.Close()
			c := New(srv.URL+"/v1", "", WithAutoPrefixPrefill(false))
			for i := 0; i < 2; i++ {
				err := c.PrefillPrefix(context.Background(), prefixRequest())
				if native && err != nil {
					t.Fatal(err)
				}
				if !native && !errors.Is(err, port.ErrCapabilityAbsent) {
					t.Fatal(err)
				}
			}
			if probes.Load() != 1 {
				t.Fatalf("capability probed %d times", probes.Load())
			}
			if !native && completions.Load() != 0 {
				t.Fatal("unsupported gateway received a generation request")
			}
		})
	}
}
