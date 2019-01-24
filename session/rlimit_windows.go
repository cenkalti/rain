// +build windows

package session

import "syscall"

func setRLimit(limit int, value uint64) error {
	return nil
}
