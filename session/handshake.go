package session

func (t *Torrent) getSKey(sKeyHash [20]byte) []byte {
	if sKeyHash == t.sKeyHash {
		return t.infoHash[:]
	}
	return nil
}

func (t *Torrent) checkInfoHash(infoHash [20]byte) bool {
	return infoHash == t.infoHash
}
