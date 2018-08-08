package tracker

type Client interface {
	PeerID() [20]byte
	Port() int
}
