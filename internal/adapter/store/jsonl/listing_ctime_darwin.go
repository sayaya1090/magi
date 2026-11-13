package jsonl

import (
	"fmt"
	"syscall"
)

func listingChangeTime(st *syscall.Stat_t) string {
	return fmt.Sprintf("%d:%d", st.Ctimespec.Sec, st.Ctimespec.Nsec)
}
