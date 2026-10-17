package main

import (
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// A starting daemon continues the conversation a person was last having here — not a child's, not one
// an Office document opened for itself, not a scheduled run's — and opens a new one only when there
// is none (2026-10-17).
func TestLatestConversationSkipsWhatNobodyWasHaving(t *testing.T) {
	now := time.Now()
	metas := []session.SessionMeta{
		{ID: "old", LastActivity: now.Add(-time.Hour)},
		{ID: "mine", LastActivity: now.Add(-time.Minute)},
		{ID: "child", Parent: "mine", LastActivity: now},
		{ID: "deck", For: "pid-1", LastActivity: now},
		{ID: "cron", Origin: "cron:nightly", LastActivity: now},
	}
	if got := latestConversation(metas); got != "mine" {
		t.Fatalf("continued %q, want the newest conversation a person was having", got)
	}
	if got := latestConversation(metas[2:]); got != "" {
		t.Fatalf("nothing to continue must open a new one, got %q", got)
	}
}
