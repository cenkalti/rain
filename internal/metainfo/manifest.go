package metainfo

import "github.com/cenkalti/rain/v2/storage"

// Manifest returns the description of the torrent files that is given to storage.
func (i *Info) Manifest() storage.Manifest {
	m := storage.Manifest{
		InfoHash:    i.Hash,
		Name:        i.Name,
		PieceLength: i.PieceLength,
		Files:       make([]storage.ManifestFile, len(i.Files)),
	}
	for j, f := range i.Files {
		m.Files[j] = storage.ManifestFile{Path: f.Path, Size: f.Length, Padding: f.Padding}
	}
	return m
}
