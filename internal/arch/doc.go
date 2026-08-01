// Package arch holds guardrails about the SHAPE of this repository rather than the behaviour of
// any part of it: which layer may import which, and how much of a pattern that hides failures the
// tree is allowed to carry.
//
// They are Go tests, not scripts, so they run under the same `go test ./internal/... ./cmd/...`
// everything else does. A guardrail nobody remembers to invoke is a guardrail that reports a
// violation the week after it shipped.
//
// The style is a RATCHET where the tree is not already clean: what exists is recorded and may not
// grow, rather than forbidden outright. Forbidding what is already there fails on the first run
// and gets deleted; recording it makes the next one visible, which is the one that can still be
// argued about. Rebaseline deliberately with MAGI_RATCHET_UPDATE=1.
package arch
