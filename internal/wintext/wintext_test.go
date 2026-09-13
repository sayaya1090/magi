package wintext

import "testing"

// 이 둘은 플랫폼과 무관한 계약이다: **이미 읽히는 것과 글이 아닌 것은 건드리지 않는다.**
// 윈도우에서 이것이 깨지면 멀쩡한 UTF-8 출력이 코드 페이지로 재해석되고(더 나쁜 깨짐), 바이너리
// 출력이 글인 척하게 된다 — 둘 다 고치려는 결함보다 나쁘다.

func TestTextThatAlreadyReadsIsLeftExactlyAsItIs(t *testing.T) {
	for _, s := range []string{"", "plain ascii\n", "한글과 ASCII 섞인 줄\n", "emoji 🌍 ok"} {
		if got := string(ToUTF8([]byte(s))); got != s {
			t.Errorf("UTF-8 을 고쳐 놓았다: %q → %q", s, got)
		}
	}
}

func TestSomethingCarryingANULIsNotTextAndIsNotTouched(t *testing.T) {
	// An ELF header, a UTF-16 stream, an image. Invalid UTF-8 AND binary — so the NUL rule is what
	// decides, not the UTF-8 one.
	b := []byte{0x7f, 'E', 'L', 'F', 0x00, 0x02, 0xc0, 0xa7, 0x00}
	got := ToUTF8(b)
	if string(got) != string(b) {
		t.Errorf("바이너리를 글로 읽으려 했다: % x → % x", b, got)
	}
}
