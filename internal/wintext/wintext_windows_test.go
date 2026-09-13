//go:build windows

package wintext

import (
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

// wideCharToMultiByte is the encode direction, which x/sys/windows does not carry (it has
// MultiByteToWideChar and not its counterpart). Declared here rather than in the package: nothing in
// the product encodes TO a code page — only this test does, to make its own subject.
var procWideCharToMultiByte = windows.NewLazySystemDLL("kernel32.dll").NewProc("WideCharToMultiByte")

func wideCharToMultiByte(cp uint32, w []uint16, b []byte) (int, error) {
	var pb *byte
	nb := 0
	if len(b) > 0 {
		pb, nb = &b[0], len(b)
	}
	r, _, err := procWideCharToMultiByte.Call(uintptr(cp), 0,
		uintptr(unsafe.Pointer(&w[0])), uintptr(len(w)),
		uintptr(unsafe.Pointer(pb)), uintptr(nb), 0, 0)
	if r == 0 {
		return 0, err
	}
	return int(r), nil
}

// **이 기계의 코드 페이지로 쓴 글이 되돌아오는가** — 왕복으로 잰다.
//
// ⚠ 기대값을 박아 두지 않는다. CP949 바이트를 상수로 적으면 그 시험은 한국어 윈도우에서만 참이고
// 다른 곳에서는 「고장」을 보고한다. 그래서 **이 기계에게 물어서** 문자열을 고르고, 같은 코드 페이지로
// 인코딩한 바이트를 넣는다 — 어느 로케일에서도 같은 주장을 한다.

// asCodePage encodes s with WideCharToMultiByte, the counterpart of what ToUTF8 uses. It returns
// false when this machine's code page cannot represent s — which is the answer for most strings on
// most machines, and the reason the test picks its subject rather than declaring it.
func asCodePage(cp uint32, s string) ([]byte, bool) {
	w := utf16.Encode([]rune(s))
	if len(w) == 0 {
		return nil, false
	}
	// WC_ERR_INVALID_CHARS is not what refuses an unrepresentable character — that is what the
	// default-char pointer is for. Ask for the length, encode, then decode it back and compare: a
	// character the code page could not place comes back as '?', and the comparison catches it.
	n, err := wideCharToMultiByte(cp, w, nil)
	if err != nil || n <= 0 {
		return nil, false
	}
	b := make([]byte, n)
	n, err = wideCharToMultiByte(cp, w, b)
	if err != nil || n <= 0 {
		return nil, false
	}
	b = b[:n]
	back, ok := decode(cp, b, mbErrInvalidChars)
	if !ok || string(back) != s {
		return nil, false
	}
	return b, true
}

// aStringThisMachineCanWrite picks a non-ASCII string the console's own code page round-trips, and
// says so when there is none (a pure-UTF-8 console: nothing to decode, nothing to measure).
func aStringThisMachineCanWrite(t *testing.T) (string, []byte, uint32) {
	t.Helper()
	cps := candidates()
	if len(cps) == 0 {
		t.Skip("이 기계는 UTF-8 콘솔이다 — 디코드할 것이 없다")
	}
	for _, s := range []string{"위치 줄:1 문자:27", "日本語の行", "Grüße, Straße", "naïve café"} {
		if b, ok := asCodePage(cps[0], s); ok && !utf8.Valid(b) {
			return s, b, cps[0]
		}
	}
	t.Skipf("코드 페이지 %d 로는 위 문자열 중 어느 것도 못 쓴다 — 이 기계에서는 잴 수 없다", cps[0])
	return "", nil, 0
}

func TestBytesWrittenInThisMachinesCodePageComeBackAsTheTextTheyWere(t *testing.T) {
	want, b, cp := aStringThisMachineCanWrite(t)
	t.Logf("코드 페이지 %d · % x", cp, b)
	if got := string(ToUTF8(b)); got != want {
		t.Errorf("코드 페이지 %d 로 쓴 글이 안 돌아왔다:\n 넣은 것: % x\n 나온 것: %q\n 원본:    %q", cp, b, got, want)
	}
}

// 잘린 자리가 있어도 **읽히는 것은 읽힌다.** 큰 출력은 가운데를 잘라내므로 다중 바이트 문자 한가운데를
// 지나는 일이 실제로 일어난다. 그때 통째로 포기하면(엄격 디코드만 있으면) 전부 U+FFFD 로 가는데,
// 그것은 이 함수가 없을 때와 같다 — 그러니 관용 경로가 있어야 하고, 그것이 서 있는지 여기서 묻는다.
func TestACutThroughACharacterStillLeavesTheRestReadable(t *testing.T) {
	want, b, cp := aStringThisMachineCanWrite(t)
	if len(b) < 4 {
		t.Skip("이 문자열은 자를 만큼 길지 않다")
	}
	// Drop the last byte: if it was half of a character, a strict decode of the whole now fails.
	got := string(ToUTF8(b[:len(b)-1]))
	head := want
	for len(head) > 0 && !strings.HasPrefix(got, head) {
		_, size := utf8.DecodeLastRuneInString(head)
		head = head[:len(head)-size]
	}
	if head == "" {
		t.Errorf("잘린 바이트열에서 아무것도 못 건졌다 (코드 페이지 %d): %q", cp, got)
	} else {
		t.Logf("건진 것: %q (원본 %q 의 앞쪽)", got, want)
	}
}
