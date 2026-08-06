package lua

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// Children that run at the same time must not write the same tree.
//
// spawn_all's whole point is that they run together, and a child with no workspace of its own works
// in the PARENT's directory. Two of those editing one file is a lost update that nobody reports:
// each child's account says it made its change, both are telling the truth about what they wrote,
// and one of the writes is simply gone. The parent's guard cannot see it either — it captures a
// file before and after, assuming one thing at a time touches it.
func TestSpawnAllRefusesConcurrentWritersInOneTree(t *testing.T) {
	write := []string{"read", "edit"}
	read := []string{"read", "grep"}

	cases := []struct {
		name    string
		specs   []port.SpawnSpec
		refused bool
	}{
		{"two writers in the parent's tree", []port.SpawnSpec{
			{Tools: write}, {Tools: write}}, true},
		{"bash counts as writing", []port.SpawnSpec{
			{Tools: []string{"bash"}}, {Tools: write}}, true},
		{"one writer is nobody to collide with", []port.SpawnSpec{
			{Tools: write}, {Tools: read}}, false},
		{"readers may share the tree", []port.SpawnSpec{
			{Tools: read}, {Tools: read}, {Tools: read}}, false},
		{"their own checkouts, so nothing is shared", []port.SpawnSpec{
			{Tools: write, Workspace: "clone"}, {Tools: write, Workspace: "clone"}}, false},
		{"one clone, one in the parent's tree", []port.SpawnSpec{
			{Tools: write, Workspace: "clone"}, {Tools: write}}, false},
	}
	for _, c := range cases {
		err := refuseSharedWrites(c.specs)
		if c.refused && err == nil {
			t.Errorf("%s: allowed", c.name)
		}
		if !c.refused && err != nil {
			t.Errorf("%s: refused (%v)", c.name, err)
		}
		if c.refused && err != nil && !strings.Contains(err.Error(), `workspace="clone"`) {
			t.Errorf("%s: the refusal does not say how to fix it: %v", c.name, err)
		}
	}
}
