package eval

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/llm/openai"
	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// reviewCorpus is a deliberately COMPLEX target: six files spanning four review
// lenses (security / concurrency / correctness / resources) with ten planted,
// mutually-distinct defects. The size and spread are the point — a single pass
// must hold all six files at once (attention dilutes), while fan-out can give
// each file/concern a focused subagent. That asymmetry is the hypothesis under
// test: does decomposition recover defects a lone agent drops on complex work?
var reviewCorpus = map[string]string{
	"auth.go": `package svc

import (
	"crypto/md5"
	"database/sql"
	"encoding/hex"
)

// Authenticate looks up a user and checks their password.
func Authenticate(db *sql.DB, name, password string) bool {
	row := db.QueryRow("SELECT pass FROM users WHERE name = '" + name + "'")
	var stored string
	row.Scan(&stored)
	return hashPassword(password) == stored
}

func hashPassword(p string) string {
	sum := md5.Sum([]byte(p))
	return hex.EncodeToString(sum[:])
}
`,
	"cache.go": `package svc

// Cache is a process-wide string cache shared across request goroutines.
type Cache struct {
	m map[string]string
}

func NewCache() *Cache { return &Cache{m: map[string]string{}} }

// Set stores a value. Called concurrently from many request handlers.
func (c *Cache) Set(k, v string) {
	c.m[k] = v
}

func (c *Cache) Get(k string) string {
	return c.m[k]
}
`,
	"handler.go": `package svc

import (
	"io"
	"net/http"
)

type Config struct{ Limit int }

func Handle(w http.ResponseWriter, r *http.Request, cfg *Config) {
	body, _ := io.ReadAll(r.Body)
	if len(body) > cfg.Limit {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	w.Write(body)
}
`,
	"calc.go": `package svc

// Average returns the mean of xs.
func Average(xs []int) int {
	sum := 0
	for i := 0; i < len(xs); i++ {
		sum += xs[i]
	}
	return sum / len(xs)
}
`,
	"file.go": `package svc

import "os"

// AppendLine appends a line to a file.
func AppendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(line + "\n")
	return err
}
`,
	"worker.go": `package svc

import "time"

// FetchWithRetry retries fetch until it succeeds.
func FetchWithRetry(fetch func() error) {
	for {
		if err := fetch(); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
`,
}

// plantedIssues maps each defect to keywords indicating a review caught it. Kept
// specific so one generic phrase doesn't credit several distinct issues.
var plantedIssues = map[string][]string{
	"sql-injection":   {"sql injection", "injection", "sanitiz", "parameteri", "prepared"},
	"weak-hash":       {"md5", "weak hash", "insecure hash", "bcrypt", "sha-256", "sha256", "weak hashing", "cryptographically", "stronger hash"},
	"data-race":       {"data race", "race condition", "concurrent map", "mutex", "sync.", "thread-safe", "thread safe", "not safe for concurrent", "without a lock", "synchroniz"},
	"cache-unbounded": {"unbounded", "eviction", "evict", "grows without", "no limit", "no maximum size", "memory growth", "no ttl", "cache size", "grow indefinitely"},
	"ignored-error":   {"ignored error", "error is ignored", "unchecked error", "swallow", "not checked", "ignoring the error", "ignores the returned", "scan error", "discards the error"},
	"nil-deref":       {"nil pointer", "nil deref", "dereference", "cfg is nil", "not validated", "nil check", "if cfg ==", "nil config"},
	"off-by-one":      {"off-by-one", "off by one", "out of range", "out-of-bounds", "bounds", "<= len", "index out"},
	"div-zero":        {"divide by zero", "division by zero", "div-by-zero", "zero division", "len(xs) == 0", "empty slice", "empty input", "divides by zero"},
	"file-leak":       {"not closed", "never closed", "file leak", "f.close", "defer f.close", "close the file", "missing close", "fd leak", "handle leak"},
	"infinite-retry":  {"infinite loop", "infinite retry", "no backoff", "exponential backoff", "retry limit", "no cap", "unbounded retry", "max retries", "maximum retries", "never gives up"},
}

func coverage(reply string) (int, []string) {
	low := strings.ToLower(reply)
	var found []string
	for issue, kws := range plantedIssues {
		for _, kw := range kws {
			if strings.Contains(low, kw) {
				found = append(found, issue)
				break
			}
		}
	}
	return len(found), found
}

