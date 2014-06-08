package rain

import (
	"os"
	"path/filepath"
)

// download represents an active download in the program.
type download struct {
	TorrentFile *TorrentFile
	// Stats
	Downloaded int64
	Uploaded   int64
	// Left       int64
}

func NewDownload(t *TorrentFile) *download {
	return &download{TorrentFile: t}
}

func (d *download) Left() int64 {
	return d.TorrentFile.TotalLength - d.Downloaded
}

func (d *download) allocate(where string) error {
	var err error
	info := &d.TorrentFile.Info

	// Single file
	if info.Length != 0 {
		info.file, err = createTruncateSync(filepath.Join(where, info.Name), info.Length)
		return err
	}

	// Multiple files
	for _, f := range info.Files {
		parts := append([]string{where, info.Name}, f.Path...)
		path := filepath.Join(parts...)
		err = os.MkdirAll(filepath.Dir(path), os.ModeDir|0755)
		if err != nil {
			return err
		}
		f.file, err = createTruncateSync(path, f.Length)
		if err != nil {
			return err
		}
	}
	return nil
}

func createTruncateSync(path string, length int64) (*os.File, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	err = f.Truncate(length)
	if err != nil {
		return nil, err
	}

	err = f.Sync()
	if err != nil {
		return nil, err
	}

	return f, nil
}
