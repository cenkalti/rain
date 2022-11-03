//go:build !linux

package filestorage

import "os"

func disableReadAhead(f *os.File) error {
	return nil
}

func applyNoAtimeFlag(f int) int {
	return f
}
