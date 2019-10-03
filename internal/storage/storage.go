// Package storage contains an interface for reading and writing files in a torrent.
package storage

import "io"

// Storage is an interface for reading/writing torrent files.
type Storage interface {
	Open(name string, size int64) (f File, exists bool, err error)
}

// File interface for reading/writing torrent data.
type File interface {
	io.ReaderAt
	io.WriterAt
	io.Closer
}
