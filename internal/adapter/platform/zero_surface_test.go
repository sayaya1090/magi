package platform

import "testing"

// New is the host adapter: the directories a daemon lives by must answer.
func TestNewAnswersItsDirectories(t *testing.T) {
	p := New()
	if p == nil {
		t.Fatal("no platform")
	}
	if p.ConfigDir() == "" || p.DataDir() == "" {
		t.Fatal("a daemon cannot live by empty directories")
	}
}
