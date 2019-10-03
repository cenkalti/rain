package peerwriter

// BlockUploaded is used to signal the Torrent when a piece block is uploaded to remote peer.
// BlockUploaded can be used to count the number of bytes uploaded to peers.
type BlockUploaded struct {
	Length uint32
}
