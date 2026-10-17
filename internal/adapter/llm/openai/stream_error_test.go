package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestStreamErrorEnvelopeIsNotASuccessfulEmptyAnswer(t *testing.T) {
	for _, prefix := range []string{"", "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n", "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"} {
		t.Run(fmt.Sprintf("prefix-%d", len(prefix)), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, prefix+"data: {\"error\":{\"message\":\"native tool isolation failed\"}}\n\ndata: [DONE]\n\n")
			}))
			defer srv.Close()
			stream, err := New(srv.URL, "test").StreamChat(context.Background(), port.ChatRequest{Model: "fixture"})
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for e := range stream {
				if e.Type == port.ProviderError {
					count++
					if !strings.Contains(e.Err.Error(), "native tool isolation failed") {
						t.Fatalf("lost backend cause: %v", e.Err)
					}
				}
			}
			if count != 1 {
				t.Fatalf("got %d errors; expected one explicit backend failure", count)
			}
		})
	}
}
