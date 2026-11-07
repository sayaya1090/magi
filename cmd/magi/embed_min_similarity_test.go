package main

import (
	"testing"

	"github.com/sayaya1090/magi/internal/config"
)

// The floor comes from the environment, then config.toml, then the model. Only bge-m3 has a measured
// default; any other model keeps the old rule (0), because a floor on an unmeasured scale could
// silently turn semantic recall off.
func TestEmbedMinSimilarityPrecedence(t *testing.T) {
	set := 0.4
	cases := []struct {
		name  string
		cfg   config.Config
		model string
		env   string
		want  float64
	}{
		{"bge-m3 default", config.Config{}, "bge-m3", "", 0.5},
		{"bge-m3 tagged", config.Config{}, "bge-m3:latest", "", 0.5},
		{"bge-m3 with org", config.Config{}, "BAAI/bge-m3", "", 0.5},
		{"other model", config.Config{}, "nomic-embed-text", "", 0},
		{"no model", config.Config{}, "", "", 0},
		{"config wins over model", config.Config{EmbedMinSimilarity: &set}, "bge-m3", "", 0.4},
		{"env wins over config", config.Config{EmbedMinSimilarity: &set}, "bge-m3", "0.7", 0.7},
		{"bad env is ignored", config.Config{EmbedMinSimilarity: &set}, "bge-m3", "high", 0.4},
	}
	for _, c := range cases {
		if got := embedMinSimilarity(c.cfg, c.model, c.env); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
