package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// 이 애드인의 마크. **그려서 낸다 — 파일로 안 싣는다.**
//
// 같은 마크가 웹 콘솔에 이미 있고(`internal/webassets.Icon`), 거기 적힌 사유가 여기도 그대로
// 걸린다: *"drawn here rather than shipped as a PNG because a binary asset in a source tree is a
// thing nobody can review."* 리본에 뜨던 것이 실제로 그 값을 치렀다 — `addin/assets/icon-*.png`
// 는 82~184바이트짜리 **단색 주황 네모**였고, 아무도 그 파일을 열어 보지 않았으므로 「추가 기능」
// 을 눌러 본 사람이 처음 발견했다(2026-09-04).
//
// Office 의 리본은 SVG 를 안 받는다. 그래서 웹은 SVG 한 장이면 되는 자리에 여기는 크기별 PNG 가
// 필요하고, 그 필요가 바이너리를 트리에 들이는 이유가 되지는 않는다 — 그리는 코드는 읽을 수 있다.
//
// 값은 웹 마크에서 그대로 옮겼다(viewBox 192): 세 위원이 반지름 21 로 (96,70)·(70,115)·(122,115)
// 에 서고, 반지름 43 짜리 고리가 (96,97) 을 두른다(선 굵기 4, 불투명도 .55).
//
// **바탕을 안 깐다.** 웹 쪽이 적어 둔 이유가 여기서 더 강하다 — 리본은 사용자 테마를 따라
// 밝기도 어둡기도 하고, 거기 깔린 네모는 마크가 아니라 스티커로 보인다.
const iconBox = 192.0

type disc struct {
	cx, cy, r float64
	col       color.NRGBA
}

// councillors 는 세 위원과 그들을 두르는 고리.
func councillors() []disc {
	return []disc{
		{96, 70, 21, color.NRGBA{0xFF, 0xB4, 0x54, 0xFF}},
		{70, 115, 21, color.NRGBA{0x5C, 0xD8, 0xE6, 0xFF}},
		{122, 115, 21, color.NRGBA{0xFF, 0x8A, 0x8A, 0xFF}},
	}
}

// ringInk 은 고리의 색. 불투명도 .55 를 알파에 그대로 싣는다.
var ringInk = color.NRGBA{0xFF, 0x7A, 0x1A, 0x8C}

// iconPNG 는 size×size PNG 를 그린다.
//
// **8배로 그려서 줄인다.** 16px 짜리를 직접 찍으면 원이 계단이 되고, 리본에서 그건 마크가 아니라
// 흠으로 보인다. 상자 평균으로 줄이는 것이라 알파도 같이 섞인다 — 바탕이 없으므로 가장자리는
// 투명으로 흐려져야 맞다.
func iconPNG(size int) ([]byte, error) {
	const ss = 8
	big := size * ss
	img := image.NewNRGBA(image.Rect(0, 0, big, big))
	scale := float64(big) / iconBox
	for _, d := range councillors() {
		fillDisc(img, d.cx*scale, d.cy*scale, d.r*scale, d.col)
	}
	// 고리는 안이 빈 원이라 두 반지름 사이만 칠한다.
	fillRing(img, 96*scale, 97*scale, (43-2)*scale, (43+2)*scale, ringInk)

	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a int
			for dy := 0; dy < ss; dy++ {
				for dx := 0; dx < ss; dx++ {
					c := img.NRGBAAt(x*ss+dx, y*ss+dy)
					// **알파를 곱해서 더한다.** 안 그러면 투명한 화소의 색이 평균을 끌어당겨
					// 가장자리에 검은 테가 생긴다.
					r += int(c.R) * int(c.A)
					g += int(c.G) * int(c.A)
					b += int(c.B) * int(c.A)
					a += int(c.A)
				}
			}
			if a == 0 {
				continue
			}
			out.SetNRGBA(x, y, color.NRGBA{
				R: uint8(r / a), G: uint8(g / a), B: uint8(b / a),
				A: uint8(a / (ss * ss)),
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fillDisc(img *image.NRGBA, cx, cy, r float64, col color.NRGBA) {
	fillRing(img, cx, cy, 0, r, col)
}

// fillRing 은 두 반지름 사이를 칠한다. `inner` 가 0 이면 꽉 찬 원이다.
func fillRing(img *image.NRGBA, cx, cy, inner, outer float64, col color.NRGBA) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			d2 := dx*dx + dy*dy
			if d2 > outer*outer || (inner > 0 && d2 < inner*inner) {
				continue
			}
			img.SetNRGBA(x, y, col)
		}
	}
}

