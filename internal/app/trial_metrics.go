package app

import (
	"context"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/port"
)

type trialMetricsKey struct{}
type trialMetrics struct {
	mu      sync.Mutex
	data    port.RunMetrics
	started time.Time
}

func (m *trialMetrics) start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data.Calls++
}
func (m *trialMetrics) finish(u event.Usage, reported bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if reported {
		m.data.UsageReports++
	}
	m.data.Input += u.In
	m.data.Output += u.Out
	m.data.Cached += u.Cached
	m.data.CacheReported = m.data.CacheReported || u.CacheReported
}
func (m *trialMetrics) snapshot() port.RunMetrics {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.data
	r.TokensKnown = r.Calls > 0 && r.UsageReports == r.Calls
	r.ElapsedMS = time.Since(m.started).Milliseconds()
	return r
}
func trialMeter(ctx context.Context) *trialMetrics {
	m, _ := ctx.Value(trialMetricsKey{}).(*trialMetrics)
	return m
}
