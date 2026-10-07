package office

import (
	"path/filepath"
	"strings"
	"testing"
)

// The macOS line has to work when pasted as is: the default config directory on a Mac is
// `~/Library/Application Support/magi`, so an unquoted path splits at the space, and `-d` names the
// admin trust domain, which disagrees with `-k login.keychain-db` (PowerPoint manual §9.2).
func TestCertInstallHintMacLinePastesAsIs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Application Support", "magi")
	cert, _ := CertPaths(dir)
	var mac string
	for _, line := range strings.Split(CertInstallHint(dir), "\n") {
		if strings.Contains(line, "macOS:") {
			mac = line
		}
	}
	if mac == "" {
		t.Fatal("no macOS line in the hint — this test reads nothing")
	}
	if strings.Contains(mac, " -d ") {
		t.Errorf("macOS line uses -d (admin trust domain) against the login keychain: %s", mac)
	}
	if !strings.Contains(mac, `"`+cert+`"`) {
		t.Errorf("macOS line does not quote the certificate path %q: %s", cert, mac)
	}
}
