//go:build !windows

package office

import "errors"

// Mac·Linux 에는 Word COM 이 없다 — 그 판에서 1.3 에 없는 기능은 정말로 길이 없고, 창의 거절이 그대로 간다.
func openWordDocOS(string) (wordDoc, error) {
	return nil, errors.New("COM 은 Windows 에만 있습니다")
}
