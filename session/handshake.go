package session

func (t *torrent) getSKey(sKeyHash [20]byte) []byte {
	if sKeyHash == t.sKeyHash {
		return t.infoHash[:]
	}
	return nil
}

func (t *torrent) checkInfoHash(infoHash [20]byte) bool {
	return infoHash == t.infoHash
}
