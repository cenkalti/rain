package tracker

import "crypto/sha1"

type Transfer interface {
	Uploaded() int64
	Downloaded() int64
	Left() int64
	InfoHash() [sha1.Size]byte
}
