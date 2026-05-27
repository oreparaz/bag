//go:build darwin

package stat

import (
	"syscall"
	"time"
)

func accessTime(st *syscall.Stat_t) time.Time {
	return time.Unix(st.Atimespec.Sec, st.Atimespec.Nsec)
}

func changeTime(st *syscall.Stat_t) time.Time {
	return time.Unix(st.Ctimespec.Sec, st.Ctimespec.Nsec)
}
