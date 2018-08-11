package torrentdata

import (
	"os"
	"path/filepath"

	"github.com/cenkalti/rain/internal/metainfo"
)

func prepareFiles(info *metainfo.Info, where string) (files []*os.File, checkHash bool, err error) {
	var f *os.File
	var exists bool

	// Single file in torrent
	if !info.MultiFile {
		f, exists, err = openOrCreate(filepath.Join(where, info.Name), info.Length)
		if err != nil {
			return
		}
		files = []*os.File{f}
		checkHash = exists
		return
	}

	// Multiple files in torrent grouped in a folder
	files = make([]*os.File, len(info.Files))
	for i, f := range info.Files {
		parts := append([]string{where, info.Name}, f.Path...)
		path := filepath.Join(parts...)
		err = os.MkdirAll(filepath.Dir(path), os.ModeDir|0750)
		if err != nil {
			return
		}
		files[i], exists, err = openOrCreate(path, f.Length)
		if err != nil {
			return
		}
		if exists {
			checkHash = true
		}
	}
	return
}

func openOrCreate(path string, length int64) (f *os.File, exists bool, err error) {
	defer func() {
		if err != nil && f != nil {
			_ = f.Close()
		}
	}()
	const mode = 0670
	f, err = os.OpenFile(path, os.O_RDWR, mode) // nolint: gosec
	if os.IsNotExist(err) {
		f, err = os.OpenFile(path, os.O_RDWR|os.O_CREATE, mode) // nolint: gosec
		if err != nil {
			return
		}
		err = f.Truncate(length)
		return
	}
	if err != nil {
		return
	}
	exists = true
	fi, err := f.Stat()
	if err != nil {
		return
	}
	if fi.Size() != length {
		err = f.Truncate(length)
	}
	return
}
