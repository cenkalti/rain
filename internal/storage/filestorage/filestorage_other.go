// +build !linux

package filestorage

import "os"

func disableReadAhead(f *os.File) error {
	return nil
}
