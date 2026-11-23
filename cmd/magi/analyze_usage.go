package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	pluginlua "github.com/sayaya1090/magi/internal/adapter/plugin/lua"
)

// logAnalyzeUsage writes one line per magi.analyze call to the daemon log, beside `council timing`.
// It does not go through pluginLogf, which drops everything without MAGI_DEBUG: a cache rate can only
// be read from a log that was already being kept when the calls happened.
func logAnalyzeUsage(u pluginlua.AnalyzeUsage) {
	fmt.Fprintln(os.Stderr, analyzeUsageLine(u))
}

// analyzeUsageLine says "unreported" where the backend said nothing: zero cached tokens is a miss,
// and a backend that never mentions its cache has not missed.
func analyzeUsageLine(u pluginlua.AnalyzeUsage) string {
	label := u.Label
	if label == "" {
		label = "unlabeled"
	}
	input, output, cached := "unreported", "unreported", "unreported"
	if u.Metrics.TokensKnown {
		input, output = strconv.Itoa(u.Metrics.Input), strconv.Itoa(u.Metrics.Output)
	}
	if u.Metrics.CacheReported {
		cached = strconv.Itoa(u.Metrics.Cached)
	}
	status := "ok"
	if u.Err != nil {
		status = "failed"
	}
	elapsed := (time.Duration(u.Metrics.ElapsedMS) * time.Millisecond).Round(time.Millisecond)
	line := fmt.Sprintf("magi: analyze usage plugin=%s label=%s input=%s output=%s cached_tokens=%s elapsed=%s status=%s",
		u.Plugin, label, input, output, cached, elapsed, status)
	if u.Err != nil {
		// WHY it failed, on the same line. The plugin's own message about the failure goes through
		// pluginLogf, which keeps nothing without MAGI_DEBUG, so "failed" was all the log ever said
		// (Engram reflection failed 5 times in a row on 2026-11-23 with no reason on disk).
		line += " reason=" + strconv.Quote(analyzeReason(u.Err.Error()))
	}
	return line
}

// analyzeReason folds an error onto one line and bounds it: a backend may answer a page of HTML.
func analyzeReason(s string) string {
	const max = 200
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}
