package git

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/atomicfile"
	"github.com/sayaya1090/magi/internal/core/embed"
	"github.com/sayaya1090/magi/internal/core/lexical"
	"github.com/sayaya1090/magi/internal/filelock"
	"github.com/sayaya1090/magi/internal/port"
)

// Managed records are local projections with explicit identity and CAS. They do
// not rewrite legacy memory/skill files or claim to implement fleet replication.
// One atomic JSON file contains both current state and idempotency receipts, so
// a crash cannot publish content without its operation marker.
type managedRecord struct {
	Version         int                         `json:"version"`
	Change          port.ExperienceChange       `json:"change"`
	Revision        string                      `json:"revision"`
	ContentRevision string                      `json:"content_revision"`
	Operations      map[string]managedOperation `json:"operations"`
}
type managedOperation struct {
	Digest  string                 `json:"digest"`
	Receipt port.ExperienceReceipt `json:"receipt"`
}

var managedID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

func digestJSON(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (s *Store) ApplyExperience(ctx context.Context, c port.ExperienceChange) (port.ExperienceReceipt, error) {
	var empty port.ExperienceReceipt
	if !managedID.MatchString(c.ObjectID) || !managedID.MatchString(c.OperationID) || c.Owner == "" || c.ObservationID == "" {
		return empty, fmt.Errorf("managed experience requires safe object/operation IDs, owner and observation")
	}
	if c.Scope != "" && c.Scope != "project" {
		return empty, fmt.Errorf("managed experience currently supports project scope only")
	}
	if c.State != "active" && c.State != "withdrawn" && c.State != "archived" {
		return empty, fmt.Errorf("invalid managed experience state")
	}
	if c.Kind != "memory" && c.Kind != "skill" {
		return empty, fmt.Errorf("invalid managed experience kind")
	}
	if c.State == "active" && ((c.Kind == "memory" && (c.Memory == nil || c.Memory.Text == "" || c.Skill != nil)) || (c.Kind == "skill" && (c.Skill == nil || c.Skill.Name == "" || c.Skill.Body == "" || c.Memory != nil))) {
		return empty, fmt.Errorf("managed experience payload does not match its kind")
	}
	if c.Kind == "skill" && c.State == "active" && c.HostOutcome != "verified" && !(c.Owner == "user:legacy-import" && c.Source == "local-skill-import" && c.HostOutcome == "user-imported") {
		return empty, fmt.Errorf("automatic skill requires host verified")
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return empty, err
	}
	release, err := filelock.Acquire(ctx, filepath.Join(s.dir, ".managed-write.lock"))
	if err != nil {
		return empty, err
	}
	defer release()
	dir := filepath.Join(s.dir, "managed")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return empty, err
	}
	path := filepath.Join(dir, c.ObjectID+".json")
	var old managedRecord
	b, err := atomicfile.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(b, &old); err != nil || old.Version != 1 || old.Change.ObjectID != c.ObjectID {
			return empty, fmt.Errorf("invalid managed record; refusing overwrite")
		}
		// Owner comes from the host bridge, never a model-authored owner claim.
		if old.Change.Owner != c.Owner || old.Change.Kind != c.Kind {
			return empty, fmt.Errorf("managed experience ownership/kind mismatch")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return empty, err
	}
	opDigest := digestJSON(c)
	if op, ok := old.Operations[c.OperationID]; ok {
		if op.Digest != opDigest {
			return empty, fmt.Errorf("operation ID reused with different content")
		}
		return op.Receipt, nil
	}
	if old.Revision != c.ExpectedRevision {
		return empty, fmt.Errorf("managed experience revision conflict")
	}
	if old.Revision == "" && c.State != "active" {
		return empty, fmt.Errorf("cannot retire missing managed experience")
	}
	// Retain payload behind a tombstone for audit/restore; retrieval checks state.
	if c.State != "active" {
		c.Memory = old.Change.Memory
		c.Skill = old.Change.Skill
	}
	revision := digestJSON(struct {
		Parent string
		Change port.ExperienceChange
	}{old.Revision, c})
	contentRevision := digestJSON(struct {
		Kind, State string
		Memory      *port.Memory
		Skill       *port.Skill
	}{c.Kind, c.State, c.Memory, c.Skill})
	receipt := port.ExperienceReceipt{ObjectID: c.ObjectID, Revision: revision, ContentRevision: contentRevision, State: c.State}
	if old.Operations == nil {
		old.Operations = map[string]managedOperation{}
	}
	old.Operations[c.OperationID] = managedOperation{Digest: opDigest, Receipt: receipt}
	record := managedRecord{Version: 1, Change: c, Revision: revision, ContentRevision: contentRevision, Operations: old.Operations}
	b, err = json.MarshalIndent(record, "", "  ")
	if err != nil {
		return empty, err
	}
	if err := atomicfile.Write(path, append(b, '\n'), 0o600); err != nil {
		return empty, err
	}
	return receipt, nil
}

