package update

import "testing"

func TestUpdatePolicy(t *testing.T) {
	cases := []struct {
		cur, latest string
		want        Policy
	}{
		{"1.2.3", "1.2.4", PolicyNotify}, // patch bump → notify
		{"1.2.3", "1.2.99", PolicyNotify},
		{"1.2.3", "1.3.0", PolicyForce}, // minor bump ("중간") → force
		{"1.2.3", "1.3.1", PolicyForce},
		{"1.2.3", "2.0.0", PolicyForce}, // major bump → force
		{"v1.2.3", "v2.1.0", PolicyForce},
		{"1.2.3", "1.2.3", PolicyNone}, // same
		{"1.2.3", "1.2.2", PolicyNone}, // latest older (patch)
		{"1.3.0", "1.2.9", PolicyNone}, // latest older (minor)
		{"2.0.0", "1.9.9", PolicyNone}, // latest older (major)
		{"dev", "1.2.3", PolicyNotify}, // unparseable current → offer, never force
		{"1.2.3", "garbage", PolicyNone},
		{"1.2.3", "1.2.3-rc1", PolicyNone},  // pre-release of same version → not newer
		{"1.2.3", "1.3.0-rc1", PolicyForce}, // pre-release of a minor bump → force
	}
	for _, c := range cases {
		if got := UpdatePolicy(c.cur, c.latest); got != c.want {
			t.Errorf("UpdatePolicy(%q, %q) = %v, want %v", c.cur, c.latest, got, c.want)
		}
	}
}
