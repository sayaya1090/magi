package lua

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
)

type cancellationAnalyzer struct{ cancelled bool }

func (a *cancellationAnalyzer) Analyze(ctx context.Context, _, _, _ string) (string, error) {
	a.cancelled = errors.Is(ctx.Err(), context.Canceled)
	return "", ctx.Err()
}

func TestToolAnalysisUsesTheCallingTurnsCancellation(t *testing.T) {
	analyzer := &cancellationAnalyzer{}
	r := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: r, Analyzer: analyzer, Logf: func(string) {}})
	dir := writePlugin(t, "name=\"analysis\"\ncapabilities=[\"tool\",\"analyze\"]\n", `
magi.register_tool{name="analysis", schema={type="object"}, execute=function()
  local answer, err = magi.analyze{system="compare", text="evidence"}
  return answer or err, answer == nil
end}`)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	tool, _ := r.Get("analysis")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := tool.Execute(ctx, json.RawMessage(`{}`), port.ToolEnv{})
	if err != nil || !res.IsError || !analyzer.cancelled {
		t.Fatalf("analysis outlived cancelled turn: %s %v", res.Content, err)
	}
}

type partialMeasuredAnalyzer struct{}

func (*partialMeasuredAnalyzer) Analyze(context.Context, string, string, string) (string, error) {
	return "", errors.New("unmeasured path")
}
func (*partialMeasuredAnalyzer) AnalyzeMeasured(context.Context, string, string, string) (string, port.RunMetrics, error) {
	return "partial", port.RunMetrics{Calls: 1, UsageReports: 1, Input: 42, Output: 7, TokensKnown: true}, errors.New("stream interrupted")
}
func TestMeasuredAnalysisPreservesUsageOnFailure(t *testing.T) {
	r := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: r, Analyzer: &partialMeasuredAnalyzer{}, Logf: func(string) {}})
	dir := writePlugin(t, "name=\"measured\"\ncapabilities=[\"tool\",\"analyze\"]\n", `
 magi.register_tool{name="measured",schema={type="object"},execute=function()
  return magi.analyze_measured{system="compare",text="evidence"}
 end}`)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	tool, _ := r.Get("measured")
	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`), port.ToolEnv{})
	if err != nil || result.IsError {
		t.Fatalf("bridge: %s %v", result.Content, err)
	}
	var measured struct {
		Text, Error string
		Metrics     port.RunMetrics
	}
	if err := json.Unmarshal(result.Content, &measured); err != nil {
		t.Fatal(err)
	}
	if measured.Text != "partial" || measured.Error != "stream interrupted" || measured.Metrics.Input != 42 || !measured.Metrics.TokensKnown {
		t.Fatalf("lost measured failure: %s", result.Content)
	}
}
