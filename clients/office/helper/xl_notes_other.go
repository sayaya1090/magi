//go:build !windows

package office

import "errors"

// Mac·Linux 에는 Excel COM 이 없다 — 2021 의 메모는 그 판에서는 정말로 길이 없고, 답이 그렇게 말한다.
func openXLNoterOS() (xlNoter, error) {
	return nil, errors.New("COM 은 Windows 에만 있습니다")
}
