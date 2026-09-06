package office

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 그림 파일 하나를 읽어 판에 넘길 수 있는 모양으로.
//
// # 왜 헬퍼가 읽는가
//
// 애드인은 브라우저 안이라 디스크를 못 읽는다. 모델은 사진을 지어낼 수 없다. 남은 길은 둘인데:
//
//  1. 모델이 base64 를 인자로 싣는다 — **대화가 그림으로 찬다.** 1MB 짜리 사진이 1.3MB 의 글이
//     되어 매 걸음 다시 실려 간다. 그리고 모델은 애초에 그 바이트를 어디서도 못 얻는다.
//  2. **모델은 경로만 말하고 헬퍼가 읽는다.** 헬퍼는 사람의 머신에서 도는 보통 프로세스라 파일을
//     읽을 수 있고, 판까지는 로컬 연결이라 바이트가 대화를 안 지난다.
//
// 둘째다. 모델의 문맥에 들어가는 것은 경로 한 줄뿐이다.
//
// # 그래서 조심할 것
//
// **모델이 부른 경로의 파일을 우리가 읽는다.** 그 말이 무겁다 — 남이 준 덱에 숨은 글이 모델을
// 꾀어 엉뚱한 파일을 가리키게 할 수 있고(§6.13), 그러면 그 내용이 슬라이드에 박혀 사람이 그것을
// 그대로 남에게 보낸다.
//
// 그래서 **내용을 보고 그림이 아니면 거절한다.** 확장자를 안 믿는다 — `.png` 로 이름만 바꾼
// 설정 파일은 첫 여덟 바이트가 다르다. 이 한 줄이 「비밀을 슬라이드에 박아 내보내기」를 막는다.
//
// 크기에도 천장을 둔다. 로컬 연결이라도 30MB 짜리 사진은 판을 멎게 하고, 멎은 판은 사람에게
// 고장이다.

// maxImageBytes 는 받아 줄 그림 하나의 천장.
//
// 넉넉하되 유한하다 — 발표 자료에 넣을 사진은 대개 몇 MB 안쪽이고, 그보다 큰 것은 슬라이드에
// 넣기 전에 줄이는 것이 맞다. **자르지 않고 거절한다**: 반쯤 읽은 그림은 그림이 아니다.
const maxImageBytes = 12 << 20 // 12MB

// ImageFile 은 읽어 온 그림 하나.
type ImageFile struct {
	Base64 string
	// Ext 는 부품 이름에 쓸 확장자(점 없이). 확장자가 아니라 **내용**이 정한다.
	Ext string
	// Mime 은 `[Content_Types].xml` 에 적을 형식.
	Mime string
	// Width·Height 는 원래 크기(픽셀). 0 이면 못 읽은 것이고, 그때는 비율을 못 지킨다.
	Width, Height int
	// Path 는 실제로 읽은 절대 경로. **답에 싣는다** — 어느 파일을 읽었는지 사람이 봐야 한다.
	Path  string
	Bytes int
}

// sniff 는 **내용으로** 그림 종류를 가린다. 확장자는 안 본다.
func sniff(b []byte) (ext, mime string, ok bool) {
	switch {
	case len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return "png", "image/png", true
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "jpeg", "image/jpeg", true
	case len(b) >= 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a"):
		return "gif", "image/gif", true
	case len(b) >= 2 && b[0] == 'B' && b[1] == 'M':
		return "bmp", "image/bmp", true
	}
	return "", "", false
}

