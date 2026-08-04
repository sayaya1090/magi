// Package envflag reads the MAGI_* switches that turn a mechanism on or off.
//
// It exists because the same convention was implemented five times. internal/app had envOff,
// cmd/magi had envOn, two tool files carried inline copies of envOff's switch, and two more
// readers had quietly drifted off it: the council's literals gate did not accept "no", and the
// embedded-plugin gate accepted only the literal word "off". A sixth reader tested for a
// non-empty value, so MAGI_WORKFLOW=0 turned the workflow ON — the one value every other switch
// in the tree reads as "off".
//
// Nothing forced the copies to agree, and a switch that ignores the value you gave it looks
// exactly like a switch you never set.
package envflag

import (
	"os"
	"strings"
)

// Enabled reports whether the switch named by name is on. An explicit off-value (0/off/false/no)
// or on-value (1/on/true/yes) decides it, case- and space-insensitively; anything else — unset,
// empty, or a word neither side claims — leaves def, so a typo cannot silently flip a mechanism.
func Enabled(name string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "0", "off", "false", "no":
		return false
	case "1", "on", "true", "yes":
		return true
	}
	return def
}
