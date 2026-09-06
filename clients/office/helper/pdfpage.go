package office

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
)

// RenderPDFPage 은 PDF 한 쪽을 PNG 로 만든다 — Word 의 눈(render_page). Word.js 는 페이지 그림을 안 주고
// 문서 전체를 PDF 로만 내주므로(getFileAsync), 그림으로 만드는 일은 사람 머신에서 도는 이 프로세스 몫이다.
//
// 길은 둘이고 둘 다 남의 도구다: poppler 의 pdftoppm(어느 쪽이든, 어느 OS 든), 없으면 macOS 의 sips(첫 쪽만).
// 둘 다 없으면 **말하고 거절한다** — 빈 그림을 지어내지 않는다.
func RenderPDFPage(pdf []byte, page, maxWidth int) ([]byte, error) {
	if page < 1 {
		page = 1
	}
	if maxWidth <= 0 {
		maxWidth = 800
	}
	dir, err := os.MkdirTemp("", "magi-pdf-")
	if err != nil {
		return nil, fmt.Errorf("임시 폴더를 못 만들었습니다: %w", err)
	}
	defer os.RemoveAll(dir)
	in := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(in, pdf, 0o600); err != nil {
		return nil, fmt.Errorf("PDF 를 못 적었습니다: %w", err)
	}
	if bin, err := exec.LookPath("pdftoppm"); err == nil {
		out := filepath.Join(dir, "page")
		cmd := exec.Command(bin, "-png", "-singlefile", "-f", strconv.Itoa(page), "-l", strconv.Itoa(page), "-scale-to-x", strconv.Itoa(maxWidth), "-scale-to-y", "-1", in, out)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("pdftoppm 이 %d쪽을 못 그렸습니다(%s): %w", page, bytes.TrimSpace(stderr.Bytes()), err)
		}
		png, err := os.ReadFile(out + ".png")
		if err != nil {
			return nil, fmt.Errorf("pdftoppm 결과를 못 읽었습니다 — %d쪽이 없는 쪽일 수 있습니다: %w", page, err)
		}
		return png, nil
	}
	if runtime.GOOS == "darwin" {
		if page != 1 {
			return nil, fmt.Errorf("이 Mac 에는 pdftoppm 이 없어 sips 로 첫 쪽만 그립니다 — %d쪽은 못 그립니다(brew install poppler)", page)
		}
		out := filepath.Join(dir, "page.png")
		cmd := exec.Command("sips", "-s", "format", "png", "--resampleWidth", strconv.Itoa(maxWidth), in, "--out", out)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("sips 가 PDF 를 못 그렸습니다(%s): %w", bytes.TrimSpace(stderr.Bytes()), err)
		}
		return os.ReadFile(out)
	}
	return nil, fmt.Errorf("이 머신에는 PDF 를 그림으로 만드는 도구가 없습니다 — poppler 의 pdftoppm 을 깔면 됩니다(Windows: choco install poppler / Mac: brew install poppler). 그 전엔 read_html 이 눈입니다")
}

var pdfPageRe = regexp.MustCompile(`/Type\s*/Page[^s]`)

// PDFPageCount 는 쪽 수 — 객체 사전의 /Type /Page 를 센다(/Pages 는 뺀다). 압축된 객체 스트림에 든 문서는
// 0 을 답할 수 있고, 그때는 모른다는 뜻이라 쪽 검사를 안 한다.
func PDFPageCount(pdf []byte) int {
	return len(pdfPageRe.FindAll(pdf, -1))
}
