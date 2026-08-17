package app

import "testing"

func TestLooseIntShapes(t *testing.T) {
	for _, c := range []struct {
		in   any
		want int
	}{
		{float64(540), 540}, {"540", 540}, {"540.0", 540}, {" 42 ", 42}, {nil, 0}, {true, 0},
	} {
		if got := looseInt(c.in); got != c.want {
			t.Errorf("looseInt(%#v) = %d, want %d", c.in, got, c.want)
		}
	}
}
