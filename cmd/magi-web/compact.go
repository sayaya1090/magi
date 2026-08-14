package main

import (
	"context"
	"net/http"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Folding a companion's history, from the console.
//
// The context panel says a companion is at 82% of its window and how many times its history has
// already been summarised away. Until now it offered no lever: a supervisor could see the problem
// and had to open a terminal, attach, and type /compact. The daemon has accepted this command since
// it was written — the TUI calls it — so the gap was only that nothing here did.
//
// # Why a person and not the agent
//
// magi compacts by itself when the window fills past its ratio. This is for the case that rule does
// not cover: a person who can see the run is about to need room and would rather it happened now,
// between turns, than in the middle of the next one.
//
// # What it costs, said where it is offered
//
// A compaction is lossy: the summary keeps the gist, the shards keep the detail on disk, and the
// live window loses the original wording. The page says so beside the button, because "compact" is
// a word that sounds free.
func (s *server) compact(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) || postOnly(w, r) {
		return
	}
	err := s.alone(r, func(cl *daemon.Client, sid session.SessionID) error {
		return cl.Compact(context.Background(), command.Compact{SessionID: sid})
	})
	if err != nil {
		// A daemon built without the control interface answers plainly, and that answer is worth
		// passing through rather than flattening: "this daemon cannot be controlled remotely" is a
		// different thing to know from "the socket is gone".
		http.Error(w, err.Error(), daemonSaysNo(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
