//go:build !windows

package office

import "errors"

// Mac·Linux 에는 Word COM 이 없다 — pdftoppm·sips 가 그 판의 눈이다(pdfpage.go).
func renderWordPageOS(string, int, int) ([]byte, error) {
	return nil, errors.New("COM 은 Windows 에만 있습니다")
}
