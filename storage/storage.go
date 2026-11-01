// Package storage contains an interface for reading and writing files in a torrent.
package storage

import (
	"context"
	"errors"
	"io"
)

// ErrNotWritten is returned by File.ReadAt when the range contains bytes that
// were never written. While verifying existing data, a piece that cannot be read
// for this reason is treated as missing and downloaded, instead of failing the torrent.
var ErrNotWritten = errors.New("storage: range not written")

// Storage is an interface for reading/writing torrent files.
type Storage interface {
	// Open a file. If the file does not exist, it will be created.
	Open(name string, size int64) (f File, exists bool, err error)
	// RootDir returns the location of the torrent's files.
	// File storage returns an absolute directory path. Other storage may return
	// any identifier of its own, such as a URL.
	RootDir() string
}

// File interface for reading/writing torrent data.
type File interface {
	io.ReaderAt
	io.WriterAt
	io.Closer
}

// Provider is a torrent storage provider.
type Provider interface {
	// GetStorage returns a storage for a torrent.
	GetStorage(torrentID string) (Storage, error)
}

// Preparer is implemented by storage that needs the layout of the whole torrent
// before any of its files are opened.
// Prepare is called before every allocation, so it must be idempotent.
// ctx is canceled when the torrent is stopped.
type Preparer interface {
	Prepare(ctx context.Context, m Manifest) error
}

// Remover is implemented by storage that deletes its own data when the torrent
// is removed from the session. Storage that does not implement it keeps its data.
// m is nil if the metadata of the torrent is not downloaded yet.
// The torrent stays in the session if Remove returns an error, so Remove may be
// called again for the same torrent, possibly concurrently.
type Remover interface {
	Remove(m *Manifest, keepData bool) error
}

// Manifest describes the files of a torrent, in torrent order.
type Manifest struct {
	InfoHash    [20]byte
	Name        string
	PieceLength uint32
	Files       []ManifestFile
}

// ManifestFile is a file in the Manifest.
type ManifestFile struct {
	Path    string
	Size    int64
	Padding bool
}
