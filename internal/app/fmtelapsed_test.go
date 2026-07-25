package app

import (
	"testing"
	"time"
)

// fmtElapsed renders the turn/deliberation clock the council cost-cap messages show: seconds under a
// minute, whole minutes under an hour, "HhMMm" beyond — with the minute part zero-padded. Untested;
// lock the three ranges and their boundaries.
func TestFmtElapsed(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m"}, // exactly a minute rolls to the minute form
		{90 * time.Second, "1m"}, // truncated, not rounded
		{59 * time.Minute, "59m"},
		{time.Hour, "1h00m"}, // minute part zero-padded
		{time.Hour + time.Minute, "1h01m"},
		{2*time.Hour + 5*time.Minute, "2h05m"},
	}
	for _, c := range cases {
		if got := fmtElapsed(c.d); got != c.want {
			t.Errorf("fmtElapsed(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
