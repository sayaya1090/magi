package port

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

// ManagedExperienceStore is an optional local lifecycle boundary. Existing,
// independently authored memories remain on ExperienceStore; they are never
// silently adopted, overwritten or withdrawn by a managed observation.
type ManagedExperienceStore interface {
	ApplyExperience(context.Context, ExperienceChange) (ExperienceReceipt, error)
}

// ManagedExperienceInspector exposes read/search access over local managed
// records and pinned core-tier memories without rewriting legacy files.
type ManagedExperienceInspector interface {
	SearchManagedExperience(ctx context.Context, query, kind string, minSim float64, limit int) ([]ManagedRecordSummary, error)
	CoreMemories(ctx context.Context, maxChars int) ([]Memory, error)
}

type ManagedRecordSummary struct {
	ObjectID        string  `json:"object_id"`
	Revision        string  `json:"revision"`
	ContentRevision string  `json:"content_revision"`
	Owner           string  `json:"owner"`
	Kind            string  `json:"kind"`
	State           string  `json:"state"`
	HostOutcome     string  `json:"host_outcome"`
	Similarity      float64 `json:"similarity,omitempty"`
	Score           int     `json:"score,omitempty"`
	Cold            bool    `json:"cold,omitempty"`
	UpdatedAt       string  `json:"updated_at,omitempty"`
	UpdatedEpoch    int64   `json:"updated_epoch,omitempty"`
	Memory          *Memory `json:"memory,omitempty"`
	Skill           *Skill  `json:"skill,omitempty"`
}

type ExperienceChange struct {
	ObjectID         string  `json:"object_id"`
	ExpectedRevision string  `json:"expected_revision"`
	OperationID      string  `json:"operation_id"`
	ObservationID    string  `json:"observation_id"`
	Owner            string  `json:"owner"`
	Scope            string  `json:"scope"`
	Kind             string  `json:"kind"`  // memory or skill
	State            string  `json:"state"` // active, archived or withdrawn
	Memory           *Memory `json:"memory,omitempty"`
	Skill            *Skill  `json:"skill,omitempty"`
	Source           string  `json:"source"`
	HostOutcome      string  `json:"host_outcome"`
}

type ExperienceReceipt struct {
	ObjectID        string `json:"object_id"`
	Revision        string `json:"revision"`
	ContentRevision string `json:"content_revision"`
	State           string `json:"state"`
}

const maxDescriptionRunes = 200

func clipMemoryRunes(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:max-1])) + "…"
}

// EffectiveDescription returns the explicit description (capped to 200 runes) or
// derives a one-line summary from Text when reading pre-schema records.
func (m Memory) EffectiveDescription() string {
	if d := strings.TrimSpace(m.Description); d != "" {
		return clipMemoryRunes(d, maxDescriptionRunes)
	}
	t := strings.TrimSpace(m.Text)
	for _, pfx := range []string{"[project] ", "[team] ", "[global] "} {
		if strings.HasPrefix(t, pfx) {
			t = strings.TrimSpace(strings.TrimPrefix(t, pfx))
			break
		}
	}
	if idx := strings.IndexByte(t, '\n'); idx >= 0 {
		t = strings.TrimSpace(t[:idx])
	}
	if idx := strings.Index(t, ". "); idx >= 24 && idx < 180 {
		t = t[:idx+1]
	}
	return clipMemoryRunes(t, maxDescriptionRunes)
}

// EffectiveKind returns Kind when valid, or infers "lesson" for legacy records.
func (m Memory) EffectiveKind() string {
	switch m.Kind {
	case "lesson", "fact", "procedure", "preference", "pitfall":
		return m.Kind
	}
	return "lesson"
}

// EffectiveAreas returns Areas when present, or infers from a leading [tag] in Text.
func (m Memory) EffectiveAreas() []string {
	if len(m.Areas) > 0 {
		return m.Areas
	}
	t := strings.TrimSpace(m.Text)
	for _, pfx := range []string{"[project] ", "[team] ", "[global] "} {
		if strings.HasPrefix(t, pfx) {
			t = strings.TrimSpace(strings.TrimPrefix(t, pfx))
			break
		}
	}
	if strings.HasPrefix(t, "[") {
		if end := strings.Index(t, "]"); end > 1 && end <= 30 {
			tag := strings.TrimSpace(t[1:end])
			switch tag {
			case "구현":
				return []string{"구현"}
			case "디버깅", "버그수정", "버그":
				return []string{"디버깅"}
			case "분석", "조사":
				return []string{"분석"}
			case "설정", "배포", "인프라":
				return []string{"설정"}
			case "리팩터링", "리팩토링", "구조개선":
				return []string{"리팩터링"}
			case "문서":
				return []string{"문서"}
			}
		}
	}
	return nil
}

// EffectiveTier returns "core" or "reference".
func (m Memory) EffectiveTier() string {
	switch m.Tier {
	case "core", "reference":
		return m.Tier
	}
	if m.Kind == "preference" {
		return "core"
	}
	return "reference"
}

// EffectiveStatus returns Status when valid, or "observed" for pre-schema records.
func (m Memory) EffectiveStatus() string {
	switch m.Status {
	case "hypothesis", "observed", "conflicted", "disconfirmed":
		return m.Status
	}
	return "observed"
}

// IsExpired reports whether ValidUntil is set and precedes now.
func (m Memory) IsExpired(now time.Time) bool {
	vu := strings.TrimSpace(m.ValidUntil)
	if vu == "" {
		return false
	}
	if ts, err := time.Parse(time.RFC3339, vu); err == nil {
		return now.After(ts)
	}
	if d, err := time.Parse("2006-01-02", vu); err == nil {
		endOfDay := d.UTC().Add(24*time.Hour - time.Nanosecond)
		return now.UTC().After(endOfDay)
	}
	return false
}

