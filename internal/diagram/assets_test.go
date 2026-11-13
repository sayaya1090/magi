package diagram

import (
	"strings"
	"testing"
)

func TestPreviewEscapesUserInputAndIncludesOfflineRenderer(t *testing.T) {
	h := HTML("<script>bad</script>", "flowchart LR\nA[<img src=x>]")
	if strings.Contains(h, "<h1><script>") || strings.Contains(h, "<code data-lang=\"mermaid\">flowchart LR\nA[<img") {
		t.Fatal("unescaped diagram")
	}
	if !strings.Contains(h, "window.magiMermaid") || !strings.Contains(h, "&lt;img") {
		t.Fatal("missing offline renderer or escaped input")
	}
}