// sizeOf 는 원래 크기를 읽는다. **못 읽으면 0 을 준다** — 지어내면 사진이 찌그러진다.
//
// 비율이 왜 중요한가: 사람이 크기를 안 말하면 우리가 정해야 하는데, 원래 비율을 모르면 정사각형에
// 우겨넣게 되고 그건 화면에서 바로 보인다. 「넣어 줬는데 이상하다」가 되면 안 넣느니만 못하다.
func sizeOf(ext string, b []byte) (int, int) {
	switch ext {
	case "png":
		// IHDR 은 항상 첫 청크다: 8(서명) + 4(길이) + 4("IHDR") 다음이 폭·높이.
		if len(b) >= 24 {
			return int(binary.BigEndian.Uint32(b[16:20])), int(binary.BigEndian.Uint32(b[20:24]))
		}
	case "gif":
		if len(b) >= 10 {
			return int(binary.LittleEndian.Uint16(b[6:8])), int(binary.LittleEndian.Uint16(b[8:10]))
		}
	case "bmp":
		if len(b) >= 26 {
			return int(int32(binary.LittleEndian.Uint32(b[18:22]))),
				int(int32(binary.LittleEndian.Uint32(b[22:26])))
		}
	case "jpeg":
		// 표시(marker)를 훑어 SOF 를 찾는다. 길이 칸이 있는 표시만 건너뛴다.
		for i := 2; i+9 < len(b); {
			if b[i] != 0xFF {
				i++
				continue
			}
			m := b[i+1]
			// 채움 바이트와 길이 없는 표시들.
			if m == 0xFF {
				i++
				continue
			}
			if m == 0xD8 || m == 0x01 || (m >= 0xD0 && m <= 0xD7) {
				i += 2
				continue
			}
			size := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
			// SOF0..SOF15 중 DHT(C4)·JPG(C8)·DAC(CC) 는 크기 정보가 아니다.
			if m >= 0xC0 && m <= 0xCF && m != 0xC4 && m != 0xC8 && m != 0xCC {
				if i+9 < len(b) {
					return int(binary.BigEndian.Uint16(b[i+7 : i+9])),
						int(binary.BigEndian.Uint16(b[i+5 : i+7]))
				}
				return 0, 0
			}
			i += 2 + size
		}
	}
	return 0, 0
}

// ReadImage 는 경로 하나를 읽어 그림인지 보고 넘겨준다.
//
// 실패는 전부 **사람이 읽는 문장**이다 — 이 답은 모델을 거쳐 사람에게 간다.
func ReadImage(path string) (ImageFile, error) {
	raw := strings.TrimSpace(path)
	if raw == "" {
		return ImageFile{}, fmt.Errorf("어느 그림 파일인지 경로를 주세요")
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		abs = raw
	}
	st, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return ImageFile{}, fmt.Errorf("그런 파일이 없습니다: %s", abs)
		}
		return ImageFile{}, fmt.Errorf("파일을 못 봤습니다(%s): %w", abs, err)
	}
	if st.IsDir() {
		return ImageFile{}, fmt.Errorf("%s 는 폴더입니다 — 그림 파일 하나를 짚어 주세요", abs)
	}
	if st.Size() > maxImageBytes {
		// **자르지 않고 거절한다.** 반쯤 읽은 그림은 그림이 아니다.
		return ImageFile{}, fmt.Errorf("그림이 너무 큽니다(%.1fMB, 최대 %dMB) — "+
			"슬라이드에 넣기 전에 줄여 주세요: %s",
			float64(st.Size())/(1<<20), maxImageBytes>>20, abs)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return ImageFile{}, fmt.Errorf("파일을 못 읽었습니다(%s): %w", abs, err)
	}

	// **내용으로 가린다.** 확장자를 믿으면 `.png` 로 이름만 바꾼 아무 파일이나 슬라이드에 박히고,
	// 그 슬라이드는 사람이 그대로 남에게 보낸다.
	ext, mime, ok := sniff(data)
	if !ok {
		return ImageFile{}, fmt.Errorf("%s 는 그림 파일이 아닙니다 — "+
			"내용을 보고 가린 것이라 확장자만 바꾼 파일도 여기서 걸립니다. "+
			"PNG·JPEG·GIF·BMP 만 넣을 수 있습니다", abs)
	}
	w, h := sizeOf(ext, data)
	return ImageFile{
		Base64: base64.StdEncoding.EncodeToString(data),
		Ext:    ext, Mime: mime, Width: w, Height: h,
		Path: abs, Bytes: len(data),
	}, nil
}
