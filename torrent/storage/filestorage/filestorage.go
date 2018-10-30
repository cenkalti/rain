// Package filestorage implements Storage interface that uses files on disk as storage.
package filestorage

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/cenkalti/rain/torrent/storage"
)

const StorageType = "file"

const destKey = "dest"

type FileStorage struct {
	dest string
}

func New(dest string) (*FileStorage, error) {
	var err error
	dest, err = filepath.Abs(dest)
	if err != nil {
		return nil, err
	}
	return &FileStorage{dest: dest}, nil
}

var _ storage.Storage = (*FileStorage)(nil)

func (s *FileStorage) Open(name string, size int64) (f storage.File, exists bool, err error) {
	// All files are saved under dest.
	name = filepath.Join(s.dest, name)

	// Create containing dir if not exists.
	err = os.MkdirAll(filepath.Dir(name), os.ModeDir|0750)
	if err != nil {
		return
	}

	// Make sure OS file is closed in case of any error.
	var of *os.File
	defer func() {
		if err != nil && of != nil {
			_ = of.Close()
		}
	}()

	// Open OS file.
	const mode = 0640
	of, err = os.OpenFile(name, os.O_RDWR, mode) // nolint: gosec
	if os.IsNotExist(err) {
		of, err = os.OpenFile(name, os.O_RDWR|os.O_CREATE, mode) // nolint: gosec
		if err != nil {
			return
		}
		f = of
		err = of.Truncate(size)
		return
	}
	if err != nil {
		return
	}
	f = of
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

func (s *FileStorage) Type() string {
	return StorageType
}

func (s *FileStorage) Args() map[string]interface{} {
	return map[string]interface{}{
		destKey: s.dest,
	}
}

func (s *FileStorage) Load(args map[string]interface{}) error {
	dest, ok := args[destKey]
	if !ok {
		return errors.New("no dest param in file storage args")
	}
	s.dest, ok = dest.(string)
	if !ok {
		return errors.New("dest param in file storage args must be string")
	}
	return nil
}
