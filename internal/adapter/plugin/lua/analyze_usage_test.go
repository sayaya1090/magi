package lua

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
)

type usageAnalyzer struct {
	reply   string
	err     error
	metrics port.RunMetrics
}

func (a *usageAnalyzer) Analyze(context.Context, string, string, string) (string, error) {
	return "", errors.New("unmeasured path")
}
func (a *usageAnalyzer) AnalyzeMeasured(context.Context, string, string, string) (string, port.RunMetrics, error) {
	return a.reply, a.metrics, a.err
}

func analyzeUsageOf(t *testing.T, analyzer Analyzer, call string) ([]AnalyzeUsage, *syncLog) {
	t.Helper()
	var mu sync.Mutex
	var got []AnalyzeUsage
	log := &syncLog{}
	h := NewHostWithConfig(HostConfig{ToolSink: builtin.NewRegistry(), Analyzer: analyzer, Logf: log.logf,
		OnAnalyze: func(u AnalyzeUsage) { mu.Lock(); defer mu.Unlock(); got = append(got, u) }})
	dir := writePlugin(t, `name="ana"`+"\n"+`capabilities=["analyze"]`, `magi.on("startup", function()
		local raw, err = `+call+`
		magi.log("raw=" .. tostring(raw) .. " err=" .. tostring(err))
	end)`)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	h.FireEvent("startup")
	mu.Lock()
	defer mu.Unlock()
	return append([]AnalyzeUsage(nil), got...), log
}

// The plain call is the one Engram makes, and it used to throw the usage away.
func TestAnalyzeReportsItsUsageWithThePluginAndLabel(t *testing.T) {
	a := &usageAnalyzer{reply: "ok", metrics: port.RunMetrics{Calls: 1, UsageReports: 1, Input: 100, Output: 7, Cached: 40, CacheReported: true, TokensKnown: true}}
	got, _ := analyzeUsageOf(t, a, `magi.analyze{system="s", text="t", label="reflection"}`)
	if len(got) != 1 {
		t.Fatalf("reports = %d, want 1", len(got))
	}
	u := got[0]
	if u.Plugin != "ana" || u.Label != "reflection" || u.Metrics.Input != 100 || u.Metrics.Cached != 40 || !u.Metrics.CacheReported || u.Err != nil {
		t.Fatalf("report = %+v", u)
	}
}

// A failed call spent tokens too, and the plugin still gets its soft failure.
func TestAnalyzeReportsAFailedCallAndStillFailsSoftly(t *testing.T) {
	a := &usageAnalyzer{err: errors.New("boom"), metrics: port.RunMetrics{Calls: 1, Input: 5, TokensKnown: true}}
	got, log := analyzeUsageOf(t, a, `magi.analyze{text="t"}`)
	if len(got) != 1 || got[0].Err == nil || got[0].Metrics.Input != 5 || got[0].Label != "" {
		t.Fatalf("reports = %+v", got)
	}
	if want := "raw=nil err=analyze: boom"; !containsLine(log.String(), want) {
		t.Fatalf("log %q lacks %q", log.String(), want)
	}
}

// An analyzer that cannot measure is reported as unknown, never as zero tokens.
func TestAnalyzeWithoutMeasurementReportsUnknownTokens(t *testing.T) {
	got, _ := analyzeUsageOf(t, &fakeAnalyzer{reply: "ok"}, `magi.analyze{text="t", label="facts"}`)
	if len(got) != 1 || got[0].Metrics.TokensKnown || got[0].Metrics.Calls != 1 || got[0].Label != "facts" {
		t.Fatalf("reports = %+v", got)
	}
}

func containsLine(s, want string) bool {
	for i := 0; i+len(want) <= len(s); i++ {
		if s[i:i+len(want)] == want {
			return true
		}
	}
	return false
}

// The usage line can only split facts, reflection and dream if each call says which it is.
func TestEveryEngramAnalyzeCallNamesItself(t *testing.T) {
	src, err := os.ReadFile("../../../../plugins/engram/init.lua")
	if err != nil {
		t.Fatal(err)
	}
	calls := regexp.MustCompile(`magi\.analyze\s*\{[^}]*\}`).FindAllString(string(src), -1)
	if len(calls) != 4 {
		t.Fatalf("found %d magi.analyze calls, want 4: %q", len(calls), calls)
	}
	for _, c := range calls {
		if !strings.Contains(c, "label = ") {
			t.Errorf("no label: %s", c)
		}
	}
}
