package tracker

type Transfer interface {
	BytesUploaded() int64
	BytesDownloaded() int64
	BytesLeft() int64
	InfoHash() [20]byte
	PeerID() [20]byte
	Port() int
}
