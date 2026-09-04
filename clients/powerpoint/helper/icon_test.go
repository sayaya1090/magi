package main

import (
	"bytes"
	"fmt"
	"image/color"
	"image/png"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/webassets"
)

// 리본에 **단색 네모**가 떴다. 파일 넷이 82~184바이트였고 아무도 열어 보지 않았다 — 사람이
// 「추가 기능」을 눌러 본 날 발견했다(2026-09-04). 그러니 여기서 재는 것은 「파일이 있다」가
// 아니라 **그림이 그림인가**다.
func TestTheMarkIsNotOneFlatSquare(t *testing.T) {
	for _, size := range iconSizes {
		body, err := iconPNG(size)
		if err != nil {
			t.Fatalf("%d: %v", size, err)
		}
		img, err := png.Decode(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("%d: %v", size, err)
		}
		b := img.Bounds()
		if b.Dx() != size || b.Dy() != size {
			t.Errorf("%d: %dx%d 로 나왔다", size, b.Dx(), b.Dy())
		}
		seen := map[color.RGBA]int{}
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				r, g, bb, a := img.At(x, y).RGBA()
				seen[color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(bb >> 8), uint8(a >> 8)}]++
			}
		}
		// 단색이면 색이 하나다. 네모면 투명 화소가 없다. 둘 다 리본에서 본 그 모양이다.
		if len(seen) < 4 {
			t.Errorf("%d: 색이 %d 가지뿐이다 — 단색에 가깝다", size, len(seen))
		}
		clear := 0
		for c, n := range seen {
			if c.A == 0 {
				clear += n
			}
		}
		if clear == 0 {
			t.Errorf("%d: 투명한 자리가 없다 — 바탕을 깐 네모다", size)
		}
	}
}

// **기대값을 웹 마크에서 가져온다.**
//
// 앞 판본은 `councillors()` 를 돌면서 그 자리의 색을 봤다. 그건 검사 대상에서 기대값을 읽는
// 것이라, 위원을 하나 지우면 시험도 같이 줄어 **조용히 통과했다** — 변이를 심어 보고 알았다
// (2026-09-04). 이 저장소가 이름 붙여 둔 「맞는 답이 틀린 근거로 맞는」 자리다.
//
// 정본은 웹 콘솔의 마크다(사용자가 「웹 파비콘 참고해서」라고 지목한 그것). 거기서 원을 뽑아
// 여기 값과 맞추면, **둘 중 어느 쪽이 바뀌어도** 운다.
func TestOurMarkIsTheWebMark(t *testing.T) {
	circles := regexp.MustCompile(`<circle cx="(\d+)" cy="(\d+)" r="(\d+)"([^/]*)/>`).
		FindAllStringSubmatch(webassets.Icon, -1)
	if len(circles) != 4 {
		t.Fatalf("웹 마크에서 원 %d 개를 찾았다 — 넷이어야 한다(위원 셋 + 고리)", len(circles))
	}
	type want struct {
		cx, cy, r int
		fill      string
	}
	var discs []want
	var ring *want
	for _, c := range circles {
		n := func(s string) int { v, _ := strconv.Atoi(s); return v }
		w := want{n(c[1]), n(c[2]), n(c[3]), ""}
		if m := regexp.MustCompile(`fill="(#[0-9A-Fa-f]{6})"`).FindStringSubmatch(c[4]); m != nil {
			w.fill = m[1]
			discs = append(discs, w)
			continue
		}
		if strings.Contains(c[4], `fill="none"`) {
			w.fill = regexp.MustCompile(`stroke="(#[0-9A-Fa-f]{6})"`).FindStringSubmatch(c[4])[1]
			ring = &w
		}
	}
	if len(discs) != 3 || ring == nil {
		t.Fatalf("웹 마크가 위원 셋 + 고리 하나가 아니다: 위원 %d, 고리 %v", len(discs), ring != nil)
	}
	got := councillors()
	if len(got) != len(discs) {
		t.Fatalf("위원이 %d 명인데 웹 마크는 %d 명이다", len(got), len(discs))
	}
	for i, w := range discs {
		g := got[i]
		hex := fmt.Sprintf("#%02X%02X%02X", g.col.R, g.col.G, g.col.B)
		if int(g.cx) != w.cx || int(g.cy) != w.cy || int(g.r) != w.r || !strings.EqualFold(hex, w.fill) {
			t.Errorf("%d번 위원이 웹 마크와 다르다: 여기 (%v,%v,r%v,%s) · 웹 (%d,%d,r%d,%s)",
				i, g.cx, g.cy, g.r, hex, w.cx, w.cy, w.r, w.fill)
		}
	}
	hex := fmt.Sprintf("#%02X%02X%02X", ringInk.R, ringInk.G, ringInk.B)
	if !strings.EqualFold(hex, ring.fill) {
		t.Errorf("고리 색이 웹 마크와 다르다: 여기 %s · 웹 %s", hex, ring.fill)
	}
	// 웹은 불투명도를 따로 적는다(`opacity=".55"`). 우리는 알파에 실으므로 값이 같은지 본다.
	const opacity = 0.55
	if a := uint8(math.Round(opacity * 255)); ringInk.A != a {
		t.Errorf("고리 불투명도가 다르다: %d, 웹의 .55 는 %d", ringInk.A, a)
	}
	if !strings.Contains(webassets.Icon, `opacity=".55"`) {
		t.Error("웹 마크의 불투명도가 .55 가 아니게 됐다 — 여기 값도 같이 봐야 한다")
	}
	// **바탕 있는 판본을 쓰지 않는다.** 리본은 테마를 따라 밝기도 어둡기도 하고, 거기 깔린
	// 네모는 마크가 아니라 스티커다. 웹이 그 판본을 따로 갖고 있으므로 여기 값을 못 박는다.
	if strings.Contains(webassets.Icon, "<rect") {
		t.Error("웹 마크에 바탕이 생겼다 — 리본용으로는 바탕 없는 쪽이어야 한다")
	}
}

