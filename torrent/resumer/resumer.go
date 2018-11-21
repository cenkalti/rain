// Package resumer contains an interface that is used by torrent package for resuming an existing download.
package resumer

// Resumer provides operations to save and load resume info for a Torrent.
type Resumer interface {
	WriteInfo([]byte) error
	WriteBitfield([]byte) error
}
