package storage

import "io"

type Storage interface {
	// Open(name string, size int64) (exists bool, err error)
	// ReadFileAt(name string, p []byte, off int64) error
	// WriteFileAt(name string, p []byte, off int64) error
	// SyncFile(name string) error

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
