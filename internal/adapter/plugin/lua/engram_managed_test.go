package lua

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	expgit "github.com/sayaya1090/magi/internal/adapter/experience/git"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
)

func TestEngramManagedSkillUndoReachesExperienceStore(t *testing.T) {
	wd := t.TempDir()
	store := expgit.New(t.TempDir())
	log := &syncLog{}
	fa := &fakeAnalyzer{replies: []string{
		`{"lesson":null,"skill":{"name":"compiler_port","description":"compiler port diagnostics","technique":"compiler original technique"}}`,
		`{"lesson":null,"skill":{"name":"compiler_port","description":"compiler port diagnostics","technique":"compiler revised technique"}}`,
	}}
	h := NewHostWithConfig(HostConfig{ToolSink: builtin.NewRegistry(), ContextReg: &fakeContextReg{}, Analyzer: fa, Experience: store, DataDir: t.TempDir(), Runtime: RuntimeInfo{Workdir: wd}, Logf: log.logf})
	if _, err := h.Load(context.Background(), filepath.Join("..", "..", "..", "..", "plugins", "engram")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.DrainEvents(5 * time.Second) })
	send := func(event string, fields map[string]string) {
		h.FireEventWith(event, fields)
		h.DrainEvents(5 * time.Second)
	}
	send("user_message", map[string]string{"session": "s", "text": "compiler port"})
	send("turn_finished", map[string]string{"session": "s", "text": "compiler original", "outcome": "verified"})
	_, skills, err := store.Retrieve(context.Background(), "compiler", nil)
	if err != nil || len(skills) != 1 {
		t.Fatalf("initial projection: %+v %v; %s", skills, err, log.String())
	}
	send("user_message", map[string]string{"session": "s", "text": "revise compiler"})
	send("turn_finished", map[string]string{"session": "s", "text": "compiler revised", "outcome": "verified"})
	_, skills, err = store.Retrieve(context.Background(), "compiler", nil)
	if err != nil || len(skills) != 1 || !strings.Contains(skills[0].Body, "revised") {
		t.Fatalf("revision: %+v %v; %s", skills, err, log.String())
	}
	send("user_message", map[string]string{"session": "s", "text": "N"})
	_, skills, err = store.Retrieve(context.Background(), "compiler", nil)
	if err != nil || len(skills) != 1 || !strings.Contains(skills[0].Body, "original") {
		t.Fatalf("undo must restore shared projection too: %+v %v; %s", skills, err, log.String())
	}
	b, err := os.ReadFile(filepath.Join(wd, ".claude", "skills", "compiler_port", "SKILL.md"))
	if err != nil || !strings.Contains(string(b), "original") {
		t.Fatalf("local undo: %s %v", b, err)
	}
	h.mu.Lock()
	p := h.plugins["engram"]
	h.mu.Unlock()
	if err := p.writeStoreValue("skill_lastused", "compiler_port\t2000-01-01", nil); err != nil {
		t.Fatal(err)
	}
	send("turn_finished", map[string]string{"session": "s", "text": "plain reply", "outcome": "done"})
	_, skills, err = store.Retrieve(context.Background(), "compiler", nil)
	if err != nil || len(skills) != 0 {
		t.Fatalf("archived skill remains retrievable: %+v %v; %s", skills, err, log.String())
	}
	if _, err := os.Stat(filepath.Join(wd, ".claude", "skills", ".archive", "compiler_port", "SKILL.md")); err != nil {
		t.Fatalf("local archived skill missing: %v", err)
	}
}

type lostManagedAck struct{ *expgit.Store }

func (s lostManagedAck) ApplyExperience(ctx context.Context, c port.ExperienceChange) (port.ExperienceReceipt, error) {
	_, err := s.Store.ApplyExperience(ctx, c)
	if err != nil {
		return port.ExperienceReceipt{}, err
	}
	return port.ExperienceReceipt{}, fmt.Errorf("simulated lost acknowledgement after commit")
}

type selectivelyBlockedManaged struct{ *expgit.Store }

func (s selectivelyBlockedManaged) ApplyExperience(ctx context.Context, c port.ExperienceChange) (port.ExperienceReceipt, error) {
	if strings.HasPrefix(c.ObjectID, "blocked") {
		return port.ExperienceReceipt{}, fmt.Errorf("blocked object")
	}
	return s.Store.ApplyExperience(ctx, c)
}

