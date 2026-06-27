package tui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"strings"
)

// renderImageFile decodes an image and renders it for the terminal: a high-res
// inline protocol (kitty/iterm2) when supported, otherwise half-block pixels
// (▀ with fg=top/bg=bottom) which work on any truecolor terminal (D8).
func renderImageFile(path string, maxCols int, proto string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return "", err
	}
	switch proto {
	case "kitty":
		return renderKitty(img, maxCols), nil
	case "iterm2":
		return renderITerm2(img, maxCols), nil
	default:
		return renderHalfBlock(img, maxCols), nil
	}
}

// encodePNG re-encodes an image to PNG bytes.
func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// renderKitty emits the kitty graphics protocol (base64 PNG, chunked), sized to
// cols columns.
func renderKitty(img image.Image, cols int) string {
	data := base64.StdEncoding.EncodeToString(encodePNG(img))
	const chunk = 4096
	var b strings.Builder
	for i := 0; i < len(data); i += chunk {
		end := i + chunk
		if end > len(data) {
			end = len(data)
		}
		more := 0
		if end < len(data) {
			more = 1
		}
		if i == 0 {
			fmt.Fprintf(&b, "\x1b_Gf=100,a=T,c=%d,m=%d;%s\x1b\\", cols, more, data[i:end])
		} else {
			fmt.Fprintf(&b, "\x1b_Gm=%d;%s\x1b\\", more, data[i:end])
		}
	}
	return b.String()
}

// renderITerm2 emits the iTerm2 inline-image protocol (base64 PNG).
func renderITerm2(img image.Image, cols int) string {
	data := base64.StdEncoding.EncodeToString(encodePNG(img))
	return fmt.Sprintf("\x1b]1337;File=inline=1;width=%d;preserveAspectRatio=1:%s\a", cols, data)
}

func renderHalfBlock(img image.Image, maxCols int) string {
	b := img.Bounds()
	iw, ih := b.Dx(), b.Dy()
	if iw == 0 || ih == 0 {
		return ""
	}
	if maxCols < 8 {
		maxCols = 8
	}
	cols := maxCols
	if iw < cols {
		cols = iw
	}
	// Each row of text = 2 vertical pixels; preserve aspect ratio.
	rows := ih * cols / iw / 2
	if rows < 1 {
		rows = 1
	}

	at := func(px, py int) (uint32, uint32, uint32) {
		sx := b.Min.X + px*iw/cols
		sy := b.Min.Y + py*ih/(rows*2)
		r, g, bl, _ := img.At(sx, sy).RGBA()
		return r >> 8, g >> 8, bl >> 8
	}

	var sb strings.Builder
	for ry := 0; ry < rows; ry++ {
		for cx := 0; cx < cols; cx++ {
			tr, tg, tb := at(cx, ry*2)
			br, bg, bb := at(cx, ry*2+1)
			fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", tr, tg, tb, br, bg, bb)
		}
		sb.WriteString("\x1b[0m\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}
