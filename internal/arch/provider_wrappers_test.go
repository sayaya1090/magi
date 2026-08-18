package arch

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A wrapper must not swallow what it wraps.
//
// port.LLMProvider declares one method, because StreamChat is the only thing every backend must
// do. Everything else a backend can offer — listing its catalog, measuring a window, being pointed
// somewhere else — is reached by a type assertion, and a type assertion meets the WRAPPER. Two
// wrappers exist here (the stream guard, the usage meter) and both were built implementing the
// port and nothing more, so the capabilities vanished one layer up: the console's model menu came
// back empty for as long as that guard had existed, with a backend behind it that listed three
// models to a plain curl. Nothing refused. The question never arrived.
//
// So every type that wraps a provider carries `var _ port.ProviderExtras = <it>`, and this test
// fails the build when a new one appears without that line. The compiler then says which method is
// missing, at the moment it is written, instead of a menu going quiet six months later.
func TestEveryProviderWrapperKeepsTheCapabilitiesItWraps(t *testing.T) {
	root := filepath.Join("..", "..")
	// A wrapper is a struct with a field of the provider's own type. That is what makes it a
	// wrapper rather than an implementation, and it is visible in the source without a type
	// checker.
	wrapper := regexp.MustCompile(`type\s+(\w+)\s+struct\s*\{[^}]*\binner\s+port\.LLMProvider`)
	var missing []string
	err := filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		src := string(b)
		for _, m := range wrapper.FindAllStringSubmatch(src, -1) {
			name := m[1]
			// The assertion may be written on the value or the pointer; both prove the method set.
			if strings.Contains(src, "port.ProviderExtras = "+name+"{}") ||
				strings.Contains(src, "port.ProviderExtras = (*"+name+")(nil)") {
				continue
			}
			missing = append(missing, path+": "+name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range missing {
		t.Errorf("%s wraps a provider without `var _ port.ProviderExtras = …` — every optional "+
			"capability it does not forward is one no caller can reach through it", m)
	}
}
