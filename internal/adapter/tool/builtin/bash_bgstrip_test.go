package builtin

import "testing"

// stripBackgroundArtifacts drops a leading nohup and a single trailing `&` (which only make a
// background=true job's shell exit immediately), but never a `&&` chain or a mid-command operator.
func TestStripBackgroundArtifacts(t *testing.T) {
	strip := []struct{ in, want string }{
		{"nohup python3 server.py > /dev/null 2>&1 &", "python3 server.py > /dev/null 2>&1"},
		{"cd /app && python server.py &", "cd /app && python server.py"},
		{"nohup ./daemon", "./daemon"},
		{"python server.py  &  ", "python server.py"},
	}
	for _, c := range strip {
		got, ok := stripBackgroundArtifacts(c.in)
		if got != c.want || !ok {
			t.Errorf("stripBackgroundArtifacts(%q) = %q,%v; want %q,true", c.in, got, ok, c.want)
		}
	}
	keep := []string{"python server.py", "make && ./run", "a && b"}
	for _, in := range keep {
		if got, ok := stripBackgroundArtifacts(in); got != in || ok {
			t.Errorf("stripBackgroundArtifacts(%q) should be unchanged, got %q,%v", in, got, ok)
		}
	}
}