// arm is one experimental configuration of the same audit task.
type arm struct {
	name     string
	delegate bool   // register the task tool + subagents
	forced   bool   // explicitly instruct delegation in the prompt
	planner  bool   // enable the pre-flight planner (fan out read-only explorers)
	system   string // top-level system prompt
}

const soloSystem = "You are magi, a terminal coding agent. Use your tools (read/grep/glob/list) to inspect the working directory and do the user's task YOURSELF in a single pass. Never ask the user to paste files. Be thorough and concise."

// conductorSystem is the dedicated-orchestrator prompt (a delegate-first lead;
// delegate-first): delegate-first identity + a HARD parallel-batch mandate, so the
// model fans out all independent work in ONE task call instead of dribbling
// sequential dispatches (which serialize even over a non-blocking dispatch).
const conductorSystem = "You are the orchestration LEAD. You do NOT read or fix code yourself — you DELEGATE every piece of substantive work to subagents via the task tool, then synthesize their results. Your value is coordination, not doing.\n\n" +
	"PARALLELISM IS MANDATORY. First decompose the request into ALL its independent pieces, then dispatch them in a SINGLE task call as tasks:[{agent,prompt},...] so they run at the SAME TIME. NEVER dispatch independent pieces one task call at a time across turns — that serializes the work and is a failure of your role. For a multi-file audit/review, split BY FILE (one subagent per file) and put every one of them in that single tasks:[...] call.\n\n" +
	"Each subagent starts COLD and cannot see this conversation, so give it rich self-contained context: the exact file to review and what to report. After the single dispatch, do not idle or re-dispatch; the results arrive as messages. When all results are in, synthesize them into ONE final report listing every concrete defect with its file. Be concise."

// TestConductorParallel validates the dedicated-orchestrator prompt: does it make
// the model batch all subagents into ONE parallel task call (fast, dur ~ solo)
// instead of serializing (dur ~ N x solo)? Compares conductor vs solo on dur,
// spawns, and coverage. Gated like the others.
func TestConductorParallel(t *testing.T) {
	base := os.Getenv("MAGI_EVAL_BASE")
	if base == "" {
		t.Skip("set MAGI_EVAL_BASE/_MODEL/_KEY to run the conductor validation")
	}
	model := os.Getenv("MAGI_EVAL_MODEL")
	if model == "" {
		model = "qwen3-coder:30b"
	}
	key := os.Getenv("MAGI_EVAL_KEY")
	if key == "" {
		key = os.Getenv("MAGI_API_KEY")
	}
	llm := openai.New(base, key)
	plat := platform.New()
	const trials = 2
	const prompt = "Audit this Go service for ALL concrete defects across security, concurrency, correctness, robustness, and resource management. There are multiple files. Report every issue you find with the file name and a one-line explanation."
	arms := []arm{
		{name: "solo     ", delegate: false, system: soloSystem},
		{name: "conductor", delegate: true, system: conductorSystem},
	}
	for i := 0; i < trials; i++ {
		for _, am := range arms {
			reply, r := runReview(t, llm, model, plat, prompt, am)
			cov, found := coverage(reply)
			t.Logf("trial %d %s cov=%2d/10 spawns=%d dur=%s tok-out=%d %v",
				i+1, am.name, cov, r.Spawns, r.Dur.Round(time.Second), r.TokOut, found)
		}
	}
}