// **그린 그림에 세 위원이 실제로 찍혔는가.** 위는 값이 같은지를 봤고 이건 화소를 본다.
// 가장 큰 것에서 잰다 — 16px 에서는 셋이 서로 번진다.
func TestTheThreeCouncillorsAreEachThere(t *testing.T) {
	body, err := iconPNG(80)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	scale := 80.0 / iconBox
	for _, d := range councillors() {
		x, y := int(d.cx*scale), int(d.cy*scale)
		r, g, b, a := img.At(x, y).RGBA()
		got := color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
		if got.A < 250 {
			t.Errorf("(%d,%d) 가 비어 있다: %+v", x, y, got)
			continue
		}
		if got.R != d.col.R || got.G != d.col.G || got.B != d.col.B {
			t.Errorf("(%d,%d) 색이 다르다: 원했다 %+v, 나왔다 %+v", x, y, d.col, got)
		}
	}
	// 고리가 위원들 **바깥**에 있다. 반지름 43 이면 (96,97) 에서 오른쪽으로 43 떨어진 자리다.
	x, y := int(96*scale+43*scale), int(97*scale)
	if _, _, _, a := img.At(x, y).RGBA(); a>>8 < 60 {
		t.Errorf("고리가 (%d,%d) 에 없다", x, y)
	}
	// 가운데는 비어 있어야 한다 — 고리는 채워진 원이 아니다.
	if _, _, _, a := img.At(int(96*scale), int(97*scale)).RGBA(); a>>8 > 10 {
		t.Errorf("고리 안이 칠해져 있다")
	}
}

// **매니페스트가 이름 대는 크기와 그리는 크기가 같아야 한다.** 한쪽만 늘면 리본이 조용히
// 404 를 받고, 그때 화면에 서는 것은 빈 자리다.
func TestEverySizeTheManifestNamesIsDrawn(t *testing.T) {
	body, err := os.ReadFile("../addin/manifest.xml")
	if err != nil {
		t.Fatal(err)
	}
	want := regexp.MustCompile(`/assets/icon-(\d+)\.png`).FindAllStringSubmatch(string(body), -1)
	if len(want) == 0 {
		t.Fatal("매니페스트에서 아이콘 주소를 못 찾았다 — 훑을 것이 없으면 이 시험은 아무것도 안 잰다")
	}
	have := map[int]bool{}
	for _, s := range iconSizes {
		have[s] = true
	}
	for _, m := range want {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("%q: %v", m[0], err)
		}
		if !have[n] {
			t.Errorf("매니페스트는 %d 를 부르는데 그리지 않는다", n)
		}
	}
}
