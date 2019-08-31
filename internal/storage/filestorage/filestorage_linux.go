package filestorage

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func disableReadAhead(f *os.File) error {
	return unix.Fadvise(int(f.Fd()), 0, 0, unix.FADV_RANDOM)
}

func applyNoAtimeFlag(f int) int {
	return f | syscall.O_NOATIME
}
