// +build !windows

package session

import "syscall"

func setRLimit(limit int, value uint64) error {
	rLimit := syscall.Rlimit{
		Cur: value,
		Max: value,
	}
	return syscall.Setrlimit(limit, &rLimit)
}
