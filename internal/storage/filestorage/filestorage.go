// Package filestorage implements Storage interface that uses files on disk as storage.
package filestorage

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cenkalti/rain/v2/storage"
)

// FileStorage implements Storage interface for saving files on disk.
type FileStorage struct {
	dest     string
	perm     fs.FileMode
	ownsDest bool
}

// New returns a new FileStorage at the destination.
// If ownsDest is true, dest contains only this torrent and Remove deletes it entirely.
// Otherwise, Remove deletes only the top level entries of the torrent's files.
func New(dest string, perm fs.FileMode, ownsDest bool) (*FileStorage, error) {
	var err error
	dest, err = filepath.Abs(dest)
	if err != nil {
		return nil, err
	}
	return &FileStorage{dest: dest, perm: perm, ownsDest: ownsDest}, nil
}

var (
	_ storage.Storage = (*FileStorage)(nil)
	_ storage.Remover = (*FileStorage)(nil)
)

// Open a file.
func (s *FileStorage) Open(name string, size int64) (f storage.File, exists bool, err error) {
	name = filepath.Clean(name)

	// All files are saved under dest.
	name = filepath.Join(s.dest, name)

	// Create containing dir if not exists.
	err = os.MkdirAll(filepath.Dir(name), os.ModeDir|s.perm)
	if err != nil {
		return
	}

	// Make sure OS file is closed in case of any error.
	var of *os.File
	defer func() {
		if err == nil && of != nil {
			err = disableReadAhead(of)
		}
		if err != nil && of != nil {
			_ = of.Close()
		} else {
			f = of
		}
	}()

	// Open OS file.
	var mode = s.perm &^ 0111
	openFlags := os.O_RDWR | os.O_SYNC
	openFlags = applyNoAtimeFlag(openFlags)
	of, err = os.OpenFile(name, openFlags, mode)
	if os.IsNotExist(err) {
		openFlags |= os.O_CREATE
		of, err = os.OpenFile(name, openFlags, mode)
		if err != nil {
			return
		}
		err = of.Truncate(size)
		return
	}
	if err != nil {
		return
	}
	exists = true
	fi, err := of.Stat()
	if err != nil {
		return
	}
	if fi.Size() != size {
		err = of.Truncate(size)
	}
	return
}

// RootDir is the root of opened storage file.
func (s *FileStorage) RootDir() string {
	return s.dest
}

// Remove deletes the torrent's files from disk.
func (s *FileStorage) Remove(m *storage.Manifest, keepData bool) error {
	if keepData {
		return nil
	}
	if s.ownsDest {
		return os.RemoveAll(s.dest)
	}
	if m == nil {
		return nil
	}
	seen := make(map[string]struct{})
	for _, f := range m.Files {
		if f.Padding {
			continue
		}
		top, _, _ := strings.Cut(filepath.ToSlash(f.Path), "/")
		if _, ok := seen[top]; ok {
			continue
		}
		seen[top] = struct{}{}
		if !filepath.IsLocal(top) {
			return fmt.Errorf("file path is not local: %q", f.Path)
		}
		if err := os.RemoveAll(filepath.Join(s.dest, top)); err != nil {
			return err
		}
	}
	return nil
}
