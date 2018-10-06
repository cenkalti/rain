package torrentdata

import (
	"path/filepath"

	"github.com/cenkalti/rain/storage"
	"github.com/cenkalti/rain/torrent/internal/metainfo"
)

func prepareFiles(info *metainfo.Info, sto storage.Storage) (files []storage.File, checkHash bool, err error) {
	var f storage.File
	var exists bool

	// Single file in torrent
	if !info.MultiFile {
		f, exists, err = sto.Open(info.Name, info.Length)
		if err != nil {
			return
		}
		files = []storage.File{f}
		checkHash = exists
		return
	}

	// Multiple files in torrent grouped in a folder
	files = make([]storage.File, len(info.Files))
	for i, f := range info.Files {
		parts := append([]string{info.Name}, f.Path...)
		path := filepath.Join(parts...)
		files[i], exists, err = sto.Open(path, f.Length)
		if err != nil {
			return
		}
		if exists {
			checkHash = true
		}
	}
	return
}
