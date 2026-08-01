package tui

import "testing"

// The seed list used to be rotated by editing the source, which put a housekeeping change in the
// history every tick and made the literal read as a record of what was last run. It is a
// parameter now, and these are the two things that have to hold for that to be safe.
func TestTheSeedsComeFromTheEnvironmentOrTheBaseline(t *testing.T) {
	t.Setenv("MAGI_FUZZ_SEEDS", "")
	if got := fuzzSeeds(t); len(got) != len(fuzzBaseline) || got[0] != fuzzBaseline[0] {
		t.Errorf("an unset variable did not fall back to the baseline: %v", got)
	}
	for _, raw := range []string{"1,2,3", "1 2 3", " 1, 2 ,3 ", "1\n2\n3"} {
		t.Setenv("MAGI_FUZZ_SEEDS", raw)
		got := fuzzSeeds(t)
		if len(got) != 3 || got[0] != 1 || got[2] != 3 {
			t.Errorf("%q parsed to %v", raw, got)
		}
	}
}

// A malformed entry must FAIL, not be skipped. Nineteen seeds walked because one had a typo would
// report the same green as twenty, and knowing which ground was covered is the whole point of
// rotating them.
func TestAMalformedSeedIsNotSilentlySkipped(t *testing.T) {
	for _, bad := range []string{"1,two,3", "1,,3x", "abc"} {
		t.Run(bad, func(t *testing.T) {
			fake := &testing.T{}
			done := make(chan bool)
			go func() {
				defer func() { recover(); close(done) }() // t.Fatalf in a goroutine unwinds via Goexit
				t.Setenv("MAGI_FUZZ_SEEDS", bad)
				fuzzSeeds(fake)
			}()
			<-done
			if !fake.Failed() {
				t.Errorf("%q was accepted", bad)
			}
		})
	}
}