func (s *Store) managedPool(ctx context.Context, query string) ([]Scored[port.Memory], []Scored[port.Skill], error) {
	paths, err := filepath.Glob(filepath.Join(s.dir, "managed", "*.json"))
	if err != nil {
		return nil, nil, err
	}
	if len(paths) == 0 {
		return nil, nil, nil
	}
	release, err := filelock.Acquire(ctx, filepath.Join(s.dir, ".managed-write.lock"))
	if err != nil {
		return nil, nil, err
	}
	defer release()
	var memories []Scored[port.Memory]
	var skills []Scored[port.Skill]
	terms := tokenize(query)
	for _, path := range paths {
		b, err := atomicfile.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		var r managedRecord
		if err := json.Unmarshal(b, &r); err != nil || r.Version != 1 || filepath.Base(path) != r.Change.ObjectID+".json" {
			return nil, nil, fmt.Errorf("invalid managed record: %s", filepath.Base(path))
		}
		if r.Change.State != "active" {
			continue
		}
		retention, err := s.retention(ctx, r, "")
		if err != nil {
			return nil, nil, err
		}
		if retention.Archived {
			continue
		}
		if r.Change.Kind == "memory" && r.Change.Memory != nil {
			m := *r.Change.Memory
			now := time.Now()
			if s.retentionNow != nil {
				now = s.retentionNow()
			}
			if m.EffectiveStatus() == "disconfirmed" || m.IsExpired(now) {
				continue
			}
			contentRevision := r.ContentRevision
			if contentRevision == "" {
				contentRevision = r.Revision
			}
			m.ID = "managed:" + r.Change.ObjectID + "@" + contentRevision
			score := overlap(terms, m.Description+" "+m.Text)
			if matchesManagedID(query, r.Change.ObjectID, m.ID) {
				score += 1000
			}
			memories = append(memories, Scored[port.Memory]{Score: score, Cold: retention.Cold, V: m})
		} else if r.Change.Kind == "skill" && r.Change.Skill != nil {
			sk := *r.Change.Skill
			score := overlap(terms, sk.Name+" "+sk.Description+" "+sk.Body)
			if matchesManagedID(query, r.Change.ObjectID, "managed:"+r.Change.ObjectID) {
				score += 1000
			}
			skills = append(skills, Scored[port.Skill]{Score: score, Cold: retention.Cold, V: sk})
		}
	}
	return memories, skills, nil
}

func matchesManagedID(query, objectID, fullID string) bool {
	if objectID == "" {
		return false
	}
	q := strings.ToLower(strings.TrimSpace(query))
	obj := strings.ToLower(objectID)
	full := strings.ToLower(fullID)
	if q == obj || q == "managed:"+obj || q == full || strings.Contains(q, "managed:"+obj) {
		return true
	}
	for _, f := range strings.Fields(q) {
		f = strings.Trim(f, "[](),:;\"'`")
		if idx := strings.IndexByte(f, '@'); idx > 0 {
			f = f[:idx]
		}
		if f == obj || f == "managed:"+obj {
			return true
		}
	}
	return false
}

// embedForRank returns one vector per chunk followed by the query's.
// With a warmer embedder, it checks the vector cache without blocking; if any chunk
// is missing, it warms the cache in the background and avoids stalling the caller.
func embedForRank(ctx context.Context, emb Same, chunks []string, query string) ([][]float32, error) {
	if w, ok := emb.(interface {
		Peekable() bool
		Peek(texts []string) ([][]float32, bool)
		Warm(texts []string)
	}); ok && w.Peekable() {
		docs, all := w.Peek(chunks)
		if !all {
			w.Warm(chunks)
			return nil, errors.New("embedding cache is still being filled")
		}
		qctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		q, err := emb.Embed(qctx, []string{query})
		if err != nil {
			return nil, err
		}
		if len(q) != 1 {
			return nil, fmt.Errorf("asked for 1 query embedding and got %d", len(q))
		}
		return append(docs, q[0]), nil
	}
	return emb.Embed(ctx, append(append([]string{}, chunks...), query))
}

