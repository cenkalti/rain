package torrent

import (
	"io/fs"
	"path/filepath"

	"github.com/cenkalti/rain/v2/internal/storage"
	"github.com/cenkalti/rain/v2/internal/storage/filestorage"
)

type fileStorageProvider struct {
	DataDir                  string
	DataDirIncludesTorrentID bool
	FilePermissions          fs.FileMode
}

func newFileStorageProvider(cfg *Config) *fileStorageProvider {
	return &fileStorageProvider{
		DataDir:                  cfg.DataDir,
		DataDirIncludesTorrentID: cfg.DataDirIncludesTorrentID,
		FilePermissions:          cfg.FilePermissions,
	}
}

func (p *fileStorageProvider) GetStorage(torrentID string) (storage.Storage, error) {
	return filestorage.New(p.getDataDir(torrentID), p.FilePermissions)
}

func (p *fileStorageProvider) getDataDir(torrentID string) string {
	if p.DataDirIncludesTorrentID {
		return filepath.Join(p.DataDir, torrentID)
	}
	return p.DataDir
}
