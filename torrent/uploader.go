package torrent

// TODO implement
func (t *Torrent) uploader() {
	defer t.stopWG.Done()
}
