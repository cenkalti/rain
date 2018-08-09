package tracker

type Transfer interface {
	Uploaded() int64
	Downloaded() int64
	Left() int64
	InfoHash() [20]byte
	PeerID() [20]byte
	Port() int
}
