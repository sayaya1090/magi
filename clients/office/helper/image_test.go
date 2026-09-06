package office

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pngOf(w, h int) []byte {
	b := make([]byte, 24)
	copy(b, "\x89PNG\r\n\x1a\n")
	binary.BigEndian.PutUint32(b[16:20], uint32(w))
	binary.BigEndian.PutUint32(b[20:24], uint32(h))
	return b
}

func drop(t *testing.T, name string, body []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// **확장자가 아니라 내용으로 가린다.**
//
// 이 한 줄이 「비밀을 슬라이드에 박아 내보내기」를 막는다. 남이 준 덱에 숨은 글이 모델을 꾀어
// 엉뚱한 파일을 가리키게 할 수 있고(§6.13), 그러면 그 내용이 슬라이드에 박혀 사람이 그것을
// 그대로 남에게 보낸다.
func TestAFileThatIsNotAnImageIsRefusedEvenWithAnImageName(t *testing.T) {
	p := drop(t, "secret.png", []byte("api_key = \"sk-과연-이게-그림인가\"\n"))
	_, err := ReadImage(p)
	if err == nil {
		t.Fatal("그림이 아닌데 받아 줬다 — 이 내용이 슬라이드에 박힌다")
	}
	if !strings.Contains(err.Error(), "확장자만 바꾼") {
		t.Fatalf("왜 걸렸는지 안 적는다: %v", err)
	}
}

// 진짜 그림은 통하고, **내용이 정한 종류**를 돌려준다.
func TestARealImageIsReadAndTyped(t *testing.T) {
	// 이름은 .dat 인데 내용은 PNG 다 — 종류는 내용이 정한다.
	got, err := ReadImage(drop(t, "photo.dat", pngOf(800, 600)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Ext != "png" || got.Mime != "image/png" {
		t.Fatalf("종류를 내용으로 안 가렸다: %s / %s", got.Ext, got.Mime)
	}
	if got.Width != 800 || got.Height != 600 {
		t.Fatalf("원래 크기를 못 읽었다: %dx%d", got.Width, got.Height)
	}
	if got.Base64 == "" {
		t.Fatal("바이트를 안 실었다")
	}
	// **어느 파일을 읽었는지 답에 싣는다** — 사람이 봐야 하는 값이다.
	if !filepath.IsAbs(got.Path) {
		t.Fatalf("읽은 자리를 절대 경로로 안 준다: %s", got.Path)
	}
}

// 크기를 못 읽으면 **0 을 준다.** 지어내면 사진이 찌그러진다.
func TestAnUnreadableSizeIsZeroNotAGuess(t *testing.T) {
	// PNG 서명만 있고 IHDR 이 없는 조각.
	got, err := ReadImage(drop(t, "cut.png", []byte("\x89PNG\r\n\x1a\n")))
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 0 || got.Height != 0 {
		t.Fatalf("모르는 크기를 지어냈다: %dx%d", got.Width, got.Height)
	}
}

// JPEG 는 표시를 훑어야 크기가 나온다.
func TestAJpegSizeIsFoundByWalkingItsMarkers(t *testing.T) {
	// SOI, APP0(길이 4, 내용 2바이트), SOF0(길이 11: 정밀도1 높이2 폭2 …)
	b := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x04, 0x00, 0x00,
		0xFF, 0xC0, 0x00, 0x0B, 0x08, 0x01, 0x90, 0x02, 0x80, 0x03, 0x00, 0x00, 0x00}
	got, err := ReadImage(drop(t, "p.jpg", b))
	if err != nil {
		t.Fatal(err)
	}
	if got.Ext != "jpeg" {
		t.Fatalf("JPEG 로 안 봤다: %s", got.Ext)
	}
	if got.Width != 640 || got.Height != 400 {
		t.Fatalf("크기를 잘못 읽었다: %dx%d (원한 것 640x400)", got.Width, got.Height)
	}
}

// 없는 파일·폴더는 **그렇다고 이름을 대어** 말한다.
func TestAMissingPathSaysWhichPath(t *testing.T) {
	_, err := ReadImage(filepath.Join(t.TempDir(), "없는파일.png"))
	if err == nil || !strings.Contains(err.Error(), "없는파일.png") {
		t.Fatalf("어느 파일인지 안 알려 준다: %v", err)
	}
	dir := t.TempDir()
	_, err = ReadImage(dir)
	if err == nil || !strings.Contains(err.Error(), "폴더") {
		t.Fatalf("폴더를 폴더라고 안 한다: %v", err)
	}
}

// 빈 경로는 물어본다 — 조용히 아무것도 안 하면 안 된다.
func TestAnEmptyPathAsks(t *testing.T) {
	if _, err := ReadImage("   "); err == nil {
		t.Fatal("빈 경로를 받아 줬다")
	}
}

// 너무 크면 **자르지 않고 거절한다.** 반쯤 읽은 그림은 그림이 아니다.
func TestATooBigImageIsRefusedNotTruncated(t *testing.T) {
	big := make([]byte, maxImageBytes+1)
	copy(big, "\x89PNG\r\n\x1a\n")
	_, err := ReadImage(drop(t, "huge.png", big))
	if err == nil {
		t.Fatal("너무 큰데 받아 줬다")
	}
	if !strings.Contains(err.Error(), "줄여") {
		t.Fatalf("무엇을 하면 되는지 안 적는다: %v", err)
	}
}

// 네 종류를 다 가린다.
func TestTheFourKindsAreRecognised(t *testing.T) {
	for _, c := range []struct {
		name string
		head []byte
		ext  string
	}{
		{"png", []byte("\x89PNG\r\n\x1a\n"), "png"},
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "jpeg"},
		{"gif", []byte("GIF89a\x10\x00\x10\x00"), "gif"},
		{"bmp", append([]byte("BM"), make([]byte, 30)...), "bmp"},
	} {
		got, _, ok := sniff(c.head)
		if !ok || got != c.ext {
			t.Fatalf("%s 를 못 가렸다: %q ok=%v", c.name, got, ok)
		}
	}
	if _, _, ok := sniff([]byte("not an image at all")); ok {
		t.Fatal("아무 글이나 그림으로 봤다")
	}
}