// SearchManagedExperience lists or searches active managed records, scoring by
// both lexical overlap and best-chunk cosine similarity when an embedder is attached.
func (s *Store) SearchManagedExperience(ctx context.Context, query, kind string, minSim float64, limit int) ([]port.ManagedRecordSummary, error) {
	paths, err := filepath.Glob(filepath.Join(s.dir, "managed", "*.json"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, nil
	}
	sort.Strings(paths)
	release, err := filelock.Acquire(ctx, filepath.Join(s.dir, ".managed-write.lock"))
	if err != nil {
		return nil, err
	}
	var items []port.ManagedRecordSummary
	now := time.Now()
	if s.retentionNow != nil {
		now = s.retentionNow()
	}
	for _, path := range paths {
		b, err := atomicfile.ReadFile(path)
		if err != nil {
			continue
		}
		var r managedRecord
		if json.Unmarshal(b, &r) != nil || r.Version != 1 || filepath.Base(path) != r.Change.ObjectID+".json" {
			continue
		}
		if r.Change.State != "active" {
			continue
		}
		if kind != "" && r.Change.Kind != kind {
			continue
		}
		retention, err := s.retention(ctx, r, "")
		if err != nil || retention.Archived {
			continue
		}
		contentRev := r.ContentRevision
		if contentRev == "" {
			contentRev = r.Revision
		}
		var updatedStr string
		var updatedEpoch int64
		if fi, statErr := os.Stat(path); statErr == nil {
			updatedStr = fi.ModTime().UTC().Format(time.RFC3339)
			updatedEpoch = fi.ModTime().Unix()
		}
		sum := port.ManagedRecordSummary{
			ObjectID:        r.Change.ObjectID,
			Revision:        r.Revision,
			ContentRevision: contentRev,
			Owner:           r.Change.Owner,
			Kind:            r.Change.Kind,
			State:           r.Change.State,
			HostOutcome:     r.Change.HostOutcome,
			Cold:            retention.Cold,
			UpdatedAt:       updatedStr,
			UpdatedEpoch:    updatedEpoch,
		}
		if r.Change.Kind == "memory" && r.Change.Memory != nil {
			m := *r.Change.Memory
			if m.Description == "" {
				m.Description = m.EffectiveDescription()
			}
			if m.Kind == "" {
				m.Kind = m.EffectiveKind()
			}
			if len(m.Areas) == 0 {
				m.Areas = m.EffectiveAreas()
			}
			if m.Tier == "" {
				m.Tier = m.EffectiveTier()
			}
			if m.Status == "" {
				m.Status = m.EffectiveStatus()
			}
			m.ID = "managed:" + r.Change.ObjectID + "@" + contentRev
			if m.Status == "disconfirmed" || (query != "" && m.IsExpired(now)) {
				continue
			}
			sum.Memory = &m
		} else if r.Change.Kind == "skill" && r.Change.Skill != nil {
			sk := *r.Change.Skill
			sum.Skill = &sk
		} else {
			continue
		}
		items = append(items, sum)
	}
	release()

	q := strings.TrimSpace(query)
	if q != "" && len(items) > 0 {
		// Unified BM25F field-weighted lexical scoring
		docFields := make([]lexical.DocumentFields, len(items))
		for i := range items {
			if items[i].Memory != nil {
				docFields[i] = lexical.DocumentFields{
					Fields: []lexical.Field{
						{Text: items[i].ObjectID + " " + strings.Join(items[i].Memory.Areas, " "), Weight: 3.0},
						{Text: items[i].Memory.Description, Weight: 2.0},
						{Text: items[i].Memory.Text, Weight: 1.0},
					},
				}
			} else if items[i].Skill != nil {
				docFields[i] = lexical.DocumentFields{
					Fields: []lexical.Field{
						{Text: items[i].Skill.Name, Weight: 3.0},
						{Text: items[i].Skill.Description, Weight: 2.0},
						{Text: items[i].Skill.Body, Weight: 1.0},
					},
				}
			}
		}
		bmScores := lexical.FieldScores(q, docFields)
		for i := range items {
			score := 0
			if i < len(bmScores) && bmScores[i] > 0 {
				score = max(1, int(math.Round(bmScores[i]*1000)))
			}
			memID := ""
			if items[i].Memory != nil {
				memID = items[i].Memory.ID
			}
			if matchesManagedID(q, items[i].ObjectID, memID) {
				score += 1000
			}
			items[i].Score = score
		}

		if s.same != nil && s.same.Available() {
			var chunks []string
			var owner []int
			for i := range items {
				if items[i].Memory != nil {
					for _, c := range embed.ChunksWithHeader(items[i].Memory.Description, items[i].Memory.Text) {
						chunks = append(chunks, c)
						owner = append(owner, i)
					}
				} else if items[i].Skill != nil {
					for _, c := range embed.ChunksWithHeader(items[i].Skill.Name+": "+items[i].Skill.Description, items[i].Skill.Body) {
						chunks = append(chunks, c)
						owner = append(owner, i)
					}
				}
			}
			if vecs, err := embedForRank(ctx, s.same, chunks, q); err == nil && len(vecs) == len(chunks)+1 {
				qv := vecs[len(vecs)-1]
				for j, i := range owner {
					if c := embed.Cosine(vecs[j], qv); c > items[i].Similarity {
						items[i].Similarity = c
					}
				}
			}
		}
		hasEmbedder := s.same != nil && s.same.Available()
		filtered := items[:0]
		for _, it := range items {
			if minSim > 0 {
				if hasEmbedder {
					if minSim >= 0.70 {
						if it.Similarity >= minSim {
							filtered = append(filtered, it)
						}
					} else {
						minScore := 200
						if minSim >= 0.40 {
							minScore = 1500
						}
						if it.Similarity >= minSim || it.Score >= minScore {
							filtered = append(filtered, it)
						}
					}
				} else {
					minScore := 200
					if minSim >= 0.70 {
						minScore = 3000
					} else if minSim >= 0.40 {
						minScore = 1500
					}
					if it.Score >= minScore {
						filtered = append(filtered, it)
					}
				}
			} else if it.Similarity > 0 || it.Score > 0 {
				filtered = append(filtered, it)
			}
		}
		items = filtered
		sort.SliceStable(items, func(a, b int) bool {
			if (items[a].Score >= 1000) != (items[b].Score >= 1000) {
				return items[a].Score >= 1000
			}
			if items[a].Similarity != items[b].Similarity {
				return items[a].Similarity > items[b].Similarity
			}
			return items[a].Score > items[b].Score
		})
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

// CoreMemories returns active, observed, non-expired core-tier memories up to maxChars.
func (s *Store) CoreMemories(ctx context.Context, maxChars int) ([]port.Memory, error) {
	items, err := s.SearchManagedExperience(ctx, "", "memory", 0, 0)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if s.retentionNow != nil {
		now = s.retentionNow()
	}
	var out []port.Memory
	used := 0
	for _, it := range items {
		if it.Memory == nil || it.Cold {
			continue
		}
		m := *it.Memory
		if m.EffectiveTier() != "core" || m.EffectiveStatus() != "observed" || m.IsExpired(now) {
			continue
		}
		line := m.EffectiveDescription()
		if line == "" {
			continue
		}
		if maxChars > 0 && used+len(line)+4 > maxChars && len(out) > 0 {
			break
		}
		used += len(line) + 4
		out = append(out, m)
	}
	return out, nil
}

func (s *Store) managedInventory(ctx context.Context) ([]SkillInfo, error) {
	paths, err := filepath.Glob(filepath.Join(s.dir, "managed", "*.json"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, nil
	}
	release, err := filelock.Acquire(ctx, filepath.Join(s.dir, ".managed-write.lock"))
	if err != nil {
		return nil, err
	}
	defer release()
	var out []SkillInfo
	for _, path := range paths {
		b, err := atomicfile.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var r managedRecord
		if err := json.Unmarshal(b, &r); err != nil || r.Version != 1 || filepath.Base(path) != r.Change.ObjectID+".json" {
			return nil, fmt.Errorf("invalid managed record")
		}
		if r.Change.State != "active" {
			continue
		}
		retention, err := s.retention(ctx, r, "")
		if err != nil {
			return nil, err
		}
		info := SkillInfo{Name: "managed:" + r.Change.ObjectID, Kind: r.Change.Kind, Observed: 1}
		if retentionOwner(r.Change.Owner) {
			public := retention.Public()
			info.Retention = &public
		}
		if r.Change.Memory != nil {
			info.Description = r.Change.Memory.Text
			info.Body = r.Change.Memory.Text
		}
		if r.Change.Skill != nil {
			info.Description = r.Change.Skill.Description
			info.Body = r.Change.Skill.Body
		}
		out = append(out, info)
	}
	return out, nil
}

// Local administration withdraws a managed object rather than deleting its
// retry history. A later stale plugin update conflicts instead of resurrecting it.
func (s *Store) forgetManaged(ctx context.Context, id string) error {
	if !managedID.MatchString(id) {
		return fmt.Errorf("invalid managed ID")
	}
	b, err := atomicfile.ReadFile(filepath.Join(s.dir, "managed", id+".json"))
	if err != nil {
		return err
	}
	var r managedRecord
	if err := json.Unmarshal(b, &r); err != nil || r.Version != 1 || r.Change.ObjectID != id {
		return fmt.Errorf("invalid managed record")
	}
	c := r.Change
	c.ExpectedRevision = r.Revision
	c.State = "withdrawn"
	c.Memory = nil
	c.Skill = nil
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	c.OperationID = hex.EncodeToString(nonce)
	c.ObservationID = "local-withdrawal:" + c.OperationID
	_, err = s.ApplyExperience(ctx, c)
	return err
}
