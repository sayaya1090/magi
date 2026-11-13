package jsonl

import (
	"fmt"
	"syscall"
)

func listingChangeTime(st *syscall.Stat_t) string {
	return fmt.Sprintf("%d:%d", st.Ctim.Sec, st.Ctim.Nsec)
}
