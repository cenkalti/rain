// Package storage contains an interface for reading and writing files in a torrent.
package storage

import "io"

type Storage interface {
	Open(name string, size int64) (f File, exists bool, err error)

	Type() string
	Args() map[string]interface{}
	Load(args map[string]interface{}) error
}

type File interface {
	io.ReaderAt
	io.WriterAt
	io.Closer
}
