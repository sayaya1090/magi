package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A server that is slow is not a server that is gone. The per-call deadline is this side's
// patience — a large render can outlast it — and taking a live server's tools away because we
// stopped waiting is the same mistake as calling a 500 "dead", but worse: a 500 is the server's
// own signal, a timeout is ours.
func TestASlowServerKeepsItsTools(t *testing.T) {
	reached := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached++
		time.Sleep(300 * time.Millisecond) // longer than the caller is willing to wait
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer srv.Close()

	tr := newHTTPTransport(srv.URL, nil, nil)
	for i := 0; i < unreachableStreak; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		_ = tr.call(ctx, "tools/list", nil, nil)
		cancel()
	}
	if reached != unreachableStreak {
		t.Fatalf("the server was reached %d times, want %d — the test itself is wrong otherwise",
			reached, unreachableStreak)
	}
	select {
	case <-tr.Done():
		t.Error("a live server that answered slowly was dropped")
	default:
	}
}

// Interrupting turns says nothing about the server either — and here it was never even dialled.
func TestCancelledCallsAreNotEvidence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer srv.Close()

	tr := newHTTPTransport(srv.URL, nil, nil)
	for i := 0; i < unreachableStreak; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // the person interrupted the turn
		_ = tr.call(ctx, "tools/list", nil, nil)
	}
	select {
	case <-tr.Done():
		t.Error("three interrupted turns closed a transport that was never dialled")
	default:
	}
}

// And the case this counter exists for still counts: nobody home.
func TestNobodyHomeStillCloses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	url := srv.URL
	srv.Close() // the helper crashed

	tr := newHTTPTransport(url, nil, nil)
	for i := 0; i < unreachableStreak; i++ {
		_ = tr.call(context.Background(), "tools/list", json.RawMessage(nil), nil)
	}
	select {
	case <-tr.Done():
	default:
		t.Error("three calls that reached nobody left the transport open")
	}
}
