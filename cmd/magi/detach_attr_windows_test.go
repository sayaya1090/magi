package main

import (
	"testing"

	"golang.org/x/sys/windows"
)

// 윈도우의 떼어내기는 플래그 셋이고, 그중 하나만이 IDE 문제를 푼다 — 잡 오브젝트에서 빠져나오는
// 것. 잡이 그것을 안 허락하면 스폰 자체가 실패하므로, 빠지지 않는 나머지로 한 번 더 시도한다.
func TestWindowsAsksToLeaveTheConsoleAndTheJob(t *testing.T) {
	f := detachAttr().CreationFlags
	for name, bit := range map[string]uint32{
		"DETACHED_PROCESS":          windows.DETACHED_PROCESS,
		"CREATE_NEW_PROCESS_GROUP":  windows.CREATE_NEW_PROCESS_GROUP,
		"CREATE_BREAKAWAY_FROM_JOB": windows.CREATE_BREAKAWAY_FROM_JOB,
	} {
		if f&bit == 0 {
			t.Errorf("%s was not asked for", name)
		}
	}
	b := detachFallback()
	if b == nil {
		t.Fatal("a job that forbids breakaway must still get a daemon")
	}
	if b.CreationFlags&windows.CREATE_BREAKAWAY_FROM_JOB != 0 {
		t.Error("the fallback is the one WITHOUT the breakaway")
	}
	if b.CreationFlags&windows.DETACHED_PROCESS == 0 {
		t.Error("the fallback still leaves the console")
	}
}
