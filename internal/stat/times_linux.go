//go:build linux

package stat

import (
	"syscall"
	"time"
)

func accessTime(st *syscall.Stat_t) time.Time {
	return time.Unix(st.Atim.Sec, st.Atim.Nsec)
}

func changeTime(st *syscall.Stat_t) time.Time {
	return time.Unix(st.Ctim.Sec, st.Ctim.Nsec)
}
