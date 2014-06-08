package rain

// download represents an active download in the program.
type download struct {
	TorrentFile *TorrentFile
	// Stats
	Downloaded int64
	Uploaded   int64
	// Left       int64
}

func (d *download) Left() int64 {
	return d.TorrentFile.TotalLength - d.Downloaded
}

func NewDownload(t *TorrentFile) *download {
	return &download{TorrentFile: t}
}
