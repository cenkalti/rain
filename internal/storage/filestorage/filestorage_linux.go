package filestorage

import (
	"os"

	"golang.org/x/sys/unix"
)

func disableReadAhead(f *os.File) error {
	return unix.Fadvise(int(f.Fd()), 0, 0, unix.FADV_RANDOM)
}
