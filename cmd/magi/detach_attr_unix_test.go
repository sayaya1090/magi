//go:build !windows

package main

import "testing"

// 유닉스의 떼어내기는 새 세션이다 — 필드 이름이 플랫폼마다 달라서, 무엇을 요구했는지는 그 플랫폼
// 안에서만 물을 수 있다(윈도우의 SysProcAttr 에는 Setsid 가 아예 없다).
func TestUnixAsksForANewSession(t *testing.T) {
	if !detachAttr().Setsid {
		t.Error("unix detachment is a new session; Setsid was not asked for")
	}
	if detachFallback() != nil {
		t.Error("there is nothing to fall back from on unix")
	}
}