func TestEngramManagedOutboxDoesNotStarvePastFlushBudget(t *testing.T) {
	wd := t.TempDir()
	store := expgit.New(t.TempDir())
	log := &syncLog{}
	h := NewHostWithConfig(HostConfig{ToolSink: builtin.NewRegistry(), ContextReg: &fakeContextReg{}, Experience: selectivelyBlockedManaged{store}, DataDir: t.TempDir(), Runtime: RuntimeInfo{Workdir: wd}, Logf: log.logf})
	if _, err := h.Load(context.Background(), filepath.Join("..", "..", "..", "..", "plugins", "engram")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.DrainEvents(5 * time.Second) })
	objects := map[string]any{}
	var queue []map[string]any
	for i := 0; i < 9; i++ {
		id := fmt.Sprintf("blocked%d", i)
		if i == 8 {
			id = "healthy"
		}
		objects[id] = map[string]any{}
		queue = append(queue, map[string]any{"object_id": id, "operation_id": "operation" + id, "observation_id": "observation" + id, "kind": "memory", "state": "active", "text": "compiler recovery"})
	}
	b, err := json.Marshal(map[string]any{"links": map[string]any{}, "objects": objects, "queue": queue})
	if err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	p := h.plugins["engram"]
	h.mu.Unlock()
	if err := p.writeStoreValue("managed_experience_v1:"+wd, string(b), nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		h.FireEventWith("user_message", map[string]string{"session": "s", "text": "continue"})
		h.DrainEvents(5 * time.Second)
	}
	mem, _, err := store.Retrieve(context.Background(), "compiler", nil)
	if err != nil || len(mem) != 1 {
		t.Fatalf("eight failures starved healthy object: %+v %v; %s", mem, err, log.String())
	}
}

func TestEngramManagedRestartReplaysCommittedOperationWithoutDuplication(t *testing.T) {
	wd, data, root := t.TempDir(), t.TempDir(), t.TempDir()
	store := expgit.New(root)
	log := &syncLog{}
	load := func(experience port.ExperienceStore) *Host {
		h := NewHostWithConfig(HostConfig{ToolSink: builtin.NewRegistry(), ContextReg: &fakeContextReg{}, Analyzer: &fakeAnalyzer{reply: `{"lesson":null,"skill":{"name":"compiler_retry","description":"compiler retry","technique":"compiler retry technique"}}`}, Experience: experience, DataDir: data, Runtime: RuntimeInfo{Workdir: wd}, Logf: log.logf})
		if _, err := h.Load(context.Background(), filepath.Join("..", "..", "..", "..", "plugins", "engram")); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { h.DrainEvents(5 * time.Second) })
		return h
	}
	first := load(lostManagedAck{store})
	first.FireEventWith("user_message", map[string]string{"session": "s", "text": "compiler retry"})
	first.FireEventWith("turn_finished", map[string]string{"session": "s", "text": "compiler verified", "outcome": "verified"})
	first.DrainEvents(5 * time.Second)
	if !strings.Contains(log.String(), "pending") {
		t.Fatalf("lost acknowledgement was hidden: %s", log.String())
	}
	files, _ := filepath.Glob(filepath.Join(root, "managed", "*.json"))
	if len(files) != 1 {
		t.Fatalf("one committed projection: %v", files)
	}
	before, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	second := load(store)
	second.FireEventWith("user_message", map[string]string{"session": "resumed", "text": "continue"})
	second.DrainEvents(5 * time.Second)
	after, _ := os.ReadFile(files[0])
	if string(after) != string(before) {
		t.Fatal("replay changed revision or added a duplicate operation")
	}
	state, err := os.ReadFile(filepath.Join(data, "plugin-data", "engram.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(state), `\"expected_revision\"`) {
		t.Fatalf("acknowledged queue still pending: %s", state)
	}
}

