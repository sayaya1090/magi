package main

import (
	"errors"
	"testing"

	pluginlua "github.com/sayaya1090/magi/internal/adapter/plugin/lua"
	"github.com/sayaya1090/magi/internal/port"
)

func TestAnalyzeUsageLineTellsAMissFromSilence(t *testing.T) {
	cases := []struct {
		name string
		in   pluginlua.AnalyzeUsage
		want string
	}{
		{"hit", pluginlua.AnalyzeUsage{Plugin: "engram", Label: "reflection", Metrics: port.RunMetrics{Input: 100, Output: 7, Cached: 40, CacheReported: true, TokensKnown: true, ElapsedMS: 1500}},
			"magi: analyze usage plugin=engram label=reflection input=100 output=7 cached_tokens=40 elapsed=1.5s status=ok"},
		{"miss", pluginlua.AnalyzeUsage{Plugin: "engram", Label: "dream", Metrics: port.RunMetrics{Input: 9, Output: 1, CacheReported: true, TokensKnown: true}},
			"magi: analyze usage plugin=engram label=dream input=9 output=1 cached_tokens=0 elapsed=0s status=ok"},
		{"silent", pluginlua.AnalyzeUsage{Plugin: "x", Metrics: port.RunMetrics{Calls: 1}, Err: errors.New("boom")},
			"magi: analyze usage plugin=x label=unlabeled input=unreported output=unreported cached_tokens=unreported elapsed=0s status=failed"},
	}
	for _, c := range cases {
		if got := analyzeUsageLine(c.in); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}
