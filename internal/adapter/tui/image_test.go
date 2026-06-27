package tui

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestRenderHalfBlock(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	out := renderHalfBlock(img, 4)
	if !strings.Contains(out, "▀") {
		t.Errorf("expected half-block char")
	}
	if !strings.Contains(out, "38;2;255;0;0") {
		t.Errorf("expected truecolor red fg; got %q", out)
	}
}

func TestImageProtocols(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if k := renderKitty(img, 10); !strings.HasPrefix(k, "\x1b_Gf=100,a=T,c=10") {
		t.Errorf("kitty prefix wrong: %q", k[:20])
	}
	if it := renderITerm2(img, 10); !strings.HasPrefix(it, "\x1b]1337;File=inline=1;width=10") {
		t.Errorf("iterm2 prefix wrong: %q", it[:20])
	}
}

func TestSummarizeResult(t *testing.T) {
	if got := summarizeResult(`[{"name":"a.go","isDir":false},{"name":"b.go"},{"name":"c"}]`); got != "3 items: a.go, b.go, c" {
		t.Errorf("array summary=%q", got)
	}
	if got := summarizeResult(`["x.go","y.go","z.go","p","q","r"]`); got != "6 items: x.go, y.go, z.go, p, q, …" {
		t.Errorf("string-array summary=%q", got)
	}
	if got := summarizeResult("wrote 12 bytes to f.txt\nmore"); got != "wrote 12 bytes to f.txt" {
		t.Errorf("text summary=%q", got)
	}
	if got := summarizeResult("[]"); got != "(none)" {
		t.Errorf("empty=%q", got)
	}
}