// TestMultiAgentAB compares, on a COMPLEX multi-file audit, three arms:
//   - solo:        one agent, no task tool
//   - orch-auto:   task tool available, model decides whether to delegate
//   - orch-forced: delegation explicitly instructed (real fan-out)
//
// It measures planted-issue coverage (quality), wall time, tokens, and whether
// the model actually spawned subagents — answering whether orchestration's cost
// buys quality when the work is genuinely decomposable.
//
//	MAGI_EVAL_BASE=http://localhost:11434/v1 MAGI_EVAL_MODEL=minimax-m3:cloud \
//	MAGI_EVAL_KEY="ollama key" \
//	  go test ./internal/eval/ -run TestMultiAgentAB -v -timeout 40m
func TestMultiAgentAB(t *testing.T) {
	base := os.Getenv("MAGI_EVAL_BASE")
	if base == "" {
		t.Skip("set MAGI_EVAL_BASE/_MODEL/_KEY to run the multi-agent A/B")
	}
	model := os.Getenv("MAGI_EVAL_MODEL")
	if model == "" {
		model = "qwen3-coder:30b"
	}
	key := os.Getenv("MAGI_EVAL_KEY")
	if key == "" {
		key = os.Getenv("MAGI_API_KEY")
	}
	llm := openai.New(base, key)
	plat := platform.New()

	const trials = 3
	const prompt = "Audit this Go service for ALL concrete defects across security, concurrency, correctness, robustness, and resource management. There are multiple files. Report every issue you find with the file name and a one-line explanation."
	arms := []arm{
		{name: "solo      ", delegate: false, system: soloSystem},
		{name: "orch-auto ", delegate: true, system: orchestratorSystem},
		{name: "orch-force", delegate: true, forced: true, system: orchestratorSystem},
	}

	type agg struct {
		cov, spawns, n int
		dur            time.Duration
		tokOut         int
	}
	totals := map[string]*agg{}
	for i := 0; i < trials; i++ {
		for _, am := range arms {
			reply, r := runReview(t, llm, model, plat, prompt, am)
			cov, found := coverage(reply)
			t.Logf("trial %d %s cov=%2d/10 spawns=%d dur=%s tok-out=%d %v",
				i+1, am.name, cov, r.Spawns, r.Dur.Round(time.Second), r.TokOut, found)
			a := totals[am.name]
			if a == nil {
				a = &agg{}
				totals[am.name] = a
			}
			a.cov += cov
			a.spawns += r.Spawns
			a.dur += r.Dur
			a.tokOut += r.TokOut
			a.n++
		}
	}
	t.Log("=== MULTI-AGENT A/B (complex multi-file audit, 10 planted defects) ===")
	for _, am := range arms {
		a := totals[am.name]
		n := float64(a.n)
		t.Logf("%s  avg-coverage=%.2f/10  avg-dur=%s  avg-tok-out=%.0f  avg-spawns=%.1f",
			am.name, float64(a.cov)/n, (a.dur / time.Duration(a.n)).Round(time.Second),
			float64(a.tokOut)/n, float64(a.spawns)/n)
	}
}

func runReview(t *testing.T, llm port.LLMProvider, model string, plat port.Platform, prompt string, am arm) (string, Result) {
	r := Result{}
	dir, err := os.MkdirTemp("", "magi-ab-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	for name, content := range reviewCorpus {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store, err := jsonl.New(filepath.Join(dir, ".store"))
	if err != nil {
		t.Fatal(err)
	}
	reg := builtin.Default()
	reg.Register(builtin.Ask{})
	reg.Register(builtin.Report{})
	cfg := app.Config{
		Model:      session.ModelRef{Provider: "openai", Model: model},
		Permission: "allow",
		MaxSteps:   40,
		System:     am.system,
	}
	if am.delegate {
		reg.Register(builtin.Task{})
		cfg.Agents = evalAgents()
	}
	if am.planner {
		cfg.Planner = true
		cfg.Agents = plannerEvalAgents()
	}
	task := prompt
	if am.forced {
		task = "Delegate this audit: assign different files/concerns to the coder, tester, and reviewer subagents IN PARALLEL (one task call with multiple tasks), then synthesize their findings into one final report. " + prompt
	}
	ref := cfg.Model
	a := app.New(store, llm, reg, bus.New(), plat, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: dir, Model: ref})
	if err != nil {
		t.Fatal(err)
	}
	sub, cancelSub, err := a.Subscribe(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelSub()
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: task}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "eval"},
	})
	start := time.Now()
	var reply strings.Builder
	for done := false; !done; {
		select {
		case e, ok := <-sub:
			if !ok {
				done = true
				break
			}
			switch e.Type {
			case event.TypeAgentSpawned:
				r.Spawns++
			case event.TypePartAppended:
				var d event.PartAppendedData
				if json.Unmarshal(e.Data, &d) == nil && d.Role == session.RoleAssistant {
					switch d.Part.Kind {
					case session.PartText:
						reply.WriteString(d.Part.Text)
						reply.WriteString("\n")
					case session.PartToolCall:
						r.ToolCalls++
					}
				}
			case event.TypeTurnFinished:
				var d event.TurnFinishedData
				if json.Unmarshal(e.Data, &d) == nil {
					r.TokIn += d.Usage.In
					r.TokOut += d.Usage.Out
				}
				r.Finished = true
				done = true
			case event.TypeError:
				r.Finished = true
				done = true
			}
		case <-ctx.Done():
			r.Note = "timeout"
			done = true
		}
	}
	r.Dur = time.Since(start)
	return reply.String(), r
}
