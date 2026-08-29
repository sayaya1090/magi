package plugin

import (
	"errors"
	"testing"
)

// A multi-line error folds to one reportable line.
func TestOneLineErr(t *testing.T) {
	if got := oneLineErr(errors.New(" first\nsecond \n")); got != "first second" {
		t.Fatalf("got %q", got)
	}
}