func TestEngramUndoPreservesHumanEditsAndWithdrawsManagedProjection(t *testing.T) {
	wd := t.TempDir()
	store := expgit.New(t.TempDir())
	log := &syncLog{}
	h := NewHostWithConfig(HostConfig{ToolSink: builtin.NewRegistry(), ContextReg: &fakeContextReg{}, Analyzer: &fakeAnalyzer{reply: `{"lesson":null,"skill":{"name":"compiler_human","description":"compiler human","technique":"compiler original"}}`}, Experience: store, DataDir: t.TempDir(), Runtime: RuntimeInfo{Workdir: wd}, Logf: log.logf})
	if _, err := h.Load(context.Background(), filepath.Join("..", "..", "..", "..", "plugins", "engram")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.DrainEvents(5 * time.Second) })
	h.FireEventWith("user_message", map[string]string{"session": "s", "text": "compiler"})
	h.FireEventWith("turn_finished", map[string]string{"session": "s", "text": "compiler", "outcome": "verified"})
	h.DrainEvents(5 * time.Second)
	path := filepath.Join(wd, ".claude", "skills", "compiler_human", "SKILL.md")
	if err := os.WriteFile(path, []byte("human-authored replacement"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.FireEventWith("user_message", map[string]string{"session": "s", "text": "N"})
	h.DrainEvents(5 * time.Second)
	b, _ := os.ReadFile(path)
	if string(b) != "human-authored replacement" {
		t.Fatalf("human edit lost: %q", b)
	}
	_, skills, err := store.Retrieve(context.Background(), "compiler", nil)
	if err != nil || len(skills) != 0 {
		t.Fatalf("rejected automatic projection still active: %+v %v", skills, err)
	}
}

func TestEngramDoesNotAdoptUnmanagedSkillByItsMarker(t *testing.T) {
	wd := t.TempDir()
	path := filepath.Join(wd, ".claude", "skills", "unmanaged", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "independent skill\n<!-- engram: 작업 이력에서 자동 추출된 스킬 -->"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	log := &syncLog{}
	h := loadEngram(t, wd, &fakeAnalyzer{reply: `{"lesson":null,"skill":{"name":"unmanaged","description":"compiler","technique":"replace independent file"}}`}, log)
	h.FireEventWith("user_message", map[string]string{"session": "s", "text": "compiler"})
	h.FireEventWith("turn_finished", map[string]string{"session": "s", "text": "compiler verified", "outcome": "verified"})
	h.DrainEvents(5 * time.Second)
	b, _ := os.ReadFile(path)
	if string(b) != body {
		t.Fatalf("unmanaged skill adopted without consent: %q", b)
	}
}

func TestEngramManagedLessonCorrectionRetainsOneObject(t *testing.T) {
	wd := t.TempDir()
	root := t.TempDir()
	store := expgit.New(root)
	log := &syncLog{}
	fa := &fakeAnalyzer{replies: []string{
		`{"lesson":{"task":"compiler port","approach":"compiler old check","outcome":"success","lesson":"compiler port old advice"},"skill":null}`,
		`{"lesson":{"task":"compiler port","approach":"compiler corrected check","outcome":"success","lesson":"compiler port corrected advice","replaces":1},"skill":null}`,
	}}
	h := NewHostWithConfig(HostConfig{ToolSink: builtin.NewRegistry(), ContextReg: &fakeContextReg{}, Analyzer: fa, Experience: store, DataDir: t.TempDir(), Runtime: RuntimeInfo{Workdir: wd}, Logf: log.logf})
	if _, err := h.Load(context.Background(), filepath.Join("..", "..", "..", "..", "plugins", "engram")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.DrainEvents(5 * time.Second) })
	for _, text := range []string{"compiler original", "compiler correction"} {
		h.FireEventWith("user_message", map[string]string{"session": "s", "text": text})
		h.FireEventWith("turn_finished", map[string]string{"session": "s", "text": text, "outcome": "verified"})
		h.DrainEvents(5 * time.Second)
	}
	mem, _, err := store.Retrieve(context.Background(), "compiler", nil)
	if err != nil || len(mem) != 1 || !strings.Contains(mem[0].Text, "corrected advice") {
		t.Fatalf("one corrected projection: %+v %v; %s", mem, err, log.String())
	}
	files, _ := filepath.Glob(filepath.Join(root, "managed", "*.json"))
	if len(files) != 1 {
		t.Fatalf("correction created another identity: %v", files)
	}
}
