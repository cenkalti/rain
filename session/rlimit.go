// +build !windows

package session

import "syscall"

func setNoFile(value uint64) error {
	rLimit := syscall.Rlimit{
		Cur: value,
		Max: value,
	}
	return syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit)
}