// iconSizes 는 매니페스트가 이름 대는 크기들.
//
// **매니페스트와 여기가 갈리면 리본이 빈다.** 그래서 시험이 `manifest.xml` 에서 크기를 뽑아
// 이 목록과 맞춘다 — 한쪽만 늘어나는 날 조용히 404 가 나지 않게.
var iconSizes = []int{16, 32, 64, 80}

var (
	iconOnce  sync.Mutex
	iconCache = map[int][]byte{}
)

// serveIcon 은 `/assets/icon-<n>.png` 를 낸다.
func serveIcon(w http.ResponseWriter, r *http.Request) bool {
	// 주소에 판 번호 한 칸(/assets/v3/icon-32.png)을 허용한다 — Office 는 애드인 자원을 주소로 캐시하므로
	// 그림을 바꾸면 매니페스트의 번호를 올려 새 주소로 받게 한다. 번호는 아무 값이나 받는다 — 그림은
	// 하나뿐이다. (쿼리 ?v=2 도 시도했는데 그때는 아래 Cache-Control 이 아직 no-store 라 자리표시자가
	// 떴다 — 쿼리 자체가 안 되는지는 다시 안 쟀다. 경로 판은 되는 것이 잰 사실이라 이쪽을 둔다.)
	rest, ok := strings.CutPrefix(r.URL.Path, "/assets/")
	if !ok {
		return false
	}
	if head, tail, found := strings.Cut(rest, "/"); found && len(head) > 1 && head[0] == 'v' && isDigits(head[1:]) {
		rest = tail
	}
	name, ok := strings.CutPrefix(rest, "icon-")
	if !ok {
		return false
	}
	name, ok = strings.CutSuffix(name, ".png")
	if !ok {
		return false
	}
	size, err := strconv.Atoi(name)
	if err != nil {
		return false
	}
	// **아무 크기나 그려 주지 않는다.** 크기를 주소로 받는 그림은 낯선 요청 하나가 큰 그림을
	// 그리게 만드는 자리다. 매니페스트가 이름 댄 것만 낸다.
	known := false
	for _, s := range iconSizes {
		if s == size {
			known = true
			break
		}
	}
	if !known {
		http.NotFound(w, r)
		return true
	}
	iconOnce.Lock()
	body, cached := iconCache[size]
	if !cached {
		body, err = iconPNG(size)
		if err == nil {
			iconCache[size] = body
		}
	}
	iconOnce.Unlock()
	if err != nil {
		http.Error(w, fmt.Sprintf("icon: %v", err), http.StatusInternalServerError)
		return true
	}
	w.Header().Set("Content-Type", "image/png")
	// **아이콘만은 캐시하게 둔다.** 페이지 손잡이가 위에서 no-store 를 걸었는데, Office 의 리본은 아이콘을
	// 자기 캐시 파일에서 그린다 — 캐시할 수 없는 그림은 기본 자리표시자(파란 육각형)로 떴다(2026-09-06,
	// LTSC 2021 실측: 주소를 바꿔 새로 받게 해도 자리표시자였고, 이 헤더를 바꾸니 마크가 섰다). 주소에
	// 판 번호가 있으므로 오래 캐시해도 그림이 바뀌면 새 주소로 새로 받는다.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	// **최선 노력이다.** 이미 헤더를 보냈으므로 여기서 쓰기가 실패해도 할 말이 없다 — 상대가
	// 연결을 끊은 것이고, 그건 오류가 아니라 리본이 아이콘을 다 받았거나 안 받았다는 사실일 뿐이다.
	_, _ = w.Write(body)
	return true
}

// isDigits 는 빈 문자열이 아니고 전부 숫자인가.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
