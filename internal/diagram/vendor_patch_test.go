package diagram

import (
	"os"
	"strings"
	"testing"
)

// web/mermaid.min.js carries one local patch: the SVG label line spacing (hard-coded 1.1em in
// Mermaid's dP call) reads globalThis.magiLabelLineEm, so view.js can set M3 body-medium 20/14
// and Mermaid measures boxes and routes edges for it. A Mermaid upgrade must carry it again.
func TestBundledMermaidReadsTheViewerLineSpacing(t *testing.T) {
	b, err := os.ReadFile("web/mermaid.min.js")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "dP(u,h,globalThis.magiLabelLineEm||1.1,i)"); n != 1 {
		t.Fatalf("mermaid.min.js label line patch found %d times, want 1 (re-apply it after upgrading Mermaid)", n)
	}
	v, err := os.ReadFile("web/view.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(v), "globalThis.magiLabelLineEm=20/14") {
		t.Fatal("view.js does not set the label line spacing Mermaid reads")
	}
}
